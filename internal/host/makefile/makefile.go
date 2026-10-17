package makefile

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/projection"
	"github.com/stubbedev/shell-lsp/internal/scan"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

const (
	defaultPrefix = '\t'
	recipeFlags   = " \t@-+"
	oneShell      = ".ONESHELL"
	posixTarget   = ".POSIX"
	exportAll     = ".EXPORT_ALL_VARIABLES"
	defaultFlags  = "-c"
	posixFlags    = "-ec"
)

var (
	conditionals = []string{"ifeq", "ifneq", "ifdef", "ifndef", "else", "endif"}
	modifiers    = []string{"export", "override", "private"}
	directives   = []string{"include", "-include", "sinclude", "vpath", "undefine", "unexport", "export", "load", "-load"}
	assignOps    = []string{":::=", "::=", ":=", "+=", "?=", "!=", "="}
	recursiveOps = []string{"=", "?=", ""}
)

const whitespace = " \t\f\v"

type define struct {
	assignment *Assignment
	depth      int
}

type reader struct {
	text     string
	file     *File
	prefix   byte
	rule     *Rule
	ignored  bool
	defining *define
	open     []*Conditional
	oneShell bool
	shell    string
	shellSet bool
	flags    string
	posix    bool
	evidence string
}

func Parse(text string) *File {
	p := &reader{text: text, prefix: defaultPrefix, file: &File{Text: text}}
	if nul := strings.IndexByte(text, 0); nul >= 0 {
		p.fail(ErrSyntax, Span{Start: nul, End: nul + 1}, "make reads lines as C strings: a NUL byte drops the rest of its line and joins the next one onto it")
	}
	p.parse()
	return p.file
}

func Project(text string) (*projection.Map, dialect.Detection) {
	m, detection, _ := project(Parse(text))
	return m, detection
}

func project(f *File) (*projection.Map, dialect.Detection, bool) {
	p := f.settings()
	b := projection.NewBuilder(f.Text)
	detection, ok := p.dialect()
	if !ok {
		return b.Map(), detection, false
	}
	b.Region(encode)
	prelude := p.prelude(f)
	exprs := map[int]Expr{}
	for _, rule := range f.Rules {
		for _, l := range rule.Recipe {
			exprs[l.Span.Start] = l.Expr
		}
	}
	body := func(l projection.RecipeLine) { p.content(b, exprs[l.Start], l.Command) }
	shells := b.LineShells(prelude)
	for _, rule := range f.Rules {
		lines := make([]projection.RecipeLine, 0, len(rule.Recipe))
		for _, l := range rule.Recipe {
			lines = append(lines, p.recipeLine(l))
		}
		if p.oneShell {
			b.OneShell(prelude, slices.DeleteFunc(lines, projection.RecipeLine.Empty), body)
			continue
		}
		for _, l := range lines {
			shells.Add(l, body)
		}
	}
	return b.Map(), detection, true
}

func (f *File) settings() *reader {
	p := &reader{text: f.Text, prefix: defaultPrefix, file: f}
	for _, a := range f.Assignments {
		if len(a.Targets) == 0 && !a.Define {
			p.setting(a)
		}
	}
	for _, r := range f.Rules {
		for _, t := range r.Targets {
			p.oneShell = p.oneShell || t.Text == oneShell
			p.posix = p.posix || t.Text == posixTarget
		}
	}
	return p
}

func encode(_ string, _, _ int, text string) (string, bool) {
	if strings.ContainsRune(text, '\n') {
		return "", false
	}
	return strings.ReplaceAll(text, "$", "$$"), true
}

func (p *reader) dialect() (dialect.Detection, bool) {
	if !p.shellSet {
		return dialect.Detection{Dialect: dialect.Posix, Source: dialect.SourceHostDefault, Evidence: "make"}, true
	}
	found := dialect.Detection{Dialect: dialect.Posix, Source: dialect.SourceHostSetting, Evidence: p.evidence}
	if strings.ContainsRune(p.shell, '$') {
		return found, false
	}
	d, ok := dialect.FromShebang("#!" + p.shell)
	if !ok {
		return found, false
	}
	found.Dialect = d
	return found, true
}

