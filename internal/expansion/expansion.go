package expansion

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/glob"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelloptions"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var (
	ErrUnknown     = errors.New("value is not known")
	ErrUnsupported = errors.New("expansion is not supported")
)

const maxDepth = 8

type Value struct {
	Words []string
	Array bool
}

func scalar(s string) Value { return Value{Words: []string{s}} }

func (v Value) joined(sep string) string { return strings.Join(v.Words, sep) }

type Assumption struct {
	Name  string
	Line  int
	Value Value
}

type Result struct {
	Value       Value
	Assumptions []Assumption
}

type Evaluator struct {
	tree        *parser.Tree
	opts        shelloptions.Options
	sites       glob.Sites
	assumptions []Assumption
	overlay     map[string]Value
	depth       int
}

func New(tree *parser.Tree, opts shelloptions.Options) *Evaluator {
	return &Evaluator{tree: tree, opts: opts, sites: glob.For(tree, opts)}
}

func (e *Evaluator) Expansion(pe *syntax.ParamExp, quoted bool) (Result, error) {
	e.assumptions = nil
	v, err := e.paramExp(pe, quoted)
	if err != nil {
		return Result{}, err
	}
	return Result{Value: v, Assumptions: e.assumptions}, nil
}

func (e *Evaluator) Word(w *syntax.Word) (Result, error) {
	e.assumptions = nil
	s, err := e.word(w)
	if err != nil {
		return Result{}, err
	}
	return Result{Value: scalar(s), Assumptions: e.assumptions}, nil
}

type Outcome struct {
	Holds           bool
	Subject         string
	Groups          []string
	GroupsUncertain bool
	Assumptions     []Assumption
}

func (e *Evaluator) Matches(site glob.Site) (Outcome, error) {
	e.assumptions = nil
	if site.Subject == nil {
		return Outcome{}, unknown("the tested value")
	}
	if site.Role == glob.RoleRegex {
		switch {
		case e.tree.Dialect == dialect.Ksh:
			return Outcome{}, unsupported("=~ in ksh")
		case e.opts.NoCaseMatch:
			return Outcome{}, unsupported("matching under nocasematch")
		case e.opts.RematchPCRE:
			return Outcome{}, unsupported("PCRE matching with rematchpcre")
		}
	}
	subject, err := e.word(site.Subject)
	if err != nil {
		return Outcome{}, err
	}
	_, m, err := e.matcher(site.Word, site.Role)
	if err != nil {
		return Outcome{}, err
	}
	if site.Role == glob.RoleRegex && m.Uses(func(n glob.Node) bool { _, ok := n.(*glob.Escape); return ok }) {
		return Outcome{}, unsupported("a GNU regex escape, which BSD and macOS read differently")
	}
	match := m.Whole
	if site.Role == glob.RoleRegex {
		match = m.Search
	}
	found, ok, err := match(subject)
	if err != nil {
		return Outcome{}, err
	}
	outcome := Outcome{Holds: ok != site.Negated, Subject: subject, Assumptions: e.assumptions}
	if ok && site.Role == glob.RoleRegex {
		outcome.Groups = append(outcome.Groups, subject[found.Start:found.End])
		if found.Ambiguous {
			outcome.GroupsUncertain = true
			return outcome, nil
		}
		for _, c := range found.Captures {
			group := ""
			if c[0] >= 0 {
				group = subject[c[0]:c[1]]
			}
			outcome.Groups = append(outcome.Groups, group)
		}
	}
	return outcome, nil
}

func (e *Evaluator) zsh() bool { return e.tree.Dialect == dialect.Zsh }

func unknown(format string, args ...any) error {
	return fmt.Errorf(format+": %w", append(args, ErrUnknown)...)
}

func unsupported(format string, args ...any) error {
	return fmt.Errorf(format+": %w", append(args, ErrUnsupported)...)
}

