package parser

import (
	"fmt"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

type ScopeID int

const FileScope ScopeID = 0

type Scope struct {
	Span   Span
	Parent ScopeID
}

type Function struct {
	Name     string
	NameSpan Span
	Span     Span
	Body     ScopeID
	Within   ScopeID
	Doc      string
	Decl     *syntax.FuncDecl
}

type DefinitionKind uint8

const (
	DefAssign DefinitionKind = iota
	DefCommandEnv
	DefDeclare
	DefLoop
	DefArithmetic
	DefBuiltin
)

type Attr uint8

const (
	AttrArray Attr = 1 << iota
	AttrAssoc
	AttrExport
	AttrReadonly
	AttrInteger
	AttrNameref
)

func (a Attr) Has(flag Attr) bool { return a&flag != 0 }

type Definition struct {
	Name     string
	NameSpan Span
	Span     Span
	Kind     DefinitionKind
	Attrs    Attr
	Local    bool
	Within   ScopeID
	Value    *syntax.Word
}

type Reference struct {
	Name   string
	Span   Span
	Within ScopeID
}

type Command struct {
	Name     string
	NameSpan Span
	Span     Span
	Args     []*syntax.Word
	Within   ScopeID
}

type Redirect struct {
	Op     syntax.RedirOperator
	OpSpan Span
	Span   Span
	Target Span
}

type Heredoc struct {
	Delimiter string
	Quoted    bool
	StripTabs bool
	Span      Span
	Body      Span
}

type Comment struct {
	Span Span
	Text string
}

type Keyword struct {
	Name string
	Span Span
}

type Index struct {
	Scopes      []Scope
	Functions   []Function
	Definitions []Definition
	References  []Reference
	Commands    []Command
	Redirects   []Redirect
	Heredocs    []Heredoc
	Comments    []Comment
	Keywords    []Keyword

	definitionsByBinding map[binding][]int
	definitionsByScope   map[ScopeID][]int
	localScopes          map[string]map[ScopeID]bool
	functionsByName      map[string]int
}

type binding struct {
	name  string
	scope ScopeID
	local bool
}

type builder struct {
	text        string
	index       Index
	errs        []Error
	stack       []syntax.Node
	scopes      []ScopeID
	elseClauses map[*syntax.IfClause]bool
	writeOnly   map[*syntax.Word]bool
}

func buildIndex(file *syntax.File, text string) (Index, []Error) {
	b := &builder{
		text:        text,
		index:       Index{Scopes: []Scope{{Span: Span{Start: 0, End: len(text)}, Parent: FileScope}}},
		scopes:      []ScopeID{FileScope},
		elseClauses: map[*syntax.IfClause]bool{},
		writeOnly:   map[*syntax.Word]bool{},
	}
	syntax.Walk(file, b.visit)
	slices.SortStableFunc(b.index.Keywords, func(x, y Keyword) int { return x.Span.Start - y.Span.Start })
	b.index.buildLookups()
	return b.index, b.errs
}

func (b *builder) visit(node syntax.Node) bool {
	if node == nil {
		top := b.stack[len(b.stack)-1]
		b.stack = b.stack[:len(b.stack)-1]
		if _, ok := top.(*syntax.FuncDecl); ok {
			b.scopes = b.scopes[:len(b.scopes)-1]
		}
		return true
	}
	b.enter(node)
	b.stack = append(b.stack, node)
	return true
}

func (b *builder) scope() ScopeID { return b.scopes[len(b.scopes)-1] }

func (b *builder) parent() syntax.Node {
	if len(b.stack) == 0 {
		return nil
	}
	return b.stack[len(b.stack)-1]
}

func (b *builder) enter(node syntax.Node) {
	b.recoveredClosers(node)
	b.keywords(node)
	switch n := node.(type) {
	case *syntax.FuncDecl:
		b.function(n)
	case *syntax.CallExpr:
		b.call(n)
	case *syntax.DeclClause:
		b.declaration(n)
	case *syntax.ForClause:
		b.forClause(n)
	case *syntax.ParamExp:
		b.paramExp(n)
	case *syntax.Word:
		b.arithmeticWord(n)
	case *syntax.BinaryArithm:
		b.arithmeticAssign(n)
	case *syntax.UnaryArithm:
		if n.Op == syntax.Inc || n.Op == syntax.Dec {
			b.arithmeticDefinition(n.X, n)
		}
	case *syntax.Redirect:
		b.redirect(n)
	case *syntax.Comment:
		b.index.Comments = append(b.index.Comments, Comment{Span: b.span(n), Text: n.Text})
	case *syntax.Stmt:
		b.recoveredStmt(n)
	case *syntax.Lit:
		b.recoveredLit(n)
	case *syntax.BinaryCmd:
		if n.Y != nil && n.Y.Position.IsRecovered() {
			b.errorAt(n.OpPos, fmt.Sprintf("expected a command after %#q", n.Op.String()))
		}
	}
}

func (b *builder) function(n *syntax.FuncDecl) {
	within := b.scope()
	body := ScopeID(len(b.index.Scopes))
	b.index.Scopes = append(b.index.Scopes, Scope{Span: b.span(n), Parent: within})
	names := n.Names
	if n.Name != nil {
		names = []*syntax.Lit{n.Name}
	}
	for _, name := range names {
		b.index.Functions = append(b.index.Functions, Function{
			Name:     name.Value,
			NameSpan: b.span(name),
			Span:     b.span(n),
			Body:     body,
			Within:   within,
			Doc:      docComment(b.text, offset(n.Pos())),
			Decl:     n,
		})
	}
	b.scopes = append(b.scopes, body)
}

func (b *builder) call(n *syntax.CallExpr) {
	kind := DefAssign
	if len(n.Args) > 0 {
		kind = DefCommandEnv
	}
	for _, assign := range n.Assigns {
		b.assign(assign, kind, 0, false)
	}
	if len(n.Args) == 0 {
		return
	}
	name, ok := Literal(n.Args[0])
	if !ok {
		return
	}
	b.index.Commands = append(b.index.Commands, Command{
		Name:     name,
		NameSpan: b.span(n.Args[0]),
		Span:     b.span(n),
		Args:     n.Args[1:],
		Within:   b.scope(),
	})
	if variant, ok := declVariants[name]; ok {
		b.declarationWords(variant, n.Args[1:])
	}
	b.builtinAssignments(name, n.Args[1:])
}

var optionsWithArgument = map[string]string{
	"read":      "adinNptu",
	"mapfile":   "dnOsuCc",
	"readarray": "dnOsuCc",
	"printf":    "v",
}

func (b *builder) builtinAssignments(name string, args []*syntax.Word) {
	argOpts, known := optionsWithArgument[name]
	if !known && name != "getopts" {
		return
	}
	var names []*syntax.Word
	var arrays []*syntax.Word
	for i := 0; i < len(args); i++ {
		value, ok := Literal(args[i])
		if !ok {
			continue
		}
		if value == "--" {
			names = append(names, args[i+1:]...)
			break
		}
		if strings.HasPrefix(value, "-") && len(value) > 1 && name != "getopts" {
			last := value[len(value)-1]
			if strings.IndexByte(argOpts, last) >= 0 && i+1 < len(args) {
				i++
				switch {
				case name == "read" && last == 'a':
					arrays = append(arrays, args[i])
				case name == "printf" && last == 'v':
					names = append(names, args[i])
				}
			}
			continue
		}
		names = append(names, args[i])
	}
	switch name {
	case "getopts":
		if len(names) >= 2 {
			b.wordDefinition(names[1], 0)
		}
	case "printf":
		if len(names) > 0 && len(args) > 0 {
			if first, _ := Literal(args[0]); first == "-v" {
				b.wordDefinition(names[0], 0)
			}
		}
	case "mapfile", "readarray":
		if len(names) > 0 {
			b.wordDefinition(names[len(names)-1], AttrArray)
		}
	case "read":
		for _, w := range names {
			b.wordDefinition(w, 0)
		}
	}
	for _, w := range arrays {
		b.wordDefinition(w, AttrArray)
	}
}

func (b *builder) wordDefinition(w *syntax.Word, attrs Attr) {
	value, ok := Literal(w)
	if !ok || !isName(value) || len(w.Parts) != 1 {
		return
	}
	span := b.span(w)
	b.index.Definitions = append(b.index.Definitions, Definition{
		Name:     value,
		NameSpan: span,
		Span:     span,
		Kind:     DefBuiltin,
		Attrs:    attrs,
		Within:   b.scope(),
	})
}

type declVariant struct {
	attrs  Attr
	local  bool
	scoped bool
}

var declVariants = map[string]declVariant{
	"declare":  {scoped: true},
	"typeset":  {scoped: true},
	"local":    {local: true},
	"export":   {attrs: AttrExport},
	"readonly": {attrs: AttrReadonly},
	"nameref":  {attrs: AttrNameref, scoped: true},
	"integer":  {attrs: AttrInteger, scoped: true},
	"float":    {scoped: true},
}

func (b *builder) declaration(n *syntax.DeclClause) {
	variant := declVariants[n.Variant.Value]
	b.index.Commands = append(b.index.Commands, Command{
		Name:     n.Variant.Value,
		NameSpan: b.span(n.Variant),
		Span:     b.span(n),
		Within:   b.scope(),
	})
	attrs, global := variant.attrs, false
	for _, arg := range n.Args {
		if arg.Name == nil {
			if flags, ok := Literal(arg.Value); ok {
				attrs, global = applyFlags(attrs, global, flags)
			}
			continue
		}
		b.assign(arg, DefDeclare, attrs, variant.isLocal(b.scope(), global))
	}
}

func (b *builder) declarationWords(variant declVariant, args []*syntax.Word) {
	attrs, global := variant.attrs, false
	for _, arg := range args {
		value, ok := Literal(arg)
		if !ok {
			continue
		}
		if strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+") {
			attrs, global = applyFlags(attrs, global, value)
			continue
		}
		name, _, _ := strings.Cut(value, "=")
		if !isName(name) {
			continue
		}
		start := offset(arg.Pos())
		b.index.Definitions = append(b.index.Definitions, Definition{
			Name:     name,
			NameSpan: Span{Start: start, End: start + len(name)},
			Span:     b.span(arg),
			Kind:     DefDeclare,
			Attrs:    attrs,
			Local:    variant.isLocal(b.scope(), global),
			Within:   b.scope(),
		})
	}
}