func (p *reader) parse() {
	scan.Forward(0, len(p.text), func(pos int) int {
		end := p.logicalEnd(pos)
		line := p.text[pos:end]
		switch {
		case p.defining != nil:
			p.defineBody(pos, end, line)
		case p.ignored && line != "" && line[0] == p.prefix:
		case p.rule != nil && line != "" && line[0] == p.prefix:
			first := len(p.file.Errors)
			p.rule.Recipe = append(p.rule.Recipe, RecipeLine{Span: Span{Start: pos + 1, End: end}, Expr: p.expr(pos+1, end)})
			p.rule.Span.End = end
			if p.rule.Pattern() {
				p.convert(first, ErrPatternUnterminated, "; make fails when it uses this pattern rule")
			}
		case blankText(line):
		case strings.HasPrefix(strings.TrimLeft(line, " \t"), "#"):
			p.comment(pos+len(line)-len(strings.TrimLeft(line, " \t")), end)
		default:
			p.directive(pos, end)
		}
		return end + 1
	})
	if p.defining != nil {
		a := p.defining.assignment
		p.fail(ErrDefine, a.Name.Span, "`define "+a.Name.Text+"` has no `endef`")
		first := len(p.file.Errors)
		a.Value = p.expr(a.Value.Span.Start, len(p.text))
		p.deferred(first, a)
		a.Span.End = len(p.text)
	}
	colons := map[string]*Rule{}
	for _, r := range p.file.Rules {
		if r.Grouped && len(r.Recipe) == 0 {
			p.fail(ErrSyntax, r.Header, "grouped targets `&:` need a recipe")
		}
		for _, t := range r.Targets {
			if t.Expr.Dynamic() {
				continue
			}
			if prior, ok := colons[t.Text]; ok && prior.DoubleColon != r.DoubleColon {
				p.fail(ErrSyntax, t.Span, fmt.Sprintf("target `%s` has both `:` and `::` rules; the first is on line %d", t.Text, strings.Count(p.text[:prior.Span.Start], "\n")+1))
				continue
			}
			colons[t.Text] = r
		}
	}
	for _, c := range p.open {
		line := strings.Count(p.text[:c.Open.Keyword.Span.Start], "\n") + 1
		p.fail(ErrConditional, Span{Start: len(p.text), End: len(p.text)}, fmt.Sprintf("the `%s` on line %d has no `endif`", c.Open.Keyword.Text, line))
		c.Broken = true
		c.Span.End = len(p.text)
	}
}

func (p *reader) fail(kind ErrorKind, span Span, message string) {
	p.file.Errors = append(p.file.Errors, Error{Span: span, Message: message, Kind: kind})
}

func (p *reader) comment(start, end int) {
	p.file.Comments = append(p.file.Comments, Span{Start: start, End: end})
}

func (p *reader) logicalEnd(start int) int {
	end := len(p.text)
	scan.Forward(start, len(p.text), func(pos int) int {
		lineEnd := textline.End(p.text, pos)
		if lineEnd == len(p.text) || !scan.Continued(p.text[pos:lineEnd]) {
			end = lineEnd
			return len(p.text)
		}
		return lineEnd + 1
	})
	return end
}

func (p *reader) defineBody(start, end int, line string) {
	word := firstWord(strings.TrimLeft(line, " \t"))
	switch word {
	case "endef":
		if p.defining.depth > 0 {
			p.defining.depth--
			return
		}
		a := p.defining.assignment
		first := len(p.file.Errors)
		a.Value = p.expr(a.Value.Span.Start, max(start-1, a.Value.Span.Start))
		p.deferred(first, a)
		a.End = Span{Start: start + len(line) - len(strings.TrimLeft(line, " \t")), End: end}
		a.Span.End = end
		p.defining = nil
	case "define":
		p.defining.depth++
	}
}

func (p *reader) keyword(start int, s string) Word {
	return Word{Text: s, Span: Span{Start: start, End: start + len(s)}}
}

func (p *reader) word(start int, s string) Word {
	return Word{Text: s, Span: Span{Start: start, End: start + len(s)}, Expr: p.expr(start, start+len(s))}
}

func (p *reader) expr(start, end int) Expr {
	return exprParser{text: p.text, errors: &p.file.Errors}.parse(start, end)
}

