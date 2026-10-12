package lint

import (
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const (
	codeEvalInjection       = nativeBase + 1001
	codeShellInjection      = nativeBase + 1002
	codeArithmeticInjection = nativeBase + 1003
	codeSourceInjection     = nativeBase + 1004
)

var injectionFamily = family{
	codes: map[Code]meta{
		codeEvalInjection:       {severity: SeverityWarning},
		codeShellInjection:      {severity: SeverityWarning},
		codeArithmeticInjection: {severity: SeverityWarning, optional: optionalArithmeticInjection},
		codeSourceInjection:     {severity: SeverityWarning},
	},
	nodes: []nodeRule{
		onNode(checkInjectedCommand),
		onNodes(checkInjectedArithmetic, &syntax.ArithmCmd{}, &syntax.ArithmExp{}, &syntax.LetClause{}, &syntax.BinaryTest{}),
	},
}

type untrustedSource uint8

const (
	fromArguments untrustedSource = iota + 1
	fromInput
	fromOption
)

func (s untrustedSource) String() string {
	switch s {
	case fromArguments:
		return "the script's command-line arguments"
	case fromInput:
		return "input read at run time"
	case fromOption:
		return "a command-line option value (OPTARG)"
	}
	return "untrusted input"
}

const everyPosition = 0

type scopedName struct {
	scope parser.ScopeID
	name  string
}

type untrustedValues struct {
	tree      *parser.Tree
	vars      map[scopedName]untrustedSource
	params    map[string]map[int]untrustedSource
	validated map[scopedName][]int
}

func (p *pass) untrusted() *untrustedValues {
	if p.untrustedValues == nil {
		p.untrustedValues = analyzeUntrusted(p.tree)
	}
	return p.untrustedValues
}

func analyzeUntrusted(tree *parser.Tree) *untrustedValues {
	t := &untrustedValues{tree: tree, vars: map[scopedName]untrustedSource{}, params: map[string]map[int]untrustedSource{}, validated: map[scopedName][]int{}}
	t.collectValidations()
	for changed := true; changed; {
		changed = false
		for i := range tree.Definitions {
			def := &tree.Definitions[i]
			if src, ok := t.definitionSource(def); ok {
				changed = t.markVar(t.key(def.Name, def.Within), src) || changed
			}
		}
		for _, cmd := range tree.Commands {
			if _, isFunction := tree.FunctionNamed(cmd.Name); isFunction {
				changed = t.markArguments(cmd) || changed
			}
		}
	}
	return t
}

func (t *untrustedValues) collectValidations() {
	parser.Walk(t.tree.File, func(n syntax.Node) bool {
		var subject *syntax.Word
		switch n := n.(type) {
		case *syntax.BinaryTest:
			if validatingTestOperators[n.Op] {
				subject, _ = n.X.(*syntax.Word)
			}
		case *syntax.CaseClause:
			subject = n.Word
		case *syntax.Assign:
			if n.Name != nil && n.Value != nil && rewritesItself(n) {
				subject = n.Value
			}
		}
		if subject == nil {
			return true
		}
		if pe, ok := parser.SolePart(subject).(*syntax.ParamExp); ok && pe.Param != nil {
			offset := parser.Offset(n.Pos())
			key := t.key(pe.Param.Value, t.tree.ScopeAt(offset))
			t.validated[key] = append(t.validated[key], offset)
		}
		return true
	})
}

func rewritesItself(a *syntax.Assign) bool {
	pe, ok := parser.SolePart(a.Value).(*syntax.ParamExp)
	return ok && pe.Param != nil && pe.Param.Value == a.Name.Value && pe.Repl != nil
}

var validatingTestOperators = map[syntax.BinTestOperator]bool{
	syntax.TsReMatch: true, syntax.TsMatch: true, syntax.TsMatchShort: true, syntax.TsNoMatch: true,
}

func (t *untrustedValues) validatedBefore(name string, at int, scope parser.ScopeID) bool {
	return slices.ContainsFunc(t.validated[t.key(name, scope)], func(offset int) bool { return offset < at })
}

func (t *untrustedValues) key(name string, scope parser.ScopeID) scopedName {
	return bindingKey(t.tree, name, scope)
}

func bindingKey(tree *parser.Tree, name string, scope parser.ScopeID) scopedName {
	if !parser.IsName(name) {
		return scopedName{scope: scope, name: name}
	}
	bound, local := tree.Binding(name, scope)
	if !local {
		return scopedName{scope: parser.FileScope, name: name}
	}
	return scopedName{scope: bound, name: name}
}

func (t *untrustedValues) markVar(key scopedName, src untrustedSource) bool {
	if _, known := t.vars[key]; known {
		return false
	}
	t.vars[key] = src
	return true
}

func (t *untrustedValues) markArguments(cmd parser.Command) bool {
	changed := false
	for i, arg := range cmd.Args {
		_, src, tainted := t.expansion(arg, cmd.Within)
		if !tainted {
			continue
		}
		position := i + 1
		if expandsAllPositionals(arg) {
			position = everyPosition
		}
		if t.params[cmd.Name] == nil {
			t.params[cmd.Name] = map[int]untrustedSource{}
		}
		if _, known := t.params[cmd.Name][position]; !known {
			t.params[cmd.Name][position] = src
			changed = true
		}
	}
	return changed
}

func expandsAllPositionals(w *syntax.Word) bool {
	pe, ok := parser.SoleParam(w)
	if !ok {
		return false
	}
	kind, _ := parser.ParamKindOf(pe.Param.Value)
	return kind == parser.ParamAll
}

func (t *untrustedValues) definitionSource(def *parser.Definition) (untrustedSource, bool) {
	switch def.Kind {
	case parser.DefAssign, parser.DefDeclare, parser.DefCommandEnv:
		if assign, ok := def.Node.(*syntax.Assign); ok && assign.Array != nil {
			for _, elem := range assign.Array.Elems {
				if elem.Value == nil {
					continue
				}
				if _, src, tainted := t.expansion(elem.Value, def.Within); tainted {
					return src, true
				}
			}
		}
		if def.Value == nil {
			return 0, false
		}
		_, src, tainted := t.expansion(def.Value, def.Within)
		return src, tainted
	case parser.DefLoop:
		iter, ok := def.Node.(*syntax.WordIter)
		if !ok {
			return 0, false
		}
		if !parser.ListsWords(iter) {
			return t.positional(everyPosition, def.Within)
		}
		for _, item := range iter.Items {
			if _, src, tainted := t.expansion(item, def.Within); tainted {
				return src, true
			}
		}
	case parser.DefBuiltin:
		call, ok := def.Node.(*syntax.CallExpr)
		if !ok {
			return 0, false
		}
		c, ok := commandOf(call)
		if !ok {
			return 0, false
		}
		switch c.name {
		case "read", "mapfile", "readarray":
			return fromInput, true
		case "printf":
			if printsOnlyNumbers(c.args) {
				return 0, false
			}
			for _, arg := range c.args {
				if _, src, tainted := t.expansion(arg, def.Within); tainted {
					return src, true
				}
			}
		}
	}
	return 0, false
}

func (t *untrustedValues) expansion(root syntax.Node, scope parser.ScopeID) (*syntax.ParamExp, untrustedSource, bool) {
	var found *syntax.ParamExp
	var src untrustedSource
	parser.Walk(root, func(n syntax.Node) bool {
		if found != nil || producesOutput(n) {
			return false
		}
		pe, ok := n.(*syntax.ParamExp)
		if !ok || pe.Param == nil || pe.Length || isAlternativeExpansion(pe) {
			return true
		}
		if s, tainted := t.param(pe.Param.Value, scope); tainted {
			found, src = pe, s
			return false
		}
		return true
	})
	return found, src, found != nil
}

func producesOutput(n syntax.Node) bool {
	switch n.(type) {
	case *syntax.CmdSubst, *syntax.ProcSubst:
		return true
	}
	return false
}

func (t *untrustedValues) param(name string, scope parser.ScopeID) (untrustedSource, bool) {
	kind, _ := parser.ParamKindOf(name)
	switch {
	case name == "OPTARG":
		return fromOption, true
	case kind == parser.ParamAll:
		return t.positional(everyPosition, scope)
	case kind == parser.ParamPositional:
		n, err := strconv.Atoi(name)
		if err != nil {
			return 0, false
		}
		return t.positional(n, scope)
	case parser.IsName(name):
		src, tainted := t.vars[t.key(name, scope)]
		return src, tainted
	}
	return 0, false
}

func (t *untrustedValues) positional(n int, scope parser.ScopeID) (untrustedSource, bool) {
	fn, inFunction := t.tree.FunctionOfBody(scope)
	if !inFunction {
		return fromArguments, true
	}
	params := t.params[fn.Name]
	if src, ok := params[everyPosition]; ok {
		return src, true
	}
	if n != everyPosition {
		src, ok := params[n]
		return src, ok
	}
	positions := make([]int, 0, len(params))
	for position := range params {
		positions = append(positions, position)
	}
	if len(positions) == 0 {
		return 0, false
	}
	return params[slices.Min(positions)], true
}

func checkInjectedCommand(p *pass, call *syntax.CallExpr) {
	command, ok := p.runs(call.Args)
	if !ok {
		return
	}
	name, args := command.name, command.args
	scope := p.tree.ScopeAt(parser.Offset(call.Pos()))
	switch {
	case name == "eval":
		if len(args) == 1 && wholeExpansion(args[0]) {
			return
		}
		for _, arg := range args {
			p.reportInjection(codeEvalInjection, arg, scope, "is pasted into the code eval runs")
		}
	case parser.IsSourceName(name):
		if len(args) > 0 && !wholeExpansion(args[0]) {
			p.reportInjection(codeSourceInjection, args[0], scope, "is pasted into the path of the file "+name+" runs, so ../ or an absolute path can load any file")
		}
	default:
		if shell, script, ok := shellScript(name, args); ok && !wholeExpansion(script) {
			p.reportInjection(codeShellInjection, script, scope, "is pasted into the script "+shell+" -c runs")
		}
	}
}

func wholeExpansion(w *syntax.Word) bool {
	_, ok := parser.SolePart(w).(*syntax.ParamExp)
	return ok
}

func shellScript(name string, args []*syntax.Word) (string, *syntax.Word, bool) {
	base := path.Base(name)
	if inv, ok := shellInvocationOf(base, args); ok {
		return base, inv.operand, inv.mode == runsCommandString
	}
	if base != "su" {
		return "", nil, false
	}
	for _, opt := range parseOptions(base, args).Options {
		if (opt.Name == "-c" || opt.Name == "--command") && opt.Argument != nil {
			return base, opt.Argument.Word, true
		}
	}
	return "", nil, false
}

func (p *pass) reportInjection(code Code, w *syntax.Word, scope parser.ScopeID, sink string) {
	t := p.untrusted()
	pe, src, tainted := t.expansion(w, scope)
	if !tainted || t.validatedBefore(pe.Param.Value, parser.Offset(pe.Pos()), scope) || p.restrictedByCase(pe) {
		return
	}
	span, ok := p.span(pe)
	if !ok {
		return
	}
	text := p.text[span.Start:span.End]
	p.report(code, span, text+" comes from "+src.String()+" and "+sink+"; whoever controls it can run arbitrary commands. Check it against a fixed set of values first, or pass it as an argument instead of building code from it.")
}

func (p *pass) restrictedByCase(pe *syntax.ParamExp) bool {
	for depth := 0; p.ancestor(depth) != nil; depth++ {
		item, ok := p.ancestor(depth).(*syntax.CaseItem)
		if !ok {
			continue
		}
		clause, ok := p.ancestor(depth + 1).(*syntax.CaseClause)
		if !ok {
			continue
		}
		subject, ok := parser.SolePart(clause.Word).(*syntax.ParamExp)
		if !ok || subject.Param == nil || subject.Param.Value != pe.Param.Value {
			continue
		}
		if !slices.ContainsFunc(item.Patterns, func(w *syntax.Word) bool {
			lit, ok := parser.Literal(w)
			return !ok || strings.ContainsAny(lit, "*?[")
		}) {
			return true
		}
	}
	return false
}

func checkInjectedArithmetic(p *pass, n syntax.Node) {
	if !p.active(codeArithmeticInjection) || !p.dialect.Features().SubscriptsRunCode {
		return
	}
	scope := p.tree.ScopeAt(parser.Offset(n.Pos()))
	t := p.untrusted()
	if test, ok := n.(*syntax.BinaryTest); ok {
		if !arithmeticTestOperators[test.Op] {
			return
		}
		for _, operand := range []syntax.TestExpr{test.X, test.Y} {
			if w, ok := operand.(*syntax.Word); ok {
				name, read := parser.ArithmeticName(w)
				p.checkArithmeticName(t, w, name, read, scope)
			}
		}
	}
	var stack []syntax.Node
	parser.Walk(n, func(child syntax.Node) bool {
		if child == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if producesOutput(child) {
			return false
		}
		switch child := child.(type) {
		case *syntax.ParamExp:
			if child.Param != nil && !child.Length && !isAlternativeExpansion(child) && !t.validatedBefore(child.Param.Value, parser.Offset(child.Pos()), scope) {
				if src, tainted := t.param(child.Param.Value, scope); tainted {
					p.reportArithmeticInjection(child, src)
				}
			}
		case *syntax.Word:
			if len(stack) > 0 {
				name, read := parser.ArithmeticRead(stack[len(stack)-1], child)
				p.checkArithmeticName(t, child, name, read, scope)
			}
		}
		stack = append(stack, child)
		return true
	})
}

func (p *pass) checkArithmeticName(t *untrustedValues, w *syntax.Word, name string, read bool, scope parser.ScopeID) {
	if !read || t.validatedBefore(name, parser.Offset(w.Pos()), scope) {
		return
	}
	if src, tainted := t.param(name, scope); tainted {
		p.reportArithmeticNameInjection(w, name, src)
	}
}

var arithmeticTestOperators = map[syntax.BinTestOperator]bool{
	syntax.TsEql: true, syntax.TsNeq: true, syntax.TsLss: true, syntax.TsLeq: true, syntax.TsGtr: true, syntax.TsGeq: true,
}

func (p *pass) arithmeticRisk() string {
	return " is evaluated as an arithmetic expression, and " + p.dialect.String() + " runs any command substitution inside an array subscript such as a[$(cmd)]; check that it is a plain number first."
}

func (p *pass) reportArithmeticInjection(pe *syntax.ParamExp, src untrustedSource) {
	if span, ok := p.span(pe); ok {
		p.report(codeArithmeticInjection, span, p.text[span.Start:span.End]+" comes from "+src.String()+" and"+p.arithmeticRisk())
	}
}

func (p *pass) reportArithmeticNameInjection(w *syntax.Word, name string, src untrustedSource) {
	if span, ok := p.span(w); ok {
		p.report(codeArithmeticInjection, span, "The value of "+name+" comes from "+src.String()+" and"+p.arithmeticRisk())
	}
}
