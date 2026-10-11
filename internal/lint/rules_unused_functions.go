package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const codeUnusedFunction = nativeBase + 2001

var unusedFunctionsFamily = family{
	codes: map[Code]meta{
		codeUnusedFunction: {severity: SeverityStyle, tag: TagUnnecessary},
	},
	rules: []rule{{finish: checkUnusedFunctions}},
}

const kshMathFunctions = ".sh.math."

var shellHooks = setOf(
	"command_not_found_handle", "command_not_found_handler",
	"precmd", "preexec", "chpwd", "periodic", "zshaddhistory", "zshexit",
)

var functionIntrospection = map[string][]string{
	"compgen": {"-A"},
	"declare": {"-f", "-F"},
	"typeset": {"-f", "-F"},
}

type usageRegion struct {
	words    map[string]bool
	prefixes []string
	dynamic  bool
}

func newRegion() *usageRegion { return &usageRegion{words: map[string]bool{}} }

type functionUsage struct {
	top    *usageRegion
	bodies map[*syntax.FuncDecl]*usageRegion
}

var interactiveCommands = setOf("bind", "bindkey", "compdef", "compinit", "complete", "promptinit", "add-zsh-hook")

func checkUnusedFunctions(p *pass) {
	if len(p.tree.Functions) == 0 || p.bats || p.rcFile || p.sourceable() || p.libraryShaped() || p.configuresInteractiveShell() {
		return
	}
	if _, executable := dialect.Interpreter(p.text); !executable {
		return
	}
	usage := p.collectUsage(p.tree.File, true)
	for _, source := range p.sources {
		usage.top.merge(p.collectUsage(source.Tree.File, false).top)
	}
	used, complete := reachableFunctions(p.tree.Functions, usage)
	if !complete {
		return
	}
	for _, fn := range p.tree.Functions {
		if fn.Decl == nil || used[fn.Decl] || shellHooks[fn.Name] || strings.HasPrefix(fn.Name, "TRAP") || kshDiscipline(fn.Name) {
			continue
		}
		p.report(codeUnusedFunction, fn.NameSpan, fn.Name+" is never called: nothing in this script, or in the files it sources, runs it.")
	}
}

func kshDiscipline(name string) bool {
	return strings.Contains(name, ".") && !strings.HasPrefix(name, kshMathFunctions)
}

var sourcedMarkers = []string{"BASH_SOURCE", "ZSH_EVAL_CONTEXT", "zsh_eval_context", "(%):-%N", "(%):-%x"}

func (p *pass) sourceable() bool {
	if slices.ContainsFunc(sourcedMarkers, func(marker string) bool { return strings.Contains(p.text, marker) }) {
		return true
	}
	for _, stmt := range p.tree.File.Stmts {
		if call, ok := stmt.Cmd.(*syntax.CallExpr); ok && len(call.Args) > 0 && call.Args[0].Lit() == "return" {
			return true
		}
	}
	return comparesScriptName(p.tree.File)
}

func (p *pass) configuresInteractiveShell() bool {
	return slices.ContainsFunc(p.tree.Commands, func(c parser.Command) bool { return interactiveCommands[c.Name] })
}

func comparesScriptName(root syntax.Node) bool {
	compares := false
	parser.Walk(root, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.CaseClause:
			compares = compares || isScriptName(n.Word)
		case *syntax.BinaryTest:
			x, _ := n.X.(*syntax.Word)
			y, _ := n.Y.(*syntax.Word)
			compares = compares || isScriptName(x) || isScriptName(y)
		case *syntax.CallExpr:
			if len(n.Args) == 0 {
				return true
			}
			if name := n.Args[0].Lit(); name == "test" || name == "[" {
				compares = compares || slices.ContainsFunc(n.Args[1:], isScriptName)
			}
		}
		return !compares
	})
	return compares
}

func isScriptName(w *syntax.Word) bool {
	if w == nil {
		return false
	}
	pe, ok := solePart(w).(*syntax.ParamExp)
	return ok && pe.Param != nil && pe.Param.Value == "0"
}

var setupCommands = setOf(":", "true", "export", "readonly", "declare", "typeset", "local", "set", "shopt", "unset",
	"trap", "alias", "umask", "source", ".", "test", "[", "return", "hash", "builtin", "command", "setopt", "autoload", "zmodload")

func (p *pass) libraryShaped() bool {
	return !slices.ContainsFunc(p.tree.File.Stmts, func(stmt *syntax.Stmt) bool { return !p.setupStatement(stmt) })
}

func (p *pass) setupStatement(stmt *syntax.Stmt) bool {
	switch cmd := stmt.Cmd.(type) {
	case *syntax.FuncDecl, *syntax.DeclClause, *syntax.TestClause, *syntax.ArithmCmd:
		return true
	case *syntax.CallExpr:
		return p.setupCall(cmd)
	case *syntax.BinaryCmd:
		return p.setupStatement(cmd.X) && p.setupStatement(cmd.Y)
	case *syntax.Block:
		return p.setupStatements(cmd.Stmts)
	case *syntax.IfClause:
		for clause := cmd; clause != nil; clause = clause.Else {
			if !p.setupStatements(clause.Cond) || !p.setupStatements(clause.Then) {
				return false
			}
		}
		return true
	case *syntax.ForClause:
		return p.setupStatements(cmd.Do)
	case *syntax.RepeatClause:
		return p.setupStatements(cmd.Do)
	case *syntax.TryClause:
		return p.setupStatements(cmd.Try.Stmts) && p.setupStatements(cmd.Always.Stmts)
	case *syntax.CaseClause:
		return !slices.ContainsFunc(cmd.Items, func(item *syntax.CaseItem) bool { return !p.setupStatements(item.Stmts) })
	}
	return false
}

