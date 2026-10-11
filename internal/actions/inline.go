package actions

import (
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

type useContext uint8

const (
	unquotedUse useContext = iota
	conditionalUse
	doubleQuotedUse
	heredocUse
	arithmeticUse
)

type use struct {
	span    parser.Span
	context useContext
}

func inlineVariable(s *site) (Change, bool) {
	tree := s.Tree
	name, ok := variableAt(s.pathAt(), s.sel.Start)
	if !ok || strings.HasPrefix(name, "LC_") || tree.AttrsAnywhere(name) != 0 {
		return Change{}, false
	}
	if _, special := shelldocs.Variable(name); special {
		return Change{}, false
	}
	def, ok := soleDefinition(tree, name)
	if !ok {
		return Change{}, false
	}
	value, ok := inlinableValue(def)
	if !ok {
		return Change{}, false
	}
	reads, ok := readsFollowing(tree, def)
	if !ok {
		return Change{}, false
	}
	sourced, ok := sourcedTrees(s.Document)
	if !ok {
		return Change{}, false
	}
	v := variableScan{name: name, callables: callables(tree, sourced)}
	if slices.ContainsFunc(sourced, v.touchedBy) {
		return Change{}, false
	}
	removal, ok := assignmentRemoval(tree, def)
	if !ok {
		return Change{}, false
	}
	uses, ok := v.usesIn(tree)
	if !ok || len(uses) != reads {
		return Change{}, false
	}
	edits := []textedit.Edit{removal}
	for _, u := range uses {
		text, ok := inlinedText(value, u.context)
		if !ok {
			return Change{}, false
		}
		edits = append(edits, textedit.Edit{Start: u.span.Start, End: u.span.End, NewText: text})
	}
	return Change{Title: "Inline variable " + name, Edits: edits}, true
}

func variableAt(path []syntax.Node, at int) (string, bool) {
	for i, node := range slices.Backward(path) {
		switch n := node.(type) {
		case *syntax.Assign:
			if n.Name != nil && litSpan(n.Name).Covers(at) {
				return n.Name.Value, true
			}
		case *syntax.ParamExp:
			if n.Param != nil && litSpan(n.Param).Covers(at) {
				return n.Param.Value, true
			}
		case *syntax.Word:
			if i > 0 {
				if name, ok := parser.ArithmeticRead(path[i-1], n); ok {
					return name, true
				}
			}
		}
	}
	return "", false
}

func litSpan(l *syntax.Lit) parser.Span {
	return parser.Span{Start: parser.Offset(l.ValuePos), End: parser.Offset(l.ValueEnd)}
}

func soleDefinition(tree *parser.Tree, name string) (*parser.Definition, bool) {
	var def *parser.Definition
	for i := range tree.Definitions {
		if tree.Definitions[i].Name != name {
			continue
		}
		if def != nil {
			return nil, false
		}
		def = &tree.Definitions[i]
	}
	return def, def != nil
}

func inlinableValue(def *parser.Definition) (string, bool) {
	assign, ok := def.Node.(*syntax.Assign)
	if !ok || def.Attrs != 0 || def.Cleared != 0 || assign.Append || assign.Naked || assign.Index != nil || assign.Array != nil {
		return "", false
	}
	switch def.Kind {
	case parser.DefAssign:
	case parser.DefDeclare:
		if !def.Local {
			return "", false
		}
	default:
		return "", false
	}
	if assign.Value == nil {
		return "", true
	}
	for _, part := range assign.Value.Parts {
		if lit, ok := part.(*syntax.Lit); ok && strings.Contains(lit.Value, "~") {
			return "", false
		}
	}
	return parser.Literal(assign.Value)
}

func sourcedTrees(doc Document) ([]*parser.Tree, bool) {
	if !slices.ContainsFunc(doc.Tree.Commands, parser.Command.IsSource) {
		return nil, true
	}
	if doc.SourcedTrees == nil {
		return nil, false
	}
	return doc.SourcedTrees()
}

func callables(tree *parser.Tree, sourced []*parser.Tree) map[string]bool {
	out := map[string]bool{}
	for _, t := range append([]*parser.Tree{tree}, sourced...) {
		for _, fn := range t.Functions {
			out[fn.Name] = true
		}
		for _, c := range t.Commands {
			if c.Name != "alias" {
				continue
			}
			for _, arg := range c.Args {
				if lit, ok := parser.Literal(arg); ok {
					name, _, _ := strings.Cut(lit, "=")
					out[name] = true
				}
			}
		}
	}
	return out
}

func readsFollowing(tree *parser.Tree, def *parser.Definition) (int, bool) {
	reads := 0
	for _, ref := range tree.References {
		if ref.Name != def.Name {
			continue
		}
		if ref.Within != def.Within || ref.Span.Start < def.Span.End {
			return 0, false
		}
		reads++
	}
	return reads, reads > 0
}

func assignmentRemoval(tree *parser.Tree, def *parser.Definition) (textedit.Edit, bool) {
	path := tree.PathAt(def.NameSpan.Start)
	k := slices.IndexFunc(path, func(n syntax.Node) bool { return n == def.Node }) - 2
	if k < 1 {
		return textedit.Edit{}, false
	}
	stmt, ok := path[k].(*syntax.Stmt)
	if !ok || !soleAssignment(stmt, def.Node) || !ownsScopeBody(tree, def.Within, path[k-1]) {
		return textedit.Edit{}, false
	}
	span, _ := tree.NodeSpan(stmt)
	text := tree.Text()
	if !startsLine(text, span.Start) {
		return textedit.Edit{}, false
	}
	end := lineEnd(text, span.End)
	if rest := strings.TrimSpace(text[span.End:end]); rest == "" || strings.HasPrefix(rest, "#") {
		return textedit.Edit{Start: lineStart(text, span.Start), End: min(end+1, len(text))}, true
	}
	if !stmt.Semicolon.IsValid() {
		return textedit.Edit{}, false
	}
	next := span.End + len(text[span.End:end]) - len(strings.TrimLeft(text[span.End:end], " \t"))
	return textedit.Edit{Start: span.Start, End: next}, true
}

func soleAssignment(stmt *syntax.Stmt, assign syntax.Node) bool {
	if stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown || len(stmt.Redirs) > 0 {
		return false
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		return len(cmd.Args) == 0 && len(cmd.Assigns) == 1 && cmd.Assigns[0] == assign
	case *syntax.DeclClause:
		return cmd.Variant.Value == "local" && len(cmd.Args) == 1 && cmd.Args[0] == assign
	}
	return false
}

func ownsScopeBody(tree *parser.Tree, scope parser.ScopeID, parent syntax.Node) bool {
	if scope == parser.FileScope {
		_, ok := parent.(*syntax.File)
		return ok
	}
	fn, ok := tree.EnclosingFunction(scope)
	return ok && fn.Decl.Body.Cmd == parent
}

type variableScan struct {
	name      string
	callables map[string]bool
}

var (
	evaluatingCommands = map[string]bool{"eval": true, "trap": true, "alias": true}
	inertBuiltins      = map[string]bool{"echo": true, "cd": true, "pwd": true, "true": true, "false": true, ":": true, "type": true}
	inertExternals     = dialect.SetOf(dialect.Bash, dialect.Posix, dialect.Dash, dialect.Busybox)
)

func (v variableScan) touchedBy(tree *parser.Tree) bool {
	for _, d := range tree.Definitions {
		if d.Name == v.name && !d.Local {
			return true
		}
	}
	for _, r := range tree.References {
		if r.Name != v.name {
			continue
		}
		if _, local := tree.Binding(r.Name, r.Within); !local {
			return true
		}
	}
	_, ok := v.usesIn(tree)
	return !ok
}

func (v variableScan) usesIn(tree *parser.Tree) ([]use, bool) {
	var uses []use
	var stack []syntax.Node
	ok := true
	parser.Walk(tree.File, func(node syntax.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if !ok {
			return false
		}
		switch n := node.(type) {
		case *syntax.ParamExp:
			if n.Param != nil && n.Param.Value == v.name && n.Names == 0 {
				context, known := useContextOf(stack)
				span, _ := tree.NodeSpan(n)
				ok = known && parser.IsPlainParam(n)
				uses = append(uses, use{span: span, context: context})
			}
		case *syntax.CallExpr:
			ok = !v.evaluatedBy(tree, n)
		case *syntax.Word:
			parent := stack[len(stack)-1]
			if name, read := parser.ArithmeticRead(parent, n); read {
				if name == v.name {
					span, _ := tree.NodeSpan(n)
					uses = append(uses, use{span: span, context: arithmeticUse})
				}
				break
			}
			if lit, literal := parser.Literal(n); literal && namesVariable(lit, v.name) && !v.inertOperand(tree, parent, n) {
				ok = false
			}
		}
		stack = append(stack, node)
		return true
	})
	return uses, ok
}

func (v variableScan) evaluatedBy(tree *parser.Tree, call *syntax.CallExpr) bool {
	k := parser.CommandStart(call.Args, tree.Bats)
	if k < 0 {
		return false
	}
	command, literal := parser.Literal(call.Args[k])
	evaluating := evaluatingCommands[command]
	if literal && !evaluating {
		return false
	}
	for _, arg := range call.Args[k+1:] {
		lit, ok := parser.Literal(arg)
		if (!ok && evaluating) || (ok && mentions(lit, v.name)) {
			return true
		}
	}
	return false
}

func (v variableScan) inertOperand(tree *parser.Tree, parent syntax.Node, w *syntax.Word) bool {
	switch p := parent.(type) {
	case *syntax.Redirect:
		return p.Word == w
	case *syntax.CallExpr:
		i := slices.Index(p.Args, w)
		k := parser.CommandStart(p.Args, tree.Bats)
		if i < 0 {
			return false
		}
		if k < 0 || i <= k {
			return true
		}
		command, ok := parser.Literal(p.Args[k])
		return ok && v.inertCommand(command, tree.Dialect)
	}
	return false
}

func (v variableScan) inertCommand(command string, d dialect.Dialect) bool {
	if v.callables[command] {
		return false
	}
	if inertBuiltins[command] {
		return true
	}
	_, builtin := shelldocs.Builtin(command)
	_, keyword := shelldocs.Keyword(command)
	return !builtin && !keyword && inertExternals.Has(d)
}

func mentions(text, name string) bool {
	for at := 0; ; {
		i := strings.Index(text[at:], name)
		if i < 0 {
			return false
		}
		start, end := at+i, at+i+len(name)
		if (start == 0 || !parser.IsNameChar(text[start-1])) && (end == len(text) || !parser.IsNameChar(text[end])) {
			return true
		}
		at = start + 1
	}
}

func namesVariable(word, name string) bool {
	rest, ok := strings.CutPrefix(word, name)
	return ok && (rest == "" || rest[0] == '=' || rest[0] == '[' || strings.HasPrefix(rest, "+="))
}

func useContextOf(ancestors []syntax.Node) (useContext, bool) {
	context := unquotedUse
	for i, node := range slices.Backward(ancestors) {
		switch n := node.(type) {
		case *syntax.ParamExp:
			return 0, false
		case *syntax.DblQuoted:
			if n.Dollar {
				return 0, false
			}
			return doubleQuotedUse, true
		case *syntax.Redirect:
			if i+1 < len(ancestors) && n.Hdoc == ancestors[i+1] {
				return heredocUse, true
			}
			return context, true
		case *syntax.TestClause:
			context = conditionalUse
		case *syntax.Stmt, *syntax.CmdSubst, *syntax.ProcSubst:
			return context, true
		default:
			if parser.IsArithmetic(n) {
				return arithmeticUse, true
			}
		}
	}
	return context, true
}

func inlinedText(value string, context useContext) (string, bool) {
	switch context {
	case doubleQuotedUse:
		return backslashEscaped(value, doubleQuoteSpecials), true
	case heredocUse:
		return backslashEscaped(value, heredocSpecials), true
	case arithmeticUse:
		return value, parser.IsDigits(value)
	}
	if plainWord(value) {
		return value, true
	}
	if value == "" || strings.ContainsAny(value, " \t\n*?[") || context == conditionalUse {
		return "", false
	}
	return singleQuoted(value), true
}

func plainWord(s string) bool {
	if s == "" {
		return false
	}
	if _, keyword := shelldocs.Keyword(s); keyword {
		return false
	}
	for i := range len(s) {
		if !parser.IsNameChar(s[i]) && !strings.ContainsRune("./:,+@%-", rune(s[i])) {
			return false
		}
	}
	return true
}
