package hover

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/manpage"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
	"github.com/stubbedev/shell-lsp/internal/shelloptions"
	"github.com/stubbedev/shell-lsp/internal/textline"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const snippetWidth = 80

type Commands interface {
	Describe(ctx context.Context, name string) (string, error)
	Subcommand(ctx context.Context, command string, args []string) (manpage.Subcommand, error)
	Options(ctx context.Context, command string, args []string) ([]manpage.Option, error)
}

type Input struct {
	Tree     *parser.Tree
	Imports  []parser.Import
	Offset   int
	Commands Commands
	Lint     func() (lint.Report, bool)
}

type Result struct {
	Markdown string
	Span     parser.Span
}

func Hover(ctx context.Context, in Input) (Result, bool, error) {
	h := newHoverer(in)
	h.path = in.Tree.PathAt(in.Offset)
	h.options = shelloptions.Scan(in.Tree)
	r, ok, err := h.hover(ctx)
	if err != nil {
		return Result{}, false, err
	}
	r, ok = h.withFindings(r, ok)
	return r, ok, nil
}

func (h hoverer) hover(ctx context.Context) (Result, bool, error) {
	if r, ok := h.directive(); ok {
		return r, true, nil
	}
	if r, ok := h.keyword(); ok {
		return r, true, nil
	}
	if r, ok := h.operator(); ok {
		return r, true, nil
	}
	if r, ok := h.paramExp(); ok {
		return r, true, nil
	}
	if r, ok := h.pattern(); ok {
		return r, true, nil
	}
	hit, ok := h.Tree.SymbolAt(h.Offset)
	if !ok {
		return h.argument(ctx)
	}
	switch hit.Kind {
	case parser.HitFunction:
		i := slices.IndexFunc(h.Tree.Functions, func(fn parser.Function) bool { return fn.NameSpan == hit.Span })
		return Result{Markdown: h.function(h.Tree.Functions[i]), Span: hit.Span}, true, nil
	case parser.HitCommand:
		return h.commandName(ctx, hit.Name, hit.Span)
	case parser.HitVariable:
		return Result{Markdown: h.variable(hit.Name), Span: hit.Span}, true, nil
	}
	panic(fmt.Sprintf("hover: unknown symbol kind %d", hit.Kind))
}

type hoverer struct {
	Input
	text    string
	dialect dialect.Dialect
	scope   parser.ScopeID
	path    []parser.Located
	options shelloptions.PerShell
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
	for i, located := range slices.Backward(h.path) {
		exp, ok := located.Node.(*syntax.ParamExp)
		if !ok || exp.Param == nil && exp.NestedParam == nil {
			continue
		}
		var sections []string
		if exp.Param != nil {
			sections = append(sections, h.parameter(exp.Param.Value))
		}
		sections = append(sections, expansionDocs(exp, h.dialect)...)
		if doc, ok := h.expansionPattern(exp); ok {
			sections = append(sections, doc)
		}
		if doc, ok := h.evaluated(exp, h.quotedAt(i)); ok {
			sections = append(sections, doc)
		}
		return Result{Markdown: strings.Join(sections, "\n\n---\n\n"), Span: located.Span}, true
	}
	return Result{}, false
}

func (h hoverer) parameter(name string) string {
	kind, _ := parser.ParamKindOf(name)
	fn, inFunction := h.Tree.FunctionOfBody(h.scope)
	switch {
	case kind == parser.ParamZero && inFunction && fn.NamesArgZero(h.dialect):
		return fmt.Sprintf("**$0**: name of function `%s` while it runs", fn.Name)
	case kind == parser.ParamPositional && inFunction:
		return fmt.Sprintf("**$%s**: argument %s of function `%s`", name, name, fn.Name)
	case kind == parser.ParamPositional:
		return fmt.Sprintf("**$%s**: positional parameter %s of the script", name, name)
	case name == "-":
		return h.optionFlagsDoc()
	}
	if entry, ok := shelldocs.Parameter(name); ok {
		return fmt.Sprintf("**$%s**: %s", name, entry.Doc)
	}
	return h.variable(name)
}

func (h hoverer) optionFlagsDoc() string {
	entry, _ := shelldocs.Parameter("-")
	var sb strings.Builder
	fmt.Fprintf(&sb, "**$-**: %s", entry.Doc)
	for e := range shelldocs.Terms(shelldocs.OptionLetters, h.dialect) {
		fmt.Fprintf(&sb, "\n- `%s`: %s", e.Name, e.Doc)
	}
	return sb.String()
}

