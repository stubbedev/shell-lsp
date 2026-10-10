package hover

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
)

const snippetWidth = 80

type Commands interface {
	Describe(ctx context.Context, name string) (string, error)
}

type Input struct {
	Tree     *parser.Tree
	Offset   int
	Commands Commands
}

type Result struct {
	Markdown string
	Span     parser.Span
}

func Hover(ctx context.Context, in Input) (Result, bool, error) {
	h := newHoverer(in)
	if r, ok := h.keyword(); ok {
		return r, true, nil
	}
	if r, ok := h.paramExp(); ok {
		return r, true, nil
	}
	if r, ok := h.functionName(); ok {
		return r, true, nil
	}
	if r, ok := h.variableName(); ok {
		return r, true, nil
	}
	return h.command(ctx)
}

type hoverer struct {
	Input
	text    string
	dialect dialect.Dialect
	scope   parser.ScopeID
}

func (h hoverer) keyword() (Result, bool) {
	for _, k := range h.Tree.Keywords {
		if !k.Span.Covers(h.Offset) {
			continue
		}
		entry, ok := shelldocs.Keyword(k.Name)
		if !ok {
			return Result{}, false
		}
		return Result{Markdown: entry.Markdown(h.dialect), Span: k.Span}, true
	}
	return Result{}, false
}

func (h hoverer) paramExp() (Result, bool) {
	path := h.Tree.PathAt(h.Offset)
	for _, node := range slices.Backward(path) {
		exp, ok := node.(*syntax.ParamExp)
		if !ok || exp.Param == nil {
			continue
		}
		span, ok := h.Tree.NodeSpan(exp)
		if !ok {
			continue
		}
		sections := []string{h.parameter(exp.Param.Value)}
		sections = append(sections, expansionDocs(exp)...)
		return Result{Markdown: strings.Join(sections, "\n\n---\n\n"), Span: span}, true
	}
	return Result{}, false
}

func (h hoverer) parameter(name string) string {
	if isDigits(name) {
		if fn, ok := h.Tree.EnclosingFunction(h.scope); ok {
			return fmt.Sprintf("**$%s**: argument %s of function `%s`", name, name, fn.Name)
		}
		if name == "0" {
			entry, _ := shelldocs.Parameter(name)
			return "**$0**: " + entry.Doc
		}
		return fmt.Sprintf("**$%s**: positional parameter %s of the script", name, name)
	}
	if entry, ok := shelldocs.Parameter(name); ok {
		return fmt.Sprintf("**$%s**: %s", name, entry.Doc)
	}
	return h.variable(name)
}

func (h hoverer) variableName() (Result, bool) {
	for _, ref := range h.Tree.References {
		if ref.Span.Covers(h.Offset) {
			return Result{Markdown: h.variable(ref.Name), Span: ref.Span}, true
		}
	}
	for _, def := range h.Tree.Definitions {
		if def.NameSpan.Covers(h.Offset) {
			return Result{Markdown: h.variable(def.Name), Span: def.NameSpan}, true
		}
	}
	return Result{}, false
}

func (h hoverer) variable(name string) string {
	defs := h.Tree.Visible(name, h.scope)
	if defs.Len() == 0 {
		return h.undefinedVariable(name)
	}
	first := *defs.At(0)
	var sb strings.Builder
	fmt.Fprintf(&sb, "**%s**: %s", name, h.describe(first, defs))
	fmt.Fprintf(&sb, "\n\nDeclared on line %d:\n```%s\n%s\n```", h.line(first.Span.Start), fence(h.dialect), h.snippet(first.Span.Start))
	if n := defs.Len(); n > 1 {
		fmt.Fprintf(&sb, "\n\nAssigned in %d places.", n)
	}
	if entry, ok := shelldocs.Variable(name); ok {
		fmt.Fprintf(&sb, "\n\n%s", entry.Doc)
	}
	return sb.String()
}

func (h hoverer) describe(first parser.Definition, defs parser.Definitions) string {
	var attrs parser.Attr
	for d := range defs.All() {
		attrs |= d.Attrs
	}
	words := []string{}
	if attrs.Has(parser.AttrReadonly) {
		words = append(words, "read-only")
	}
	if attrs.Has(parser.AttrExport) {
		words = append(words, "exported")
	}
	if first.Local {
		words = append(words, "local")
	} else {
		words = append(words, "global")
	}
	switch {
	case attrs.Has(parser.AttrAssoc):
		words = append(words, "associative array")
	case attrs.Has(parser.AttrArray):
		words = append(words, "array")
	case attrs.Has(parser.AttrInteger):
		words = append(words, "integer")
	case attrs.Has(parser.AttrNameref):
		words = append(words, "name reference")
	default:
		words = append(words, "variable")
	}
	desc := strings.Join(words, " ")
	if first.Kind == parser.DefLoop {
		desc += " (loop variable)"
	}
	if first.Kind == parser.DefCommandEnv {
		desc += " (environment of a single command)"
	}
	if fn, ok := h.Tree.EnclosingFunction(first.Within); ok && first.Local {
		desc += fmt.Sprintf(" in `%s`", fn.Name)
	}
	return desc
}