func (e *Evaluator) word(w *syntax.Word) (string, error) {
	var sb strings.Builder
	for i, part := range w.Parts {
		if lit, ok := part.(*syntax.Lit); ok && i == 0 && strings.HasPrefix(lit.Value, "~") {
			return "", unknown("tilde expansion")
		}
		s, err := e.part(part, false)
		if err != nil {
			return "", err
		}
		sb.WriteString(s)
	}
	return sb.String(), nil
}

func (e *Evaluator) part(part syntax.WordPart, quoted bool) (string, error) {
	switch p := part.(type) {
	case *syntax.Lit:
		if quoted {
			return parser.UnescapeLit(p.Value, true), nil
		}
		return parser.UnescapeLit(p.Value, false), nil
	case *syntax.SglQuoted:
		value, _ := parser.Text(&syntax.Word{Parts: []syntax.WordPart{p}}, "")
		return value, nil
	case *syntax.DblQuoted:
		var sb strings.Builder
		for _, inner := range p.Parts {
			s, err := e.part(inner, true)
			if err != nil {
				return "", err
			}
			sb.WriteString(s)
		}
		return sb.String(), nil
	case *syntax.ParamExp:
		v, err := e.paramExp(p, quoted)
		if err != nil {
			return "", err
		}
		return v.joined(" "), nil
	}
	return "", unknown("%T", part)
}

func (e *Evaluator) variable(name string, at int) (Value, error) {
	if v, ok := e.overlay[name]; ok {
		return v, nil
	}
	if !parser.IsName(name) {
		return Value{}, unknown("special parameter $%s", name)
	}
	if e.depth >= maxDepth {
		return Value{}, unknown("%s depends on too many other variables", name)
	}
	def, ok := e.tree.DefinitionBefore(name, at)
	if !ok {
		return Value{}, unknown("%s is not assigned before this point", name)
	}
	computed := parser.AttrInteger | parser.AttrNameref | parser.AttrAssoc | parser.AttrTransformed
	if def.Kind != parser.DefAssign && def.Kind != parser.DefDeclare || e.tree.VisibleAttrs(name, e.tree.ScopeAt(at)).Has(computed) {
		return Value{}, unknown("the value of %s is computed", name)
	}
	e.depth++
	defer func() { e.depth-- }()
	v, err := e.assigned(def)
	if err != nil {
		return Value{}, err
	}
	if !slices.ContainsFunc(e.assumptions, func(a Assumption) bool { return a.Name == name }) {
		e.assumptions = append(e.assumptions, Assumption{Name: name, Line: e.tree.SourceLine(def.Span.Start), Value: v})
	}
	return v, nil
}

func (e *Evaluator) assigned(def *parser.Definition) (Value, error) {
	if def.Value != nil {
		s, err := e.word(def.Value)
		return scalar(s), err
	}
	assign, ok := def.Node.(*syntax.Assign)
	if !ok || assign.Naked || assign.Append || assign.Index != nil {
		return Value{}, unknown("%s has no literal value", def.Name)
	}
	if assign.Array == nil {
		return scalar(""), nil
	}
	words := make([]string, 0, len(assign.Array.Elems))
	for _, elem := range assign.Array.Elems {
		if elem.Index != nil || elem.Value == nil {
			return Value{}, unknown("%s has keyed elements", def.Name)
		}
		if slices.ContainsFunc(elem.Value.Parts, splits) {
			return Value{}, unknown("an element of %s is split or globbed", def.Name)
		}
		s, err := e.word(elem.Value)
		if err != nil {
			return Value{}, err
		}
		words = append(words, s)
	}
	return Value{Words: words, Array: true}, nil
}