func (p *reader) directive(start, end int) {
	body := p.text[start:end]
	cut := commentStart(body)
	if cut < len(body) {
		p.comment(start+cut, end)
	}
	body = trimTrailing(body[:cut])
	s := strings.TrimLeft(body, whitespace)
	at := start + len(body) - len(s)
	word := firstWord(s)
	if named(s, word) {
		word = ""
	}
	if slices.Contains(conditionals, word) {
		p.conditional(at, s, word)
		return
	}
	wasRule := p.rule != nil || p.ignored
	p.rule, p.ignored = nil, false
	var mods []Word
	for slices.Contains(modifiers, word) && modifies(strings.TrimLeft(s[len(word):], " \t")) {
		mods = append(mods, p.keyword(at, word))
		rest := strings.TrimLeft(s[len(word):], " \t")
		at += len(s) - len(rest)
		s = rest
		if word = firstWord(s); named(s, word) {
			word = ""
		}
	}
	switch {
	case word == "define":
		p.define(at, s, mods, Span{Start: start, End: end})
		return
	case word == "endef" && !hasOperator(s):
		p.fail(ErrDefine, p.keyword(at, word).Span, "`endef` without a `define`")
		return
	case slices.Contains(directives, word):
		p.plainDirective(at, s, word, mods)
		return
	}
	i, rule := operator(s)
	switch {
	case body != "" && body[0] == p.prefix && (i < 0 || rule):
		p.fail(ErrRecipeOutsideRule, Span{Start: start, End: start + len(body)}, "a recipe line comes before the first rule")
	case i >= 0 && rule && topLevel(s, 0, ";") < i:
		p.fail(ErrSyntax, Span{Start: at, End: at + len(s)}, "missing separator: a `;` comes before the rule's `:`")
	case i >= 0 && rule:
		p.parseRule(s, i, at, end)
	case i >= 0:
		p.assign(s, i, at, mods, nil)
	case strings.HasPrefix(s, "$"):
		e := p.expr(at, at+len(s))
		p.file.Expressions = append(p.file.Expressions, e)
		if text, ok := p.literalText(p.expr(at, at+topLevel(s, 0, ";"))); ok {
			p.fail(ErrSyntax, text, "missing separator: this line expands to text that is no rule, assignment or directive")
		} else {
			p.ignored = !p.empty(e)
		}
	case wasRule && (body[0] == ' ' || body[0] == '\t'):
		p.fail(ErrRecipePrefix, Span{Start: start, End: start + len(body)}, "a recipe line must start with "+prefixName(p.prefix))
	default:
		e := p.expr(at, at+len(s))
		p.file.Expressions = append(p.file.Expressions, e)
		if _, ok := p.literalText(e); ok {
			p.fail(ErrSyntax, Span{Start: at, End: at + len(s)}, "missing separator: this line is no rule, assignment or directive")
		} else {
			p.ignored = !p.empty(e)
		}
	}
}

func (p *reader) deferred(first int, a *Assignment) {
	a.Deferred = len(a.Targets) == 0 && (slices.Contains(recursiveOps, a.Op) || a.Op == "+=" && p.appendsRecursively(a))
	if !a.Deferred {
		return
	}
	p.convert(first, ErrDeferredUnterminated, "; make fails when it expands `"+a.Name.Text+"`")
}

func (p *reader) appendsRecursively(a *Assignment) bool {
	for _, prior := range slices.Backward(p.file.Assignments) {
		if prior != a && prior.Name.Text == a.Name.Text && len(prior.Targets) == len(a.Targets) {
			return prior.Deferred
		}
	}
	return true
}

func (p *reader) convert(first int, kind ErrorKind, why string) {
	for i := first; i < len(p.file.Errors); i++ {
		if e := &p.file.Errors[i]; e.Kind == ErrUnterminated {
			e.Kind = kind
			e.Message += why
		}
	}
}

func (p *reader) known(words []Word) int {
	n := 0
	for _, w := range words {
		if _, ok := p.literalText(w.Expr); ok {
			n++
		}
	}
	return n
}

func blankText(s string) bool {
	return strings.Trim(strings.ReplaceAll(s, "\\\n", " "), whitespace+"\n") == ""
}

func (p *reader) empty(e Expr) bool {
	return !slices.ContainsFunc(e.Nodes, func(n Node) bool {
		return n.Kind == NodeText && !blankText(p.text[n.Span.Start:n.Span.End]) || n.Kind != NodeText && !p.emptyReference(n)
	})
}

