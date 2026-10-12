package lint

import (
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const codeImplicitGlobal = nativeBase + 2014

var localVariablesFamily = family{
	codes: map[Code]meta{
		codeImplicitGlobal: {severity: SeverityStyle, optional: optionalLocalVariables},
	},
	finish: []func(*pass){checkImplicitGlobals},
}

type variableScopes struct {
	assigned   map[string]map[parser.ScopeID]bool
	referenced map[string]map[parser.ScopeID]bool
	declared   map[string]bool
	imported   map[string]bool
}

func (p *pass) variableScopes() variableScopes {
	v := variableScopes{assigned: map[string]map[parser.ScopeID]bool{}, referenced: map[string]map[parser.ScopeID]bool{}, declared: map[string]bool{}, imported: map[string]bool{}}
	for _, def := range p.tree.Definitions {
		switch {
		case def.Kind == parser.DefCommandEnv || def.Local:
		case def.Kind == parser.DefDeclare:
			v.declared[def.Name] = true
		default:
			addScope(v.assigned, def.Name, def.Within)
		}
	}
	for _, ref := range p.tree.References {
		addScope(v.referenced, ref.Name, ref.Within)
	}
	for _, imp := range p.imports {
		for _, def := range imp.Tree.Definitions {
			v.imported[def.Name] = true
		}
		for _, ref := range imp.Tree.References {
			v.imported[ref.Name] = true
		}
	}
	return v
}

func addScope(table map[string]map[parser.ScopeID]bool, name string, scope parser.ScopeID) {
	if table[name] == nil {
		table[name] = map[parser.ScopeID]bool{}
	}
	table[name][scope] = true
}

func (v variableScopes) sharedOutside(name string, scope parser.ScopeID) bool {
	if v.imported[name] {
		return true
	}
	for other := range v.referenced[name] {
		if other != scope && !v.assigned[name][other] {
			return true
		}
	}
	return false
}

func writesVariable(kind parser.DefinitionKind) bool {
	switch kind {
	case parser.DefAssign, parser.DefLoop, parser.DefArithmetic, parser.DefBuiltin:
		return true
	case parser.DefCommandEnv, parser.DefDeclare, parser.DefDescriptor:
	}
	return false
}

func checkImplicitGlobals(p *pass) {
	if !p.active(codeImplicitGlobal) || !p.dialect.Features().Local {
		return
	}
	scopes := p.variableScopes()
	reported := map[string]map[parser.ScopeID]bool{}
	for _, def := range p.tree.Definitions {
		if def.Within == parser.FileScope || def.Local || !writesVariable(def.Kind) || p.parameters.Has(def.Name) ||
			scopes.declared[def.Name] || scopes.assigned[def.Name][parser.FileScope] || reported[def.Name][def.Within] ||
			scopes.sharedOutside(def.Name, def.Within) || p.localBefore(def) {
			continue
		}
		fn, _ := p.tree.EnclosingFunction(def.Within)
		keyword := "local"
		if p.dialect == dialect.Ksh {
			if fn.Decl == nil || !fn.Decl.RsrvWord {
				continue
			}
			keyword = "typeset"
		}
		addScope(reported, def.Name, def.Within)
		p.report(codeImplicitGlobal, def.NameSpan, def.Name+" is assigned in "+fn.Name+" without being declared "+keyword+", so it changes or creates a global variable; add "+keyword+" "+def.Name+" at the start of the function.")
	}
}

func (p *pass) localBefore(def parser.Definition) bool {
	for scope := def.Within; ; scope = p.tree.Scopes[scope].Parent {
		for other := range p.tree.DefinitionsIn(scope).All() {
			if other.Name == def.Name && other.Local && (scope != def.Within || other.Span.Start <= def.Span.Start) {
				return true
			}
		}
		if scope == parser.FileScope {
			return false
		}
	}
}