func (p *pass) setupStatements(stmts []*syntax.Stmt) bool {
	return !slices.ContainsFunc(stmts, func(stmt *syntax.Stmt) bool { return !p.setupStatement(stmt) })
}

func (p *pass) setupCall(call *syntax.CallExpr) bool {
	if len(call.Args) == 0 {
		return true
	}
	name, ok := parser.Literal(call.Args[0])
	if !ok {
		return false
	}
	if setupCommands[name] {
		return true
	}
	if _, own := p.tree.FunctionNamed(name); !own {
		return false
	}
	return !slices.ContainsFunc(call.Args[1:], forwardsArguments)
}

func forwardsArguments(w *syntax.Word) bool {
	forwards := false
	parser.Walk(w, func(n syntax.Node) bool {
		if pe, ok := n.(*syntax.ParamExp); ok && readsPositional(pe) {
			forwards = true
		}
		return !forwards
	})
	return forwards
}

func (r *usageRegion) merge(o *usageRegion) {
	for w := range o.words {
		r.words[w] = true
	}
	r.prefixes = append(r.prefixes, o.prefixes...)
	r.dynamic = r.dynamic || o.dynamic
}

func (p *pass) collectUsage(root syntax.Node, attributeBodies bool) functionUsage {
	usage := functionUsage{top: newRegion(), bodies: map[*syntax.FuncDecl]*usageRegion{}}
	var regions []*usageRegion
	current := func() *usageRegion {
		if len(regions) == 0 {
			return usage.top
		}
		return regions[len(regions)-1]
	}
	var stack []syntax.Node
	parser.Walk(root, func(n syntax.Node) bool {
		if n == nil {
			if _, ok := stack[len(stack)-1].(*syntax.FuncDecl); ok && attributeBodies {
				regions = regions[:len(regions)-1]
			}
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		switch n := n.(type) {
		case *syntax.FuncDecl:
			if attributeBodies {
				region := newRegion()
				usage.bodies[n] = region
				regions = append(regions, region)
			}
		case *syntax.CallExpr:
			p.noteCommand(current(), n)
		case *syntax.FuncArithm:
			current().words[n.Name.Value] = true
			current().words[kshMathFunctions+n.Name.Value] = true
		case *syntax.Word:
			for token := range literalTokens(n) {
				current().words[token] = true
			}
		}
		return true
	})
	return usage
}

func (p *pass) noteCommand(region *usageRegion, call *syntax.CallExpr) {
	if len(call.Args) == 0 {
		return
	}
	word := p.effectiveCommandWord(call)
	if word == nil {
		return
	}
	name, literal := parser.Literal(word)
	if !literal {
		if lit, ok := word.Parts[0].(*syntax.Lit); ok && lit.Value != "" {
			region.prefixes = append(region.prefixes, lit.Value)
			return
		}
		region.dynamic = true
		return
	}
	flags, introspects := functionIntrospection[name]
	if name == "eval" || introspects && introspectsFunctions(call, flags) {
		region.dynamic = true
	}
}

func introspectsFunctions(call *syntax.CallExpr, flags []string) bool {
	for _, arg := range call.Args[1:] {
		lit, ok := parser.Literal(arg)
		if !ok {
			return true
		}
		for _, flag := range flags {
			if lit == flag || strings.HasPrefix(lit, "-") && strings.Contains(lit, strings.TrimPrefix(flag, "-")) {
				return true
			}
		}
	}
	return false
}

func literalTokens(w *syntax.Word) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		var visit func(parts []syntax.WordPart) bool
		visit = func(parts []syntax.WordPart) bool {
			for _, part := range parts {
				var text string
				switch part := part.(type) {
				case *syntax.Lit:
					text = part.Value
				case *syntax.SglQuoted:
					text = part.Value
				case *syntax.DblQuoted:
					if !visit(part.Parts) {
						return false
					}
					continue
				default:
					continue
				}
				for _, token := range strings.FieldsFunc(text, isTokenBoundary) {
					if !yield(token) {
						return false
					}
				}
			}
			return true
		}
		visit(w.Parts)
	}
}

func isTokenBoundary(r rune) bool {
	return strings.ContainsRune(" \t\n;|&()<>`'\"${}=,!*?[]#\\", r)
}

func reachableFunctions(functions []parser.Function, usage functionUsage) (map[*syntax.FuncDecl]bool, bool) {
	byName := map[string][]*syntax.FuncDecl{}
	for _, fn := range functions {
		if fn.Decl == nil {
			continue
		}
		byName[fn.Name] = append(byName[fn.Name], fn.Decl)
		if fn.Namespace != "" {
			qualified := "." + fn.Namespace + "." + fn.Name
			byName[qualified] = append(byName[qualified], fn.Decl)
		}
	}
	used := map[*syntax.FuncDecl]bool{}
	pending := []*usageRegion{usage.top}
	for len(pending) > 0 {
		region := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if region.dynamic {
			return nil, false
		}
		for name, decls := range byName {
			if !region.words[name] && !hasAnyPrefix(name, region.prefixes) {
				continue
			}
			for _, decl := range decls {
				if !used[decl] {
					used[decl] = true
					pending = append(pending, usage.bodies[decl])
				}
			}
		}
	}
	return used, true
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