func (p *reader) resolvedName(e Expr) (string, bool) {
	var sb strings.Builder
	for _, n := range e.Nodes {
		switch {
		case n.Kind == NodeText:
			sb.WriteString(p.text[n.Span.Start:n.Span.End])
		case !p.emptyReference(n):
			return "", false
		}
	}
	return sb.String(), true
}

func (p *reader) emptyReference(n Node) bool {
	name, ok := p.resolvedName(n.Name)
	if n.Kind != NodeReference || n.Unbalanced || !ok || n.Braced && strings.ContainsAny(name, "(){}") {
		return false
	}
	name = strings.TrimSpace(name)
	if _, builtin := Builtins.Lookup(model.KindVariable, name); builtin && !automatic(name) {
		return false
	}
	return !slices.ContainsFunc(p.file.Assignments, func(a *Assignment) bool { return a.Name.Text == name })
}

func (p *reader) spaced(e Expr) bool {
	return slices.ContainsFunc(e.Nodes, func(n Node) bool {
		return n.Kind == NodeText && strings.ContainsAny(p.text[n.Span.Start:n.Span.End], " \t")
	})
}

func (p *reader) literalText(e Expr) (Span, bool) {
	if slices.ContainsFunc(e.Nodes, func(n Node) bool { return n.Kind != NodeText && !p.emptyReference(n) }) {
		return Span{}, false
	}
	for _, n := range e.Nodes {
		if n.Kind == NodeText && !blankText(p.text[n.Span.Start:n.Span.End]) {
			return n.Span, true
		}
	}
	return Span{}, false
}

func hasOperator(s string) bool {
	i, _ := operator(s)
	return i >= 0
}

func targetAssignment(rest string) bool {
	rest = strings.TrimLeft(rest, " \t")
	for word := firstWord(rest); slices.Contains(modifiers, word) && strings.TrimSpace(rest[len(word):]) != ""; word = firstWord(rest) {
		rest = strings.TrimLeft(rest[len(word):], " \t")
	}
	i, isRule := operator(rest)
	if isRule || i < 0 {
		return false
	}
	name, opStart := assignmentName(rest, i)
	if !strings.ContainsAny(name, " \t") {
		return true
	}
	return rest[opStart] != ':' && topLevel(rest, i+1, ":") == len(rest)
}

func (p *reader) percent(w Word) bool {
	return slices.ContainsFunc(w.Expr.Nodes, func(n Node) bool {
		return n.Kind == NodeText && strings.Contains(p.text[n.Span.Start:n.Span.End], "%")
	})
}

func (p *reader) mixedTargets(targets []Word) {
	if len(targets) == 0 || !p.percent(targets[0]) {
		return
	}
	for _, t := range targets[1:] {
		if _, normal := p.literalText(t.Expr); normal && !p.percent(t) {
			p.fail(ErrSyntax, t.Span, fmt.Sprintf("normal target `%s` follows the pattern target `%s`; a rule is either a pattern rule or a normal one", t.Text, targets[0].Text))
			return
		}
	}
}

func named(s, word string) bool {
	if strings.HasPrefix(s[len(word):], ":") {
		return true
	}
	rest := strings.TrimLeft(s[len(word):], " \t")
	return slices.ContainsFunc(assignOps, func(op string) bool { return strings.HasPrefix(rest, op) })
}

func trimTrailing(s string) string {
	trimmed := strings.TrimRight(s, " \t")
	dollars := len(trimmed) - len(strings.TrimRight(trimmed, "$"))
	if dollars%2 == 1 && len(trimmed) < len(s) {
		return s[:len(trimmed)+1]
	}
	return trimmed
}