func (v declVariant) isLocal(scope ScopeID, global bool) bool {
	if v.local {
		return true
	}
	return v.scoped && scope != FileScope && !global
}

func applyFlags(attrs Attr, global bool, flags string) (Attr, bool) {
	if !strings.HasPrefix(flags, "-") {
		return attrs, global
	}
	for _, flag := range flags[1:] {
		switch flag {
		case 'a':
			attrs |= AttrArray
		case 'A':
			attrs |= AttrAssoc
		case 'x':
			attrs |= AttrExport
		case 'r':
			attrs |= AttrReadonly
		case 'i':
			attrs |= AttrInteger
		case 'n':
			attrs |= AttrNameref
		case 'g':
			global = true
		}
	}
	return attrs, global
}

func (b *builder) assign(n *syntax.Assign, kind DefinitionKind, attrs Attr, local bool) {
	if n.Name == nil || !isName(n.Name.Value) {
		return
	}
	if n.Array != nil && !attrs.Has(AttrAssoc) {
		attrs |= AttrArray
	}
	b.index.Definitions = append(b.index.Definitions, Definition{
		Name:     n.Name.Value,
		NameSpan: b.span(n.Name),
		Span:     b.span(n),
		Kind:     kind,
		Attrs:    attrs,
		Local:    local,
		Within:   b.scope(),
		Value:    assignedWord(n),
	})
}