func (h hoverer) undefinedVariable(name string) string {
	if entry, ok := shelldocs.Variable(name); ok {
		return fmt.Sprintf("**%s**: shell variable\n\n%s", name, entry.Markdown(h.dialect))
	}
	return fmt.Sprintf("**%s**: not assigned in this file; expected from the environment or a sourced file", name)
}

func (h hoverer) functionName() (Result, bool) {
	for _, fn := range h.Tree.Functions {
		if fn.NameSpan.Covers(h.Offset) {
			return Result{Markdown: h.function(fn), Span: fn.NameSpan}, true
		}
	}
	return Result{}, false
}

func FunctionDoc(tree *parser.Tree, fn parser.Function) string {
	return newHoverer(Input{Tree: tree, Offset: fn.NameSpan.Start}).function(fn)
}

func VariableDoc(tree *parser.Tree, offset int, name string) string {
	return newHoverer(Input{Tree: tree, Offset: offset}).variable(name)
}

func newHoverer(in Input) hoverer {
	return hoverer{Input: in, text: in.Tree.Text(), dialect: in.Tree.Dialect, scope: in.Tree.ScopeAt(in.Offset)}
}

func (h hoverer) function(fn parser.Function) string {
	var sb strings.Builder
	sig := FunctionSignature(h.Tree, fn)
	fmt.Fprintf(&sb, "```%s\n%s\n```", fence(h.dialect), sig.Label())
	if sig.Doc != "" {
		fmt.Fprintf(&sb, "\n\n%s", sig.Doc)
	}
	for _, p := range sig.Params {
		if p.Doc != "" {
			fmt.Fprintf(&sb, "\n\n`%s`: %s", p.Name, p.Doc)
		}
	}
	fmt.Fprintf(&sb, "\n\nFunction defined on line %d.", h.line(fn.Span.Start))
	return sb.String()
}

func (h hoverer) command(ctx context.Context) (Result, bool, error) {
	for _, cmd := range h.Tree.Commands {
		if !cmd.NameSpan.Covers(h.Offset) {
			continue
		}
		if fn, ok := h.Tree.FunctionNamed(cmd.Name); ok {
			return Result{Markdown: h.function(fn), Span: cmd.NameSpan}, true, nil
		}
		if entry, ok := shelldocs.Builtin(cmd.Name); ok {
			return Result{Markdown: entry.Markdown(h.dialect), Span: cmd.NameSpan}, true, nil
		}
		if entry, ok := shelldocs.Keyword(cmd.Name); ok {
			return Result{Markdown: entry.Markdown(h.dialect), Span: cmd.NameSpan}, true, nil
		}
		if h.Commands == nil {
			return Result{}, false, nil
		}
		doc, err := h.Commands.Describe(ctx, cmd.Name)
		if err != nil {
			return Result{}, false, err
		}
		return Result{Markdown: doc, Span: cmd.NameSpan}, true, nil
	}
	return Result{}, false, nil
}

type Parameter struct {
	Index int
	Name  string
	Doc   string
}

type Signature struct {
	Name     string
	Params   []Parameter
	Variadic bool
	Doc      string
}

func (s Signature) Label() string {
	parts := []string{s.Name}
	for _, p := range s.Params {
		parts = append(parts, p.Name)
	}
	if s.Variadic {
		parts = append(parts, "[args...]")
	}
	return strings.Join(parts, " ")
}

var paramDocLine = regexp.MustCompile(`^\$?([0-9]+|[A-Za-z_][A-Za-z0-9_]*)\s*(?:-|:)\s+(.+)$`)

func FunctionSignature(tree *parser.Tree, fn parser.Function) Signature {
	highest, variadic := positionalUse(fn)
	names := parameterNames(tree, fn)
	for index := range names {
		highest = max(highest, index)
	}
	sig := Signature{Name: fn.Name, Variadic: variadic}
	for i := 1; i <= highest; i++ {
		name, ok := names[i]
		if !ok {
			name = "$" + strconv.Itoa(i)
		}
		sig.Params = append(sig.Params, Parameter{Index: i, Name: name})
	}
	var docLines []string
	for line := range strings.SplitSeq(fn.Doc, "\n") {
		m := paramDocLine.FindStringSubmatch(line)
		if m == nil {
			docLines = append(docLines, line)
			continue
		}
		i := slices.IndexFunc(sig.Params, func(p Parameter) bool { return strconv.Itoa(p.Index) == m[1] || p.Name == m[1] })
		if i < 0 {
			docLines = append(docLines, line)
			continue
		}
		sig.Params[i].Doc = m[2]
	}
	sig.Doc = strings.TrimSpace(strings.Join(docLines, "\n"))
	return sig
}

func parameterNames(tree *parser.Tree, fn parser.Function) map[int]string {
	names := map[int]string{}
	for _, def := range tree.Definitions {
		if def.Within != fn.Body || def.Value == nil {
			continue
		}
		if index, ok := positionalValue(def.Value); ok {
			if _, taken := names[index]; !taken {
				names[index] = def.Name
			}
		}
	}
	return names
}