func (p *reader) conditional(at int, s, word string) {
	d := Directive{Keyword: p.keyword(at, word), Span: Span{Start: at, End: at + len(s)}}
	rest := strings.TrimLeft(s[len(word):], " \t")
	argsAt := at + len(s) - len(rest)
	switch word {
	case "ifeq", "ifneq", "ifdef", "ifndef":
		d.Args = p.expr(argsAt, at+len(s))
		c := &Conditional{Open: d, Span: d.Span}
		if !p.conditionArgs(&c.Open, word, rest) {
			c.Broken = true
		}
		p.open = append(p.open, c)
		p.file.Conditionals = append(p.file.Conditionals, c)
	case "else":
		if len(p.open) == 0 {
			p.fail(ErrConditional, d.Keyword.Span, "`else` without an `ifeq`, `ifneq`, `ifdef` or `ifndef`")
			return
		}
		c := p.open[len(p.open)-1]
		if next := firstWord(rest); next != "" {
			inner := p.keyword(argsAt, next)
			tail := strings.TrimLeft(rest[len(next):], " \t")
			nested := Directive{Keyword: inner, Span: d.Span, Args: p.expr(at+len(s)-len(tail), at+len(s))}
			if !slices.Contains([]string{"ifeq", "ifneq", "ifdef", "ifndef"}, next) || !p.conditionArgs(&nested, next, tail) {
				p.fail(ErrConditional, Span{Start: argsAt, End: at + len(s)}, "only another conditional can follow `else` on the same line")
			}
			d.Names, d.Args = nested.Names, nested.Args
		}
		c.Else = append(c.Else, d)
	case "endif":
		if len(p.open) == 0 {
			p.fail(ErrConditional, d.Keyword.Span, "`endif` without an `ifeq`, `ifneq`, `ifdef` or `ifndef`")
			return
		}
		c := p.open[len(p.open)-1]
		p.open = p.open[:len(p.open)-1]
		c.End = &d
		c.Span.End = d.Span.End
	}
}

func (p *reader) conditionArgs(d *Directive, word, rest string) bool {
	switch word {
	case "ifdef", "ifndef":
		name := strings.TrimSpace(rest)
		if name == "" || strings.ContainsAny(name, " \t") && !strings.HasPrefix(name, "$") {
			p.fail(ErrConditional, d.Span, "`"+word+"` needs exactly one variable name")
			return false
		}
		d.Names = []Word{{Text: name, Span: Span{Start: d.Args.Span.Start, End: d.Args.Span.Start + len(name)}, Expr: d.Args}}
		d.Args = Expr{Span: d.Args.Span}
		return true
	}
	if strings.HasPrefix(rest, "(") {
		if closing := matching(rest, 0); closing < 0 || strings.TrimSpace(rest[closing+1:]) != "" || topLevel(rest[1:closing], 0, ",") == closing-1 {
			p.fail(ErrConditional, d.Span, "invalid syntax in conditional: write `"+word+" (a,b)`")
			return false
		}
		return true
	}
	first, after, ok := quoted(rest)
	if ok {
		_, tail, ok2 := quoted(strings.TrimLeft(after, " \t"))
		if ok2 && strings.TrimSpace(tail) == "" && first >= 0 {
			return true
		}
	}
	p.fail(ErrConditional, d.Span, "invalid syntax in conditional: write `"+word+" (a,b)` or `"+word+" \"a\" \"b\"`")
	return false
}

func quoted(s string) (int, string, bool) {
	if s == "" || s[0] != '"' && s[0] != '\'' {
		return 0, "", false
	}
	end := strings.IndexByte(s[1:], s[0])
	if end < 0 {
		return 0, "", false
	}
	return end, s[end+2:], true
}