func (b *builder) forClause(n *syntax.ForClause) {
	iter, ok := n.Loop.(*syntax.WordIter)
	if !ok || iter.Name == nil || !isName(iter.Name.Value) {
		return
	}
	b.index.Definitions = append(b.index.Definitions, Definition{
		Name:     iter.Name.Value,
		NameSpan: b.span(iter.Name),
		Span:     b.span(iter),
		Kind:     DefLoop,
		Within:   b.scope(),
	})
}

func (b *builder) paramExp(n *syntax.ParamExp) {
	if n.Param == nil || n.Names != 0 || !isName(n.Param.Value) || !n.Param.ValuePos.IsValid() {
		return
	}
	b.reference(n.Param.Value, b.span(n.Param))
}

func (b *builder) reference(name string, span Span) {
	b.index.References = append(b.index.References, Reference{Name: name, Span: span, Within: b.scope()})
}

func (b *builder) arithmeticWord(w *syntax.Word) {
	if !isArithmetic(b.parent()) || b.writeOnly[w] {
		return
	}
	if name, ok := arithmeticName(w); ok {
		b.reference(name, b.span(w))
	}
}

func isArithmetic(node syntax.Node) bool {
	switch node.(type) {
	case *syntax.BinaryArithm, *syntax.UnaryArithm, *syntax.ParenArithm, *syntax.FlagsArithm,
		*syntax.ArithmExp, *syntax.ArithmCmd, *syntax.CStyleLoop, *syntax.LetClause:
		return true
	}
	return false
}