func positionalValue(w *syntax.Word) (int, bool) {
	if len(w.Parts) != 1 {
		return 0, false
	}
	part := w.Parts[0]
	if dq, ok := part.(*syntax.DblQuoted); ok && len(dq.Parts) == 1 {
		part = dq.Parts[0]
	}
	exp, ok := part.(*syntax.ParamExp)
	if !ok || exp.Param == nil || !isDigits(exp.Param.Value) || exp.Param.Value == "0" || exp.Length || exp.Excl || exp.Slice != nil || exp.Repl != nil {
		return 0, false
	}
	if exp.Exp != nil && exp.Exp.Op != syntax.DefaultUnset && exp.Exp.Op != syntax.DefaultUnsetOrNull {
		return 0, false
	}
	n, err := strconv.Atoi(exp.Param.Value)
	return n, err == nil
}

func positionalUse(fn parser.Function) (int, bool) {
	highest, variadic := 0, false
	if fn.Decl == nil || fn.Decl.Body == nil {
		return 0, false
	}
	syntax.Walk(fn.Decl.Body, func(node syntax.Node) bool {
		if _, nested := node.(*syntax.FuncDecl); nested {
			return false
		}
		exp, ok := node.(*syntax.ParamExp)
		if !ok || exp.Param == nil || !exp.Param.ValuePos.IsValid() {
			return true
		}
		switch name := exp.Param.Value; {
		case name == "@" || name == "*" || name == "#":
			variadic = true
		case isDigits(name) && name != "0":
			n, err := strconv.Atoi(name)
			if err == nil {
				highest = max(highest, n)
			}
		}
		return true
	})
	return highest, variadic
}

func expansionDocs(exp *syntax.ParamExp) []string {
	var docs []string
	add := func(key string) {
		if doc, ok := shelldocs.Expansion(key); ok {
			docs = append(docs, doc)
		}
	}
	switch {
	case exp.Length:
		add("length")
	case exp.Excl && exp.Index != nil && isAllIndex(exp.Index):
		add("keys")
	case exp.Excl:
		add("indirect")
	case exp.Names != 0:
		add("names")
	case exp.Width:
		add("width")
	case exp.IsSet:
		add("isset")
	}
	switch {
	case exp.Slice != nil:
		add("slice")
	case exp.Repl != nil:
		add("replace")
	case exp.Exp != nil:
		add(exp.Exp.Op.String())
	}
	if exp.Flags != nil {
		if flags := zshFlagDocs(exp.Flags.Value); flags != "" {
			docs = append(docs, flags)
		}
	}
	if len(exp.Modifiers) > 0 {
		docs = append(docs, zshModifierDocs(exp.Modifiers))
	}
	return docs
}

func isAllIndex(index syntax.ArithmExpr) bool {
	w, ok := index.(*syntax.Word)
	if !ok {
		return false
	}
	lit := w.Lit()
	return lit == "@" || lit == "*"
}

var zshDelimitedFlags = "jslrZgI_"

func zshFlagDocs(flags string) string {
	var lines []string
	for i := 0; i < len(flags); i++ {
		c := flags[i]
		doc, ok := shelldocs.ZshFlag(c)
		if ok {
			lines = append(lines, fmt.Sprintf("- `%c`: %s", c, doc))
		}
		if strings.IndexByte(zshDelimitedFlags, c) >= 0 && i+1 < len(flags) {
			delim := closing(flags[i+1])
			if end := strings.IndexByte(flags[i+2:], delim); end >= 0 {
				i += end + 2
			}
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "zsh expansion flags:\n" + strings.Join(lines, "\n")
}

func closing(open byte) byte {
	switch open {
	case '(':
		return ')'
	case '[':
		return ']'
	case '{':
		return '}'
	case '<':
		return '>'
	}
	return open
}

func zshModifierDocs(modifiers []*syntax.Lit) string {
	lines := make([]string, 0, len(modifiers))
	for _, m := range modifiers {
		key := strings.TrimRight(m.Value, "0123456789")
		if strings.HasPrefix(key, "gs") {
			key = "gs"
		} else if key != "" {
			key = key[:1]
		}
		if doc, ok := shelldocs.ZshModifier(key); ok {
			lines = append(lines, fmt.Sprintf("- `:%s`: %s", m.Value, doc))
		}
	}
	return "zsh modifiers:\n" + strings.Join(lines, "\n")
}

func (h hoverer) line(offset int) int {
	return h.Tree.Line(offset)
}

func (h hoverer) snippet(offset int) string {
	start := strings.LastIndexByte(h.text[:offset], '\n') + 1
	end := len(h.text)
	if i := strings.IndexByte(h.text[start:], '\n'); i >= 0 {
		end = start + i
	}
	line := strings.TrimSpace(h.text[start:end])
	if len(line) > snippetWidth {
		cut := snippetWidth
		for cut > 0 && !isRuneStart(line[cut]) {
			cut--
		}
		line = line[:cut] + "…"
	}
	return line
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

func fence(d dialect.Dialect) string {
	if d == dialect.Zsh {
		return "zsh"
	}
	return "sh"
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