func matching(s string, open int) int {
	closing := map[byte]byte{'(': ')', '{': '}'}[s[open]]
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case s[open]:
			depth++
		case closing:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func (p *reader) define(at int, s string, mods []Word, line Span) {
	rest := strings.TrimLeft(s[len("define"):], " \t")
	nameAt := at + len(s) - len(rest)
	name, op := rest, ""
	for _, candidate := range assignOps {
		if trimmed, ok := strings.CutSuffix(name, candidate); ok {
			name, op = strings.TrimRight(trimmed, " \t"), candidate
			break
		}
	}
	body := min(line.End+1, len(p.text))
	a := &Assignment{Name: p.word(nameAt, strings.TrimSpace(name)), Op: op, Keyword: Span{Start: at, End: at + len("define")}, Modifiers: mods, Define: true, Span: Span{Start: line.Start, End: line.End}, Value: Expr{Span: Span{Start: body, End: body}}}
	if a.Name.Text == "" {
		p.fail(ErrDefine, Span{Start: at, End: at + len(s)}, "`define` needs a variable name")
	}
	p.file.Assignments = append(p.file.Assignments, a)
	p.defining = &define{assignment: a}
}

func (p *reader) plainDirective(at int, s, word string, mods []Word) {
	d := &Directive{Keyword: p.keyword(at, word), Span: Span{Start: at, End: at + len(s)}}
	rest := strings.TrimLeft(s[len(word):], " \t")
	argsAt := at + len(s) - len(rest)
	switch word {
	case "include", "-include", "sinclude":
		inc := &Include{Keyword: d.Keyword, Files: p.fields(argsAt, rest), Span: d.Span}
		if word != "include" {
			inc.Kind = IncludeOptional
		}
		p.file.Includes = append(p.file.Includes, inc)
		return
	case "export", "unexport":
		if i, rule := operator(rest); i >= 0 && !rule {
			p.assign(rest, i, argsAt, append(mods, d.Keyword), nil)
			return
		}
		if word == "export" && rest == "" {
			p.file.ExportAll = true
		}
	}
	if word == "export" || word == "unexport" || word == "undefine" {
		d.Names = p.fields(argsAt, rest)
	} else {
		d.Args = p.expr(argsAt, at+len(s))
	}
	p.file.Directives = append(p.file.Directives, d)
}

func (p *reader) fields(at int, s string) []Word {
	var out []Word
	scan.Forward(0, len(s), func(i int) int {
		if strings.IndexByte(whitespace+"\\\n\r", s[i]) >= 0 {
			return i + 1
		}
		end := i
		for end < len(s) && strings.IndexByte(whitespace+"\n\r", s[end]) < 0 && !continuation(s, end) {
			if s[end] == '$' {
				end = referenceEnd(s, end, len(s))
				continue
			}
			end++
		}
		out = append(out, p.word(at+i, s[i:end]))
		return end
	})
	return out
}

func (p *reader) parseRule(s string, colon, at, end int) {
	after := colon + 1
	double := after < len(s) && s[after] == ':'
	if double {
		after++
	}
	targetText := s[:colon]
	if before := strings.TrimSuffix(targetText, "&"); len(before) < len(targetText) && (len(before)-len(strings.TrimRight(before, "$")))%2 == 0 {
		targetText = before
	}
	if strings.TrimSpace(targetText) == "" {
		p.ignored = true
		return
	}
	targets := p.fields(at, targetText)
	i := topLevel(s, after, "=;")
	if i < len(s) && s[i] == '=' && targetAssignment(s[after:]) {
		p.mixedTargets(targets)
		rest := s[after:]
		rest = strings.TrimLeft(rest, " \t")
		restAt := at + len(s) - len(rest)
		var mods []Word
		word := firstWord(rest)
		for slices.Contains(modifiers, word) && strings.TrimSpace(rest[len(word):]) != "" {
			mods = append(mods, p.keyword(restAt, word))
			trimmed := strings.TrimLeft(rest[len(word):], " \t")
			restAt += len(rest) - len(trimmed)
			rest = trimmed
			word = firstWord(rest)
		}
		if j, isRule := operator(rest); j >= 0 && !isRule {
			p.assign(rest, j, restAt, mods, targets)
		}
		return
	}
	i = topLevel(s, after, ";")
	rule := &Rule{Targets: targets, DoubleColon: double, Grouped: len(targetText) < colon, Colon: at + colon, Header: Span{Start: at, End: at + i}, Span: Span{Start: at, End: end}}
	deps := s[after:i]
	depsAt := at + after
	if second := topLevel(deps, 0, ":"); second < len(deps) {
		rule.Patterns = p.fields(depsAt, deps[:second])
		switch {
		case len(rule.Patterns) == 0:
			p.fail(ErrSyntax, Span{Start: depsAt + second, End: depsAt + second + 1}, "a static pattern rule needs a target pattern before this `:`")
		case p.known(rule.Patterns) > 1:
			p.fail(ErrSyntax, rule.Patterns[1].Span, "a static pattern rule takes one target pattern")
		}
		if len(targets) > 0 && p.percent(targets[0]) {
			p.fail(ErrSyntax, targets[0].Span, fmt.Sprintf("pattern target `%s` cannot head a static pattern rule", targets[0].Text))
		}
		for _, pattern := range rule.Patterns {
			if literal(pattern) {
				p.fail(ErrSyntax, pattern.Span, "the target pattern of a static pattern rule needs a `%`")
				continue
			}
			for _, t := range targets {
				if !pattern.Expr.Dynamic() && literal(t) && !strings.HasPrefix(t.Text, ".") && !model.MatchesPattern(pattern.Text, t.Text) {
					p.fail(ErrPatternMismatch, t.Span, fmt.Sprintf("target `%s` does not match the target pattern `%s`, so make ignores it", t.Text, pattern.Text))
				}
			}
		}
		deps = deps[second+1:]
		depsAt += second + 1
	}
	if len(rule.Patterns) == 0 {
		p.mixedTargets(targets)
	}
	if bar := topLevel(deps, 0, "|"); bar < len(deps) {
		rule.Prereqs = p.fields(depsAt, deps[:bar])
		rule.OrderOnly = p.fields(depsAt+bar+1, deps[bar+1:])
	} else {
		rule.Prereqs = p.fields(depsAt, deps)
	}
	if i < len(s) && s[i] == ';' {
		rule.Recipe = append(rule.Recipe, RecipeLine{Span: Span{Start: at + i + 1, End: end}, Inline: true, Expr: p.expr(at+i+1, end)})
	}
	p.file.Rules = append(p.file.Rules, rule)
	p.rule = rule
	for _, t := range targets {
		if t.Text == exportAll {
			p.file.ExportAll = true
		}
	}
}

func assignmentName(s string, i int) (string, int) {
	opStart := i
	for _, op := range assignOps {
		if strings.HasSuffix(s[:i+1], op) {
			opStart = i + 1 - len(op)
			break
		}
	}
	return strings.TrimRight(s[:opStart], " \t"), opStart
}

func (p *reader) assign(s string, i, at int, mods, targets []Word) {
	name, opStart := assignmentName(s, i)
	opEnd := i + 1
	if name == "" {
		p.fail(ErrSyntax, Span{Start: at + opStart, End: at + opEnd}, "this assignment has no variable name")
		return
	}
	if len(targets) == 0 && !slices.ContainsFunc(mods, func(m Word) bool { return m.Text == "export" }) && p.spaced(p.expr(at, at+len(name))) {
		if s[opStart] == ':' {
			p.parseRule(s, opStart, at, at+len(s))
			return
		}
		if colon := topLevel(s, opEnd, ":"); colon < len(s) {
			p.parseRule(s, colon, at, at+len(s))
			return
		}
		p.fail(ErrSyntax, Span{Start: at, End: at + len(name)}, "missing separator: a variable name cannot contain whitespace")
		return
	}
	value := strings.TrimLeft(s[opEnd:], " \t")
	first := len(p.file.Errors)
	a := &Assignment{
		Name:      p.word(at, name),
		Op:        s[opStart:opEnd],
		Value:     p.expr(at+len(s)-len(value), at+len(s)),
		Modifiers: mods,
		Targets:   targets,
		Span:      Span{Start: at, End: at + len(s)},
	}
	if len(targets) > 0 {
		a.Span.Start = targets[0].Span.Start
	}
	p.deferred(first, a)
	p.file.Assignments = append(p.file.Assignments, a)
	if len(targets) == 0 {
		p.setting(a)
	}
}

func (p *reader) setting(a *Assignment) {
	value := strings.TrimSpace(p.text[a.Value.Span.Start:a.Value.Span.End])
	op := a.Op[0]
	setting := oneLine(p.text[a.Name.Span.Start:a.Value.Span.End])
	switch a.Name.Text {
	case "SHELL":
		switch op {
		case '=', ':':
			p.shell, p.shellSet, p.evidence = value, true, setting
		case '!':
			p.shell, p.shellSet, p.evidence = "$(shell)", true, setting
		}
	case ".SHELLFLAGS":
		if op == '=' || op == ':' {
			p.flags = value
		}
	case ".RECIPEPREFIX":
		switch {
		case value == "":
			p.prefix = defaultPrefix
		case !strings.ContainsRune(value, '$'):
			p.prefix = value[0]
		}
	}
}

func continuation(s string, i int) bool {
	return s[i] == '\\' && i+1 < len(s) && s[i+1] == '\n'
}

func modifies(rest string) bool {
	if firstWord(rest) == "define" || slices.Contains(modifiers, firstWord(rest)) {
		return true
	}
	i, rule := operator(rest)
	return i >= 0 && !rule
}

func prefixName(prefix byte) string {
	if prefix == defaultPrefix {
		return "a tab"
	}
	return "`" + string(prefix) + "`, the `.RECIPEPREFIX`"
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\\\n", " ")), " ")
}

func operator(s string) (int, bool) {
	i := topLevel(s, 0, "=:")
	switch {
	case i == len(s):
		return -1, false
	case s[i] == '=':
		return i, false
	}
	rest := strings.TrimLeft(s[i:], ":")
	if strings.HasPrefix(rest, "=") && len(s)-i-len(rest) <= 3 {
		return i + len(s) - i - len(rest), false
	}
	return i, true
}

func commentStart(s string) int {
	return topLevel(s, 0, "#")
}

func topLevel(s string, from int, stops string) int {
	found := len(s)
	scan.Forward(from, len(s), func(i int) int {
		switch {
		case s[i] == '\\' && i+1 < len(s) && strings.IndexByte("#:\n", s[i+1]) >= 0:
			return i + 2
		case s[i] == '$' && i+1 < len(s) && (s[i+1] == '(' || s[i+1] == '{'):
			open, closeByte := s[i+1], byte(')')
			if open == '{' {
				closeByte = '}'
			}
			if j := balanced(s, i+1, len(s), func(j int) bool { return s[j] == open }, closeByte); j >= 0 {
				return j + 1
			}
			return len(s)
		case s[i] == '$':
			return i + 2
		case strings.IndexByte(stops, s[i]) >= 0:
			found = i
			return len(s)
		}
		return i + 1
	})
	return found
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " \t\\"); i >= 0 {
		return s[:i]
	}
	return s
}