func arithmeticName(expr syntax.ArithmExpr) (string, bool) {
	w, ok := expr.(*syntax.Word)
	if !ok || len(w.Parts) != 1 {
		return "", false
	}
	lit, ok := w.Parts[0].(*syntax.Lit)
	if !ok || !isName(lit.Value) {
		return "", false
	}
	return lit.Value, true
}

func (b *builder) arithmeticAssign(n *syntax.BinaryArithm) {
	switch n.Op {
	case syntax.Assgn, syntax.AddAssgn, syntax.SubAssgn, syntax.MulAssgn, syntax.QuoAssgn,
		syntax.RemAssgn, syntax.AndAssgn, syntax.OrAssgn, syntax.XorAssgn, syntax.ShlAssgn,
		syntax.ShrAssgn, syntax.AndBoolAssgn, syntax.OrBoolAssgn, syntax.XorBoolAssgn, syntax.PowAssgn:
	default:
		return
	}
	if w, ok := n.X.(*syntax.Word); ok && n.Op == syntax.Assgn {
		b.writeOnly[w] = true
	}
	b.arithmeticDefinition(n.X, n)
}

func (b *builder) arithmeticDefinition(target syntax.ArithmExpr, expr syntax.ArithmExpr) {
	name, ok := arithmeticName(target)
	if !ok {
		return
	}
	b.index.Definitions = append(b.index.Definitions, Definition{
		Name:     name,
		NameSpan: b.span(target),
		Span:     b.span(expr),
		Kind:     DefArithmetic,
		Within:   b.scope(),
	})
}

func (b *builder) redirect(n *syntax.Redirect) {
	opStart := offset(n.OpPos)
	opSpan := Span{Start: opStart, End: opStart + len(n.Op.String())}
	target := Span{Start: opSpan.End, End: opSpan.End}
	if n.Word != nil && len(n.Word.Parts) > 0 && n.Word.Pos().IsValid() {
		target = b.span(n.Word)
	}
	b.index.Redirects = append(b.index.Redirects, Redirect{
		Op:     n.Op,
		OpSpan: opSpan,
		Span:   Span{Start: offset(n.Pos()), End: target.End},
		Target: target,
	})
	if n.Op != syntax.Hdoc && n.Op != syntax.DashHdoc {
		return
	}
	delimiter, quoted := heredocDelimiter(n.Word)
	body := Span{Start: target.End, End: target.End}
	if n.Hdoc != nil && len(n.Hdoc.Parts) > 0 {
		body = b.span(n.Hdoc)
		body.End = max(body.Start, strings.LastIndexByte(b.text[:body.End], '\n')+1)
	}
	b.index.Heredocs = append(b.index.Heredocs, Heredoc{
		Delimiter: delimiter,
		Quoted:    quoted,
		StripTabs: n.Op == syntax.DashHdoc,
		Span:      Span{Start: opSpan.Start, End: target.End},
		Body:      body,
	})
}