func splits(part syntax.WordPart) bool {
	switch p := part.(type) {
	case *syntax.Lit:
		return strings.ContainsAny(parser.UnescapeLit(p.Value, false), "*?[") && !strings.Contains(p.Value, `\`)
	case *syntax.SglQuoted, *syntax.DblQuoted:
		return false
	}
	return true
}

type state struct {
	v         Value
	set       bool
	flags     []parser.ZshFlag
	quoted    bool
	joinsStar bool
}

func (s state) has(name byte) bool {
	return slices.ContainsFunc(s.flags, func(f parser.ZshFlag) bool { return f.Name == name })
}

func (s state) arg(name byte) (string, bool) {
	for _, f := range s.flags {
		if f.Name == name && len(f.Args) > 0 {
			return f.Args[0].Value, true
		}
	}
	return "", false
}

var zshSupportedFlags = "@ACFLMOPUafijnosu"

func (e *Evaluator) paramExp(pe *syntax.ParamExp, quoted bool) (Value, error) {
	if pe.Width || pe.IsSet || pe.Attributes || pe.Names != 0 || len(pe.Elems) > 0 || pe.RcExpand != syntax.OptUnset {
		return Value{}, unsupported("this form of expansion")
	}
	at := parser.Offset(pe.Pos())
	s := state{quoted: quoted}
	if pe.Flags != nil {
		s.flags = parser.ParameterFlags(pe.Flags)
		for _, f := range s.flags {
			if strings.IndexByte(zshSupportedFlags, f.Name) < 0 {
				return Value{}, unsupported("the (%c) flag", f.Name)
			}
		}
	}
	if err := e.base(pe, &s, at); err != nil {
		return Value{}, err
	}
	if err := e.subscript(pe, &s); err != nil {
		return Value{}, err
	}
	if s.has('P') {
		v, err := e.variable(s.v.joined(" "), at)
		if err != nil {
			return Value{}, err
		}
		s.v = v
	}
	if pe.Excl {
		if err := e.indirect(pe, &s, at); err != nil {
			return Value{}, err
		}
	}
	if e.zsh() && s.quoted && s.v.Array && !s.has('@') && !pe.Length && !s.has('s') && !s.has('f') {
		sep, ok := s.arg('j')
		if !ok {
			sep = " "
		}
		s.v = scalar(s.v.joined(sep))
	}
	if err := e.operator(pe, &s); err != nil {
		return Value{}, err
	}
	if err := e.modifiers(pe, &s); err != nil {
		return Value{}, err
	}
	if s.joinsStar {
		s.v = scalar(s.v.joined(" "))
	}
	if pe.Length {
		e.length(&s)
	}
	if err := e.zshFlags(pe, &s); err != nil {
		return Value{}, err
	}
	if e.zsh() && !s.quoted && s.v.Array {
		s.v.Words = slices.DeleteFunc(slices.Clone(s.v.Words), func(w string) bool { return w == "" })
	}
	return s.v, nil
}

func (e *Evaluator) base(pe *syntax.ParamExp, s *state, at int) error {
	switch {
	case pe.NestedParam != nil:
		nested, quoted := pe.NestedParam, false
		if dq, ok := nested.(*syntax.DblQuoted); ok && len(dq.Parts) == 1 {
			nested, quoted = dq.Parts[0], true
		}
		inner, ok := nested.(*syntax.ParamExp)
		if !ok {
			return unknown("command substitution")
		}
		v, err := e.paramExp(inner, quoted)
		if err != nil {
			return err
		}
		s.v, s.set = v, true
	case pe.Param != nil:
		v, err := e.variable(pe.Param.Value, at)
		if err != nil {
			return err
		}
		s.v, s.set = v, true
	default:
		s.v, s.set = scalar(""), false
	}
	if !e.zsh() && s.v.Array && pe.Index == nil {
		s.v = scalar(first(s.v.Words))
	}
	return nil
}

func first(words []string) string {
	if len(words) == 0 {
		return ""
	}
	return words[0]
}

func (e *Evaluator) subscript(pe *syntax.ParamExp, s *state) error {
	if pe.Index == nil {
		return nil
	}
	if parser.IsAllIndex(pe.Index) {
		if !s.v.Array {
			s.v = Value{Words: s.v.Words, Array: true}
		}
		lit, _ := parser.IndexLit(pe.Index)
		s.joinsStar = lit == "*" && s.quoted && !e.zsh()
		return nil
	}
	if e.zsh() {
		return e.zshSubscript(pe.Index, s)
	}
	n, ok := parser.ArithmeticConstant(pe.Index)
	if !ok {
		return unknown("subscript")
	}
	words := s.v.Words
	i := int(n)
	if i < 0 {
		i += len(words)
	}
	if i < 0 || i >= len(words) {
		s.v, s.set = scalar(""), false
		return nil
	}
	s.v = scalar(words[i])
	return nil
}

func (e *Evaluator) zshSubscript(index syntax.ArithmExpr, s *state) error {
	lo, hi, ok := zshRange(index)
	if !ok {
		return unknown("subscript")
	}
	if s.v.Array {
		from, to := zshBounds(lo, hi, len(s.v.Words))
		if !isRange(index) {
			if from >= to {
				s.v = scalar("")
				return nil
			}
			s.v = scalar(s.v.Words[from])
			return nil
		}
		s.v = Value{Words: slices.Clone(s.v.Words[from:max(from, to)]), Array: true}
		return nil
	}
	runes := []rune(s.v.joined(""))
	from, to := zshBounds(lo, hi, len(runes))
	s.v = scalar(string(runes[from:max(from, to)]))
	return nil
}

func isRange(index syntax.ArithmExpr) bool {
	b, ok := index.(*syntax.BinaryArithm)
	return ok && b.Op == syntax.Comma
}

func zshRange(index syntax.ArithmExpr) (int64, int64, bool) {
	if b, ok := index.(*syntax.BinaryArithm); ok && b.Op == syntax.Comma {
		lo, ok1 := parser.ArithmeticConstant(b.X)
		hi, ok2 := parser.ArithmeticConstant(b.Y)
		return lo, hi, ok1 && ok2
	}
	n, ok := parser.ArithmeticConstant(index)
	return n, n, ok
}

func zshBounds(lo, hi int64, n int) (int, int) {
	norm := func(i int64) int64 {
		if i < 0 {
			return int64(n) + i + 1
		}
		return i
	}
	from, to := norm(lo), norm(hi)
	if from < 1 {
		from = 1
	}
	if to > int64(n) {
		to = int64(n)
	}
	if from > to {
		return 0, 0
	}
	return int(from - 1), int(to)
}

func (e *Evaluator) indirect(pe *syntax.ParamExp, s *state, at int) error {
	if e.zsh() {
		return unsupported("${!...} in zsh")
	}
	if e.tree.Dialect == dialect.Ksh && (pe.Index == nil || !parser.IsAllIndex(pe.Index)) {
		return unsupported("${!name} in ksh, which names a reference rather than following it")
	}
	if pe.Index != nil && parser.IsAllIndex(pe.Index) {
		keys := make([]string, len(s.v.Words))
		for i := range keys {
			keys[i] = strconv.Itoa(i)
		}
		s.v = Value{Words: keys, Array: true}
		return nil
	}
	v, err := e.variable(s.v.joined(" "), at)
	if err != nil {
		return err
	}
	if v.Array {
		v = scalar(first(v.Words))
	}
	s.v = v
	return nil
}

func (e *Evaluator) eachWord(s *state, f func(string) (string, error)) error {
	words := make([]string, len(s.v.Words))
	for i, w := range s.v.Words {
		out, err := f(w)
		if err != nil {
			return err
		}
		words[i] = out
	}
	s.v.Words = words
	return nil
}

func (e *Evaluator) operator(pe *syntax.ParamExp, s *state) error {
	switch {
	case pe.Slice != nil:
		return e.slice(pe.Slice, s)
	case pe.Repl != nil:
		return e.replace(pe, s)
	case pe.Exp != nil:
		return e.expansionOp(pe, s)
	}
	return nil
}

func (e *Evaluator) slice(sl *syntax.Slice, s *state) error {
	offset, ok := parser.ArithmeticConstant(sl.Offset)
	if !ok {
		return unknown("slice offset")
	}
	length := int64(-1)
	if sl.Length != nil {
		if length, ok = parser.ArithmeticConstant(sl.Length); !ok {
			return unknown("slice length")
		}
	}
	cut := func(n int) (int, int, bool) {
		from := int(offset)
		if from < 0 {
			from += n
		}
		if from < 0 || from > n {
			return 0, 0, false
		}
		to := n
		switch {
		case length >= 0:
			to = min(n, from+int(length))
		case sl.Length != nil:
			to = n + int(length)
		}
		return from, max(from, to), to >= from
	}
	if s.v.Array {
		from, to, ok := cut(len(s.v.Words))
		if !ok {
			return unsupported("a slice out of range")
		}
		s.v.Words = slices.Clone(s.v.Words[from:to])
		return nil
	}
	runes := []rune(s.v.joined(""))
	from, to, ok := cut(len(runes))
	if !ok {
		return unsupported("a slice out of range")
	}
	s.v = scalar(string(runes[from:to]))
	return nil
}

func empty(v Value) bool {
	return len(v.Words) == 0 || len(v.Words) == 1 && v.Words[0] == ""
}

func (e *Evaluator) expansionOp(pe *syntax.ParamExp, s *state) error {
	op := pe.Exp.Op
	switch op {
	case syntax.DefaultUnset, syntax.AssignUnset, syntax.ErrorUnset:
		if s.set {
			return nil
		}
	case syntax.DefaultUnsetOrNull, syntax.AssignUnsetOrNull, syntax.ErrorUnsetOrNull:
		if s.set && !empty(s.v) {
			return nil
		}
	case syntax.AlternateUnset:
		if !s.set {
			s.v = scalar("")
			return nil
		}
	case syntax.AlternateUnsetOrNull:
		if !s.set || empty(s.v) {
			s.v = scalar("")
			return nil
		}
	case syntax.AssignAlways:
	case syntax.OtherParamOps:
		if e.tree.Dialect != dialect.Bash {
			return unsupported("${...@...} outside bash")
		}
		return e.transform(pe.Exp.Word, s)
	case syntax.ArrayExclude, syntax.ArrayIntersect, syntax.ArrayZip, syntax.ArrayZipLongest:
		return e.arrayOp(pe, s)
	default:
		return e.patternOp(pe, s)
	}
	switch op {
	case syntax.ErrorUnset, syntax.ErrorUnsetOrNull:
		return unknown("the expansion fails")
	}
	if pe.Exp.Word == nil {
		s.v = scalar("")
		return nil
	}
	w, err := e.word(pe.Exp.Word)
	if err != nil {
		return err
	}
	s.v, s.set = scalar(w), true
	return nil
}

func (e *Evaluator) transform(w *syntax.Word, s *state) error {
	op := w.Lit()
	var f func(string) string
	switch op {
	case "U":
		f = strings.ToUpper
	case "L":
		f = strings.ToLower
	case "u":
		f = upperFirst
	default:
		return unsupported("${...@%s}", op)
	}
	return e.eachWord(s, func(w string) (string, error) { return f(w), nil })
}

func upperFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

func (e *Evaluator) arrayOp(pe *syntax.ParamExp, s *state) error {
	other, err := e.variable(pe.Exp.Word.Lit(), parser.Offset(pe.Pos()))
	if err != nil {
		return err
	}
	switch pe.Exp.Op {
	case syntax.ArrayExclude:
		s.v.Words = slices.DeleteFunc(slices.Clone(s.v.Words), func(w string) bool { return slices.Contains(other.Words, w) })
	case syntax.ArrayIntersect:
		s.v.Words = slices.DeleteFunc(slices.Clone(s.v.Words), func(w string) bool { return !slices.Contains(other.Words, w) })
	default:
		n := min(len(s.v.Words), len(other.Words))
		if pe.Exp.Op == syntax.ArrayZipLongest {
			n = max(len(s.v.Words), len(other.Words))
		}
		var zipped []string
		for i := range n {
			if len(s.v.Words) == 0 || len(other.Words) == 0 {
				break
			}
			zipped = append(zipped, s.v.Words[i%len(s.v.Words)], other.Words[i%len(other.Words)])
		}
		s.v.Words = zipped
	}
	s.v.Array = true
	return nil
}

func (e *Evaluator) matcher(w *syntax.Word, role glob.Role) (glob.Role, glob.Matcher, error) {
	var resolveErr error
	role, p, ok := e.sites.Pattern(w, role, func(part syntax.WordPart) (string, bool) {
		quoted := false
		if dq, ok := part.(*syntax.DblQuoted); ok {
			part, quoted = dq, true
		}
		var s string
		var err error
		if pe, ok := part.(*syntax.ParamExp); ok {
			var v Value
			v, err = e.paramExp(pe, quoted)
			s = v.joined(" ")
		} else {
			s, err = e.part(part, quoted)
		}
		if err != nil {
			resolveErr = err
			return "", false
		}
		return s, true
	})
	if resolveErr != nil {
		return role, glob.Matcher{}, resolveErr
	}
	if !ok {
		return role, glob.Matcher{}, unknown("pattern")
	}
	if len(p.Errors) > 0 {
		return role, glob.Matcher{}, unsupported("an invalid pattern")
	}
	if unmet := p.Unmet(e.opts); len(unmet) > 0 {
		return role, glob.Matcher{}, unsupported("a pattern that needs %s", unmet[0].Option)
	}
	if e.tree.Dialect == dialect.Bash && e.opts.NoCaseMatch {
		return role, glob.Matcher{}, unsupported("matching under nocasematch")
	}
	m, err := p.Matcher()
	return role, m, err
}

func (e *Evaluator) patternOp(pe *syntax.ParamExp, s *state) error {
	switch pe.Exp.Op {
	case syntax.UpperFirst, syntax.UpperAll, syntax.LowerFirst, syntax.LowerAll:
		if e.tree.Dialect != dialect.Bash {
			return unsupported("case conversion outside bash")
		}
		return e.caseOp(pe, s)
	case syntax.MatchEmpty:
		return e.filter(pe, s)
	}
	role, ok := glob.OperatorRole(pe.Exp.Op)
	if !ok {
		return unsupported("the %s operator", pe.Exp.Op)
	}
	role, m, err := e.matcher(pe.Exp.Word, role)
	if err != nil {
		return err
	}
	return e.eachWord(s, func(w string) (string, error) {
		switch role {
		case glob.RoleRemoveShortestPrefix, glob.RoleRemoveLongestPrefix:
			return m.RemovePrefix(w, role == glob.RoleRemoveLongestPrefix)
		default:
			return m.RemoveSuffix(w, role == glob.RoleRemoveLongestSuffix)
		}
	})
}

func (e *Evaluator) filter(pe *syntax.ParamExp, s *state) error {
	_, m, err := e.matcher(pe.Exp.Word, glob.RoleFilter)
	if err != nil {
		return err
	}
	keep := s.has('M')
	var words []string
	for _, w := range s.v.Words {
		_, ok, err := m.Whole(w)
		if err != nil {
			return err
		}
		if ok == keep {
			words = append(words, w)
		}
	}
	if !s.v.Array {
		s.v = scalar(first(words))
		return nil
	}
	s.v.Words = words
	return nil
}

func (e *Evaluator) caseOp(pe *syntax.ParamExp, s *state) error {
	var m glob.Matcher
	any := pe.Exp.Word == nil || len(pe.Exp.Word.Parts) == 0
	if !any {
		var err error
		if _, m, err = e.matcher(pe.Exp.Word, glob.RoleCaseConversion); err != nil {
			return err
		}
	}
	conv := unicode.ToUpper
	if pe.Exp.Op == syntax.LowerFirst || pe.Exp.Op == syntax.LowerAll {
		conv = unicode.ToLower
	}
	all := pe.Exp.Op == syntax.UpperAll || pe.Exp.Op == syntax.LowerAll
	return e.eachWord(s, func(w string) (string, error) {
		runes := []rune(w)
		for i, r := range runes {
			if i > 0 && !all {
				break
			}
			matches := any
			if !any {
				_, ok, err := m.Whole(string(r))
				if err != nil {
					return "", err
				}
				matches = ok
			}
			if matches {
				runes[i] = conv(r)
			}
		}
		return string(runes), nil
	})
}

func (e *Evaluator) replace(pe *syntax.ParamExp, s *state) error {
	role := glob.RoleReplaceFirst
	switch {
	case pe.Repl.Whole:
		role = glob.RoleReplaceWhole
	case pe.Repl.All:
		role = glob.RoleReplaceAll
	}
	role, m, err := e.matcher(pe.Repl.Orig, role)
	if err != nil {
		return err
	}
	anchoring := map[glob.Role]glob.Anchoring{
		glob.RoleReplaceFirst: glob.Anywhere, glob.RoleReplaceAll: glob.Anywhere,
		glob.RoleReplacePrefix: glob.AtStart, glob.RoleReplaceSuffix: glob.AtEnd, glob.RoleReplaceWhole: glob.WholeValue,
	}[role]
	return e.eachWord(s, func(w string) (string, error) {
		var replErr error
		out, err := m.Replace(w, anchoring, role == glob.RoleReplaceAll, func(text string, found glob.Match) string {
			r, err := e.replacement(pe.Repl.With, text, found)
			if err != nil {
				replErr = err
			}
			return r
		})
		if replErr != nil {
			return "", replErr
		}
		return out, err
	})
}

func (e *Evaluator) replacement(w *syntax.Word, text string, found glob.Match) (string, error) {
	if w == nil {
		return "", nil
	}
	matched := text[found.Start:found.End]
	if e.zsh() {
		saved := e.overlay
		defer func() { e.overlay = saved }()
		e.overlay = zshMatchVariables(text, found)
		return e.word(w)
	}
	if e.tree.Dialect == dialect.Ksh && slices.ContainsFunc(w.Parts, hasBackslash) {
		return "", unsupported("a ksh replacement with backslashes, which can refer to subpatterns")
	}
	if e.tree.Dialect != dialect.Bash || e.opts.NoPatsubAmp {
		return e.word(w)
	}
	var sb strings.Builder
	for _, part := range w.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			s, err := e.part(part, false)
			if err != nil {
				return "", err
			}
			sb.WriteString(s)
			continue
		}
		for i := 0; i < len(lit.Value); i++ {
			switch c := lit.Value[i]; {
			case c == '\\' && i+1 < len(lit.Value):
				i++
				sb.WriteByte(lit.Value[i])
			case c == '&':
				sb.WriteString(matched)
			default:
				sb.WriteByte(c)
			}
		}
	}
	return sb.String(), nil
}

func hasBackslash(part syntax.WordPart) bool {
	lit, ok := part.(*syntax.Lit)
	return ok && strings.Contains(lit.Value, `\`)
}

func zshMatchVariables(text string, found glob.Match) map[string]Value {
	position := func(byteOffset int) string {
		return strconv.Itoa(utf8.RuneCountInString(text[:byteOffset]) + 1)
	}
	match := Value{Array: true}
	var begins, ends []string
	for _, c := range found.Captures {
		if c[0] < 0 {
			match.Words = append(match.Words, "")
			begins, ends = append(begins, "-1"), append(ends, "-1")
			continue
		}
		match.Words = append(match.Words, text[c[0]:c[1]])
		begins = append(begins, position(c[0]))
		ends = append(ends, strconv.Itoa(utf8.RuneCountInString(text[:c[1]])))
	}
	return map[string]Value{
		"MATCH":  scalar(text[found.Start:found.End]),
		"MBEGIN": scalar(position(found.Start)),
		"MEND":   scalar(strconv.Itoa(utf8.RuneCountInString(text[:found.End]))),
		"match":  match,
		"mbegin": {Words: begins, Array: true},
		"mend":   {Words: ends, Array: true},
	}
}

func (e *Evaluator) length(s *state) {
	if s.v.Array && !s.has('c') {
		s.v = scalar(strconv.Itoa(len(s.v.Words)))
		return
	}
	s.v = scalar(strconv.Itoa(utf8.RuneCountInString(s.v.joined(" "))))
}

func (e *Evaluator) zshFlags(pe *syntax.ParamExp, s *state) error {
	if !e.zsh() {
		return nil
	}
	sep, joins := s.arg('j')
	if s.has('F') {
		sep, joins = "\n", true
	}
	splitSep, splits := s.arg('s')
	if s.has('f') {
		splitSep, splits = "\n", true
	}
	if pe.Split == syntax.OptOn && !splits {
		s.v = Value{Words: strings.Fields(s.v.joined(" ")), Array: true}
	}
	if joins || splits && s.v.Array {
		if !joins {
			sep = " "
		}
		s.v = scalar(s.v.joined(sep))
	}
	if splits {
		parts := strings.Split(s.v.joined(""), splitSep)
		s.v = Value{Words: slices.DeleteFunc(parts, func(p string) bool { return p == "" && !s.has('@') }), Array: true}
	}
	for _, f := range s.flags {
		switch f.Name {
		case 'U':
			s.v.Words = mapWords(s.v.Words, strings.ToUpper)
		case 'L':
			s.v.Words = mapWords(s.v.Words, strings.ToLower)
		case 'C':
			s.v.Words = mapWords(s.v.Words, capitalize)
		}
	}
	if !s.v.Array {
		return nil
	}
	if s.has('u') {
		var unique []string
		for _, w := range s.v.Words {
			if !slices.Contains(unique, w) {
				unique = append(unique, w)
			}
		}
		s.v.Words = unique
	}
	if s.has('o') || s.has('O') {
		words, err := sorted(s.v.Words, s.has('i'), s.has('n'), s.has('O'))
		if err != nil {
			return err
		}
		s.v.Words = words
	}
	return nil
}

func mapWords(words []string, f func(string) string) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = f(w)
	}
	return out
}

func capitalize(s string) string {
	var sb strings.Builder
	start := true
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start {
				sb.WriteRune(unicode.ToUpper(r))
			} else {
				sb.WriteRune(unicode.ToLower(r))
			}
			start = false
			continue
		}
		sb.WriteRune(r)
		start = true
	}
	return sb.String()
}

func sorted(words []string, fold, numeric, descending bool) ([]string, error) {
	out := slices.Clone(words)
	if fold {
		out = mapWords(out, strings.ToLower)
	}
	if !collationFree(out) {
		return nil, unsupported("sorting that depends on the locale")
	}
	keys := make(map[string]string, len(out))
	for i, w := range words {
		keys[w] = out[i]
	}
	out = slices.Clone(words)
	slices.SortStableFunc(out, func(a, b string) int {
		c := strings.Compare(keys[a], keys[b])
		if numeric {
			c = naturalCompare(keys[a], keys[b])
		}
		if descending {
			return -c
		}
		return c
	})
	return out, nil
}

func collationFree(words []string) bool {
	var lower, upper bool
	for _, w := range words {
		for i := range len(w) {
			switch c := w[i]; {
			case c >= 'a' && c <= 'z':
				lower = true
			case c >= 'A' && c <= 'Z':
				upper = true
			case c < '0' || c > '9':
				return false
			}
		}
	}
	return !lower || !upper
}

func naturalCompare(a, b string) int {
	for a != "" && b != "" {
		da, db := digitRun(a), digitRun(b)
		if da > 0 && db > 0 {
			x, y := strings.TrimLeft(a[:da], "0"), strings.TrimLeft(b[:db], "0")
			if c := compareInt(int64(len(x)), int64(len(y))); c != 0 {
				return c
			}
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
			a, b = a[da:], b[db:]
			continue
		}
		if c := compareInt(int64(a[0]), int64(b[0])); c != 0 {
			return c
		}
		a, b = a[1:], b[1:]
	}
	return compareInt(int64(len(a)), int64(len(b)))
}

func digitRun(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}

func compareInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