func (p *reader) prelude(f *File) string {
	var sb strings.Builder
	sb.WriteString(p.shellOptions())
	for _, name := range f.exported() {
		sb.WriteString(name + "=" + projection.Dynamic + "\n")
	}
	return sb.String()
}

func (p *reader) shellOptions() string {
	flags := p.flags
	switch {
	case flags != "":
	case p.posix:
		flags = posixFlags
	default:
		flags = defaultFlags
	}
	if strings.ContainsRune(flags, '$') {
		return ""
	}
	words := strings.Fields(flags)
	var options []string
	for i := 0; i < len(words); i++ {
		word := words[i]
		switch {
		case word == "-o" && i+1 < len(words):
			options = append(options, word, words[i+1])
			i++
		case strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "--"):
			letters := strings.ReplaceAll(word[1:], "c", "")
			named := strings.HasSuffix(letters, "o") && i+1 < len(words)
			if named {
				letters = strings.TrimSuffix(letters, "o")
			}
			if letters != "" {
				options = append(options, "-"+letters)
			}
			if named {
				options = append(options, "-o", words[i+1])
				i++
			}
		}
	}
	if len(options) == 0 {
		return ""
	}
	return "set " + strings.Join(options, " ") + "\n"
}

func (p *reader) recipeLine(l RecipeLine) projection.RecipeLine {
	command := l.Span.End - len(strings.TrimLeft(p.text[l.Span.Start:l.Span.End], recipeFlags))
	comment := command < l.Span.End && p.text[command] == '#' && !strings.Contains(p.text[command:l.Span.End], "\n")
	return projection.RecipeLine{Start: l.Span.Start, Command: command, End: l.Span.End, Comment: comment}
}

func (p *reader) content(b *projection.Builder, line Expr, from int) {
	for _, n := range line.Nodes {
		if n.Span.End <= from {
			continue
		}
		if n.Kind != NodeText {
			b.Placeholder(n.Span.Start, n.Span.End)
			continue
		}
		p.verbatim(b, max(n.Span.Start, from), n.Span.End)
	}
}

func (p *reader) verbatim(b *projection.Builder, start, end int) {
	run := start
	for i := start; i < end; i++ {
		switch {
		case p.text[i] == '\\' && i+2 < end && p.text[i+1] == '\n' && p.text[i+2] == p.prefix:
			b.Verbatim(run, i+2)
			b.Opaque(i+2, i+3, "")
			run = i + 3
			i += 2
		case p.text[i] == '$' && i+1 < end && p.text[i+1] == '$':
			b.Verbatim(run, i)
			b.Escape(i, i+2, "$")
			run = i + 2
			i++
		}
	}
	b.Verbatim(run, end)
}