func heredocDelimiter(w *syntax.Word) (string, bool) {
	if w == nil {
		return "", false
	}
	quoted := false
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			quoted = quoted || strings.Contains(p.Value, `\`)
		default:
			quoted = true
		}
	}
	delimiter, _ := Literal(w)
	return delimiter, quoted
}

func (b *builder) recoveredStmt(n *syntax.Stmt) {
	if !n.Position.IsRecovered() {
		return
	}
	switch p := b.parent().(type) {
	case *syntax.BinaryCmd:
		return
	case *syntax.IfClause:
		b.emptyBody(p.ThenPos, p.Position, "then")
		return
	case *syntax.WhileClause:
		b.emptyBody(p.DoPos, p.WhilePos, "do")
		return
	case *syntax.ForClause:
		b.emptyBody(p.DoPos, p.ForPos, "do")
		return
	case *syntax.Block:
		b.emptyBody(p.Lbrace, p.Lbrace, "{")
		return
	case *syntax.Subshell:
		b.emptyBody(p.Lparen, p.Lparen, "(")
		return
	}
	b.errorAt(b.anchor(), "expected a command")
}

func (b *builder) emptyBody(keyword, fallback syntax.Pos, name string) {
	if keyword.IsValid() {
		b.errorAt(keyword, fmt.Sprintf("expected a command after %#q", name))
		return
	}
	b.errorAt(fallback, "expected a command")
}

func (b *builder) recoveredLit(n *syntax.Lit) {
	if !n.ValuePos.IsRecovered() {
		return
	}
	for _, node := range slices.Backward(b.stack) {
		switch p := node.(type) {
		case *syntax.Redirect:
			b.errorAt(p.OpPos, fmt.Sprintf("expected a word after %#q", p.Op.String()))
			return
		case *syntax.BinaryTest:
			b.errorAt(p.OpPos, fmt.Sprintf("expected a word after %#q", p.Op.String()))
			return
		case *syntax.UnaryTest:
			b.errorAt(p.OpPos, fmt.Sprintf("expected a word after %#q", p.Op.String()))
			return
		}
	}
	b.errorAt(b.anchor(), "expected a word")
}

func (b *builder) anchor() syntax.Pos {
	for _, node := range slices.Backward(b.stack) {
		if pos := node.Pos(); pos.IsValid() {
			return pos
		}
	}
	return syntax.Pos{}
}

func (b *builder) recoveredClosers(node syntax.Node) {
	switch n := node.(type) {
	case *syntax.IfClause:
		opener := "if"
		if b.elseClauses[n] {
			opener = "elif"
		}
		if n.Else != nil {
			b.elseClauses[n.Else] = true
		}
		b.missing(n.Position, n.ThenPos, opener, "then")
		if !b.elseClauses[n] {
			b.missing(n.Position, n.FiPos, opener, "fi")
		}
	case *syntax.WhileClause:
		opener := "while"
		if n.Until {
			opener = "until"
		}
		b.missing(n.WhilePos, n.DoPos, opener, "do")
		b.missing(n.WhilePos, n.DonePos, opener, "done")
	case *syntax.ForClause:
		opener := "for"
		if n.Select {
			opener = "select"
		}
		open, closer := "do", "done"
		if n.Braces {
			open, closer = "{", "}"
		}
		b.missing(n.ForPos, n.DoPos, opener, open)
		b.missing(n.ForPos, n.DonePos, opener, closer)
	case *syntax.CaseClause:
		open, closer := "in", "esac"
		if n.Braces {
			open, closer = "{", "}"
		}
		b.missing(n.Case, n.In, "case", open)
		b.missing(n.Case, n.Esac, "case", closer)
	case *syntax.Block:
		b.missing(n.Lbrace, n.Rbrace, "{", "}")
	case *syntax.Subshell:
		b.missing(n.Lparen, n.Rparen, "(", ")")
	case *syntax.CmdSubst:
		opener, closer := "$(", ")"
		switch {
		case n.Backquotes:
			opener, closer = "`", "`"
		case n.TempFile, n.ReplyVar:
			opener, closer = "${", "}"
		}
		b.missing(n.Left, n.Right, opener, closer)
	case *syntax.ArithmExp:
		opener, closer := "$((", "))"
		if n.Bracket {
			opener, closer = "$[", "]"
		}
		b.missing(n.Left, n.Right, opener, closer)
	case *syntax.ArithmCmd:
		b.missing(n.Left, n.Right, "((", "))")
	case *syntax.ParamExp:
		if !n.Short {
			b.missing(n.Dollar, n.Rbrace, "${", "}")
		}
	case *syntax.DblQuoted:
		b.missing(n.Left, n.Right, `"`, `"`)
	case *syntax.SglQuoted:
		b.missing(n.Left, n.Right, "'", "'")
	case *syntax.TestClause:
		b.missing(n.Left, n.Right, "[[", "]]")
	case *syntax.ProcSubst:
		b.missing(n.OpPos, n.Rparen, n.Op.String(), ")")
	case *syntax.ArrayExpr:
		b.missing(n.Lparen, n.Rparen, "(", ")")
	case *syntax.ParenTest:
		b.missing(n.Lparen, n.Rparen, "(", ")")
	case *syntax.ParenArithm:
		b.missing(n.Lparen, n.Rparen, "(", ")")
	}
}

func (b *builder) missing(anchor, closer syntax.Pos, opener, token string) {
	if closer.IsRecovered() {
		b.errorAt(anchor, fmt.Sprintf("missing %#q to match %#q", token, opener))
	}
}

func (b *builder) errorAt(pos syntax.Pos, message string) {
	if !pos.IsValid() {
		return
	}
	b.errs = append(b.errs, Error{Span: tokenSpan(b.text, offset(pos)), Kind: ErrSyntax, Message: message})
}

func (b *builder) span(n syntax.Node) Span {
	start := offset(n.Pos())
	end := n.End()
	if !end.IsValid() {
		return Span{Start: start, End: len(b.text)}
	}
	return Span{Start: start, End: max(start, offset(end))}
}

func isName(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case c == '_', 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z':
		case '0' <= c && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func docComment(text string, start int) string {
	lineStart := strings.LastIndexByte(text[:start], '\n') + 1
	var lines []string
	for lineStart > 0 {
		prevStart := strings.LastIndexByte(text[:lineStart-1], '\n') + 1
		line := strings.TrimSpace(text[prevStart : lineStart-1])
		if !strings.HasPrefix(line, "#") || strings.HasPrefix(line, "#!") {
			break
		}
		lineStart = prevStart
		body := strings.TrimPrefix(strings.TrimPrefix(line, "#"), " ")
		if strings.HasPrefix(body, "shellcheck ") {
			continue
		}
		lines = append(lines, body)
	}
	slices.Reverse(lines)
	return strings.Join(lines, "\n")
}

func (b *builder) keywords(node syntax.Node) {
	switch n := node.(type) {
	case *syntax.IfClause:
		switch {
		case b.elseClauses[n] && !n.ThenPos.IsValid():
			b.keyword("else", n.Position)
		case b.elseClauses[n]:
			b.keyword("elif", n.Position)
		default:
			b.keyword("if", n.Position)
			b.keyword("fi", n.FiPos)
		}
		b.keyword("then", n.ThenPos)
	case *syntax.WhileClause:
		name := "while"
		if n.Until {
			name = "until"
		}
		b.keyword(name, n.WhilePos)
		b.keyword("do", n.DoPos)
		b.keyword("done", n.DonePos)
	case *syntax.ForClause:
		name := "for"
		if n.Select {
			name = "select"
		}
		b.keyword(name, n.ForPos)
		if iter, ok := n.Loop.(*syntax.WordIter); ok {
			b.keyword("in", iter.InPos)
		}
		if !n.Braces {
			b.keyword("do", n.DoPos)
			b.keyword("done", n.DonePos)
		}
	case *syntax.CaseClause:
		b.keyword("case", n.Case)
		if !n.Braces {
			b.keyword("in", n.In)
			b.keyword("esac", n.Esac)
		}
	case *syntax.FuncDecl:
		if n.RsrvWord {
			b.keyword("function", n.Position)
		}
	case *syntax.TestClause:
		b.keyword("[[", n.Left)
		b.keyword("]]", n.Right)
	case *syntax.TimeClause:
		b.keyword("time", n.Time)
	case *syntax.CoprocClause:
		b.keyword("coproc", n.Coproc)
	case *syntax.Stmt:
		if n.Negated && n.Position.IsValid() {
			b.keyword("!", n.Position)
		}
	}
}

func (b *builder) keyword(name string, pos syntax.Pos) {
	if !pos.IsValid() {
		return
	}
	start := offset(pos)
	b.index.Keywords = append(b.index.Keywords, Keyword{Name: name, Span: Span{Start: start, End: start + len(name)}})
}

func assignedWord(n *syntax.Assign) *syntax.Word {
	if n.Naked || n.Append || n.Index != nil || n.Value == nil || len(n.Value.Parts) == 0 {
		return nil
	}
	return n.Value
}

func (ix *Index) buildLookups() {
	ix.definitionsByBinding = make(map[binding][]int, len(ix.Definitions))
	ix.definitionsByScope = make(map[ScopeID][]int, len(ix.Scopes))
	ix.localScopes = map[string]map[ScopeID]bool{}
	for i, def := range ix.Definitions {
		key := binding{name: def.Name}
		if def.Local {
			key.scope, key.local = def.Within, true
			if ix.localScopes[def.Name] == nil {
				ix.localScopes[def.Name] = map[ScopeID]bool{}
			}
			ix.localScopes[def.Name][def.Within] = true
		}
		ix.definitionsByBinding[key] = append(ix.definitionsByBinding[key], i)
		ix.definitionsByScope[def.Within] = append(ix.definitionsByScope[def.Within], i)
	}
	ix.functionsByName = make(map[string]int, len(ix.Functions))
	for i, fn := range ix.Functions {
		if _, seen := ix.functionsByName[fn.Name]; !seen {
			ix.functionsByName[fn.Name] = i
		}
	}
}