func (h hoverer) variable(name string) string {
	defs := h.Tree.Visible(name, h.scope)
	if defs.Len() == 0 {
		if imp, _, ok := parser.ImportedGlobal(h.Imports, name); ok {
			return VariableDoc(imp.Tree, nil, 0, name) + importedFrom(imp)
		}
		return h.undefinedVariable(name)
	}
	first := *defs.At(0)
	var sb strings.Builder
	fmt.Fprintf(&sb, "**%s**: %s", name, h.describe(first, defs))
	fmt.Fprintf(&sb, "\n\nDeclared on line %d:\n```%s\n%s\n```", h.line(first.Span.Start), shelldocs.FenceLanguage(h.dialect), h.snippet(first.Span.Start))
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
	if fn, ok := h.Tree.FunctionOfBody(first.Within); ok && first.Local {
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

func FunctionDoc(tree *parser.Tree, fn parser.Function) string {
	return newHoverer(Input{Tree: tree, Offset: fn.NameSpan.Start}).function(fn)
}

func ImportedFunctionDoc(imp parser.Import, fn parser.Function) string {
	return FunctionDoc(imp.Tree, fn) + importedFrom(imp)
}

func importedFrom(imp parser.Import) string {
	return fmt.Sprintf("\n\nFrom `%s`.", imp.Name)
}

func VariableDoc(tree *parser.Tree, imports []parser.Import, offset int, name string) string {
	return newHoverer(Input{Tree: tree, Imports: imports, Offset: offset}).variable(name)
}

func newHoverer(in Input) hoverer {
	return hoverer{Input: in, text: in.Tree.Text(), dialect: in.Tree.Dialect, scope: in.Tree.ScopeAt(in.Offset)}
}

func (h hoverer) function(fn parser.Function) string {
	var sb strings.Builder
	sig := FunctionSignature(h.Tree, fn)
	fmt.Fprintf(&sb, "```%s\n%s\n```", shelldocs.FenceLanguage(h.dialect), sig.Label())
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

func (h hoverer) argument(ctx context.Context) (Result, bool, error) {
	for _, cmd := range h.Tree.Commands {
		if r, ok, err := h.argumentOf(ctx, cmd); ok || err != nil {
			return r, ok, err
		}
	}
	return Result{}, false, nil
}

func (h hoverer) argumentOf(ctx context.Context, cmd parser.Command) (Result, bool, error) {
	if !cmd.Span.Covers(h.Offset) || h.isFunction(cmd.Name) {
		return Result{}, false, nil
	}
	words := h.commandWords(cmd)
	at := slices.IndexFunc(words, func(arg *syntax.Word) bool {
		span, ok := h.Tree.NodeSpan(arg)
		return ok && span.Covers(h.Offset)
	})
	if at < 0 {
		return Result{}, false, nil
	}
	span, _ := h.Tree.NodeSpan(words[at])
	word, literal := parser.Literal(words[at])
	if !literal {
		return Result{}, false, nil
	}
	args := parser.ArgumentTexts(words)
	args[at] = word
	for layer := range h.Tree.Runners().Layers(cmd.Name, args) {
		local := at - layer.Offset
		switch {
		case h.isFunction(layer.Name):
			return Result{}, false, nil
		case !layer.Wraps || local < layer.Wrap.Command:
			return h.ownArgument(ctx, layer.Name, layer.Args[:local], word, span)
		case local == layer.Wrap.Command:
			return h.commandName(ctx, word, span)
		}
	}
	return Result{}, false, nil
}

func (h hoverer) ownArgument(ctx context.Context, command string, before []string, word string, span parser.Span) (Result, bool, error) {
	if doc, ok := h.vocabularyArgument(command, before, word); ok {
		return Result{Markdown: doc, Span: span}, true, nil
	}
	if h.Commands == nil {
		return Result{}, false, nil
	}
	if strings.HasPrefix(word, "-") {
		return h.flag(ctx, command, before, word, span)
	}
	sub, err := h.Commands.Subcommand(ctx, command, append(slices.Clip(before), word))
	return found(Result{Markdown: sub.Doc, Span: span}, err)
}

func (h hoverer) commandWords(cmd parser.Command) []*syntax.Word {
	if len(cmd.Args) > 0 {
		return cmd.Args
	}
	for _, located := range h.path {
		decl, ok := located.Node.(*syntax.DeclClause)
		if !ok || parser.Offset(decl.Variant.Pos()) != cmd.NameSpan.Start {
			continue
		}
		words := make([]*syntax.Word, 0, len(decl.Args))
		for _, arg := range decl.Args {
			if arg.Name == nil && arg.Value != nil {
				words = append(words, arg.Value)
			}
		}
		return words
	}
	return nil
}

func (h hoverer) vocabularyArgument(command string, before []string, word string) (string, bool) {
	switch slot := shelldocs.ArgumentSlot(command, before, word).(type) {
	case shelldocs.OperatorSlot:
		return h.term(word, slot.Vocabularies...)
	case shelldocs.OptionSlot:
		if slot.Vocabulary == shelldocs.SetoptOptions {
			return h.zshOption(word)
		}
		return h.term(word, slot.Vocabulary)
	case shelldocs.ValueSlot:
		return h.term(word, slot.Vocabulary)
	case shelldocs.SignalSlot:
		if !slot.Dashed {
			return h.signal(word, slot.Vocabularies)
		}
		if doc, ok := h.signal(word[1:], slot.Vocabularies); ok {
			return doc, true
		}
		return h.builtinFlag(command, word)
	case shelldocs.FlagSlot:
		return h.builtinFlag(command, word)
	case shelldocs.OperandSlot, shelldocs.FinishedSlot, shelldocs.VariableSlot, shelldocs.CommandSlot, shelldocs.DirectorySlot, shelldocs.FormatSlot:
		return "", false
	default:
		panic(fmt.Sprintf("hover: unknown argument slot %T", slot))
	}
}

func (h hoverer) term(name string, vocabularies ...shelldocs.Vocabulary) (string, bool) {
	for _, v := range vocabularies {
		if e, ok := shelldocs.Term(v, name, h.dialect); ok {
			return fmt.Sprintf("**%s**\n\n%s", name, e.Markdown(h.dialect)), true
		}
	}
	return "", false
}

func (h hoverer) signal(word string, vocabularies []shelldocs.Vocabulary) (string, bool) {
	name, _ := shelldocs.NormalizeSignal(word)
	for _, v := range vocabularies {
		if e, ok := shelldocs.Term(v, name, h.dialect); ok {
			title := name
			if v == shelldocs.Signals {
				title = "SIG" + name
			}
			return fmt.Sprintf("**%s**\n\n%s", title, e.Markdown(h.dialect)), true
		}
	}
	return "", false
}

func (h hoverer) zshOption(word string) (string, bool) {
	name := zshOptionKey(word)
	negated := false
	e, ok := zshOptionNamed(name)
	if !ok && strings.HasPrefix(name, "no") {
		e, ok = zshOptionNamed(name[2:])
		negated = true
	}
	if !ok {
		return "", false
	}
	if negated {
		return fmt.Sprintf("**%s** (negates `%s`)\n\n%s", word, e.Name, e.Markdown(h.dialect)), true
	}
	return fmt.Sprintf("**%s**\n\n%s", e.Name, e.Markdown(h.dialect)), true
}

func zshOptionKey(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "_", ""))
}

func zshOptionNamed(key string) (shelldocs.Entry, bool) {
	for e := range shelldocs.Terms(shelldocs.SetoptOptions, dialect.Zsh) {
		if zshOptionKey(e.Name) == key {
			return e, true
		}
	}
	return shelldocs.Entry{}, false
}

func (h hoverer) builtinFlag(command, word string) (string, bool) {
	name, _, _ := strings.Cut(word, "=")
	if e, ok := shelldocs.BuiltinFlag(command, name, h.dialect); ok {
		return fmt.Sprintf("**%s %s**\n\n%s", command, name, e.Doc), true
	}
	if len(name) < 3 || name[1] == '-' {
		return "", false
	}
	lines := make([]string, 0, len(name)-1)
	for _, letter := range name[1:] {
		e, ok := shelldocs.BuiltinFlag(command, "-"+string(letter), h.dialect)
		if !ok {
			return "", false
		}
		lines = append(lines, fmt.Sprintf("- `-%c`: %s", letter, e.Doc))
	}
	return fmt.Sprintf("**%s %s**\n\n%s", command, name, strings.Join(lines, "\n")), true
}

func (h hoverer) operator() (Result, bool) {
	for _, located := range slices.Backward(h.path) {
		if r, ok := h.operatorOf(located.Node); ok {
			return r, true
		}
	}
	return Result{}, false
}

func (h hoverer) operatorOf(node syntax.Node) (Result, bool) {
	var (
		start, end int
		name       string
		vocabs     []shelldocs.Vocabulary
		extra      string
	)
	switch n := node.(type) {
	case *syntax.Redirect:
		name, vocabs = n.Op.String(), []shelldocs.Vocabulary{shelldocs.Redirections}
		start = parser.Offset(n.OpPos)
		end = start + len(name)
		if n.N != nil {
			start = parser.Offset(n.N.Pos())
		}
		if doc, ok := h.heredoc(n); ok {
			extra = heredocExpansion(doc)
		}
	case *syntax.BinaryCmd:
		name, vocabs = n.Op.String(), []shelldocs.Vocabulary{shelldocs.ListOperators}
		start = parser.Offset(n.OpPos)
	case *syntax.CaseItem:
		if !n.OpPos.IsValid() {
			return Result{}, false
		}
		name, vocabs = n.Op.String(), []shelldocs.Vocabulary{shelldocs.ListOperators}
		start = parser.Offset(n.OpPos)
	case *syntax.BinaryArithm:
		name, vocabs = n.Op.String(), []shelldocs.Vocabulary{shelldocs.ArithmeticOperators}
		start = parser.Offset(n.OpPos)
	case *syntax.UnaryArithm:
		name, vocabs = n.Op.String(), []shelldocs.Vocabulary{shelldocs.ArithmeticOperators}
		start = parser.Offset(n.OpPos)
	case *syntax.BinaryTest:
		name, vocabs = n.Op.String(), []shelldocs.Vocabulary{shelldocs.TestBinaryOperators, shelldocs.CondOperators}
		start = parser.Offset(n.OpPos)
	case *syntax.UnaryTest:
		name, vocabs = n.Op.String(), []shelldocs.Vocabulary{shelldocs.TestUnaryOperators}
		start = parser.Offset(n.OpPos)
	default:
		return Result{}, false
	}
	if end == 0 {
		end = start + len(name)
	}
	span := parser.Span{Start: start, End: end}
	if !span.Covers(h.Offset) {
		return Result{}, false
	}
	doc, ok := h.term(name, vocabs...)
	if !ok {
		return Result{}, false
	}
	if extra != "" {
		doc += "\n\n" + extra
	}
	return Result{Markdown: doc, Span: span}, true
}

func (h hoverer) heredoc(r *syntax.Redirect) (parser.Heredoc, bool) {
	i := slices.IndexFunc(h.Tree.Heredocs, func(doc parser.Heredoc) bool { return doc.Redirect == r })
	if i < 0 {
		return parser.Heredoc{}, false
	}
	return h.Tree.Heredocs[i], true
}

func heredocExpansion(doc parser.Heredoc) string {
	if doc.Quoted {
		return "The delimiter is quoted, so the body is taken literally: no expansion happens in it."
	}
	return "The delimiter is unquoted, so `$`, backticks and `\\` are processed in the body; quote the delimiter (`<<'EOF'`) to take it literally."
}

func (h hoverer) flag(ctx context.Context, command string, before []string, word string, span parser.Span) (Result, bool, error) {
	options, err := h.Commands.Options(ctx, command, before)
	if err != nil {
		return found(Result{}, err)
	}
	name, _, _ := strings.Cut(word, "=")
	i := slices.IndexFunc(options, func(o manpage.Option) bool { return slices.Contains(o.Names, name) })
	if i < 0 {
		return Result{}, false, nil
	}
	return Result{Markdown: fmt.Sprintf("**%s**\n\n%s", strings.Join(options[i].Names, ", "), options[i].Doc), Span: span}, true, nil
}

func found(r Result, err error) (Result, bool, error) {
	switch {
	case errors.Is(err, manpage.ErrNoPage):
		return Result{}, false, nil
	case err != nil:
		return Result{}, false, err
	}
	return r, true, nil
}

func (h hoverer) isFunction(name string) bool {
	if _, ok := h.Tree.FunctionNamed(name); ok {
		return true
	}
	_, _, ok := parser.ImportedFunction(h.Imports, name)
	return ok
}

func (h hoverer) commandName(ctx context.Context, name string, span parser.Span) (Result, bool, error) {
	if fn, ok := h.Tree.FunctionNamed(name); ok {
		return Result{Markdown: h.function(fn), Span: span}, true, nil
	}
	if imp, fn, ok := parser.ImportedFunction(h.Imports, name); ok {
		return Result{Markdown: ImportedFunctionDoc(imp, fn), Span: span}, true, nil
	}
	if entry, ok := shelldocs.Builtin(name); ok {
		return Result{Markdown: entry.Markdown(h.dialect), Span: span}, true, nil
	}
	if entry, ok := shelldocs.Keyword(name); ok {
		return Result{Markdown: entry.Markdown(h.dialect), Span: span}, true, nil
	}
	if h.Commands == nil {
		return Result{}, false, nil
	}
	doc, err := h.Commands.Describe(ctx, name)
	if err != nil {
		return Result{}, false, err
	}
	return Result{Markdown: doc, Span: span}, true, nil
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
	for def := range tree.DefinitionsIn(fn.Body).All() {
		if def.Value == nil {
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
	exp, ok := parser.SoleParam(w)
	if !ok {
		return 0, false
	}
	if kind, _ := parser.ParamKindOf(exp.Param.Value); kind != parser.ParamPositional || exp.Length || exp.Excl || exp.Slice != nil || exp.Repl != nil {
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
	parser.Walk(fn.Decl.Body, func(node syntax.Node) bool {
		if _, nested := node.(*syntax.FuncDecl); nested {
			return false
		}
		exp, ok := node.(*syntax.ParamExp)
		if !ok || exp.Param == nil || !exp.Param.ValuePos.IsValid() {
			return true
		}
		switch kind, _ := parser.ParamKindOf(exp.Param.Value); kind {
		case parser.ParamAll, parser.ParamCount:
			variadic = true
		case parser.ParamPositional:
			n, err := strconv.Atoi(exp.Param.Value)
			if err == nil {
				highest = max(highest, n)
			}
		default:
		}
		return true
	})
	return highest, variadic
}

func expansionDocs(exp *syntax.ParamExp, d dialect.Dialect) []string {
	var docs []string
	add := func(v shelldocs.Vocabulary, key string) {
		if e, ok := shelldocs.Term(v, key, d); ok {
			docs = append(docs, termDoc(e))
		}
	}
	switch {
	case exp.Length:
		add(shelldocs.ExpansionForms, "length")
	case exp.Excl && exp.Index != nil && parser.IsAllIndex(exp.Index):
		add(shelldocs.ExpansionForms, "keys")
	case exp.Excl:
		add(shelldocs.ExpansionForms, "indirect")
	case exp.Names != 0:
		add(shelldocs.ExpansionForms, "names")
	case exp.Width:
		add(shelldocs.ExpansionForms, "width")
	case exp.IsSet:
		add(shelldocs.ExpansionForms, "isset")
	}
	switch {
	case exp.Slice != nil:
		add(shelldocs.ExpansionOperators, ":")
	case exp.Repl != nil:
		add(shelldocs.ExpansionOperators, replaceOperator(exp.Repl))
	case exp.Exp != nil && exp.Exp.Op == syntax.OtherParamOps && exp.Exp.Word != nil:
		add(shelldocs.ExpansionOperators, "@"+exp.Exp.Word.Lit())
	case exp.Exp != nil:
		add(shelldocs.ExpansionOperators, exp.Exp.Op.String())
	}
	if exp.Flags != nil {
		if flags := zshFlagDocs(exp.Flags); flags != "" {
			docs = append(docs, flags)
		}
	}
	if len(exp.Modifiers) > 0 {
		docs = append(docs, zshModifierDocs(exp.Modifiers))
	}
	return docs
}

func termDoc(e shelldocs.Entry) string {
	if e.Synopsis == "" {
		return e.Doc
	}
	return fmt.Sprintf("`%s`: %s", e.Synopsis, e.Doc)
}

func replaceOperator(r *syntax.Replace) string {
	if r.All {
		return "//"
	}
	if r.Orig != nil {
		if lit := r.Orig.Lit(); strings.HasPrefix(lit, "#") || strings.HasPrefix(lit, "%") {
			return "/" + lit[:1]
		}
	}
	return "/"
}

func zshFlagDocs(flags *syntax.Lit) string {
	var lines []string
	for _, flag := range parser.ParameterFlags(flags) {
		if e, ok := shelldocs.Term(shelldocs.ZshParameterFlags, string(flag.Name), dialect.Zsh); ok {
			lines = append(lines, fmt.Sprintf("- `%c`: %s", flag.Name, e.Doc))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "zsh expansion flags:\n" + strings.Join(lines, "\n")
}

func zshModifierDocs(modifiers []*syntax.Lit) string {
	lines := make([]string, 0, len(modifiers))
	for _, m := range modifiers {
		if e, ok := shelldocs.ZshModifier(m.Value); ok {
			lines = append(lines, fmt.Sprintf("- `:%s`: %s", m.Value, e.Doc))
		}
	}
	return "zsh modifiers:\n" + strings.Join(lines, "\n")
}

func (h hoverer) line(offset int) int {
	return h.Tree.SourceLine(offset)
}

func (h hoverer) snippet(offset int) string {
	start, end := textline.Bounds(h.text, offset)
	line := strings.TrimSpace(h.text[start:end])
	if len(line) > snippetWidth {
		cut := snippetWidth
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		line = line[:cut] + "…"
	}
	return line
}
