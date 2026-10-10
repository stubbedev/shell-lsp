package lint

import (
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var declarationFamily = family{
	codes: map[Code]meta{
		2155: {severity: SeverityWarning},
		2163: {severity: SeverityWarning},
		2168: {severity: SeverityError},
		2290: {severity: SeverityError},
		2316: {severity: SeverityError},
		2318: {severity: SeverityWarning},
	},
	commands: []commandRule{
		{match: matchExact, names: declaringCommands, check: checkMaskedStatus},
		{match: matchExact, names: declaringCommands, check: checkDeclaringCommandAsName},
		{match: matchExact, names: declaringCommands, check: checkReadInSameDeclaration},
		{match: matchExact, names: append([]string{"alias"}, declaringCommands...), check: checkSpacedEquals},
		{match: matchExact, names: []string{"local"}, check: checkLocalOutsideFunction},
		{match: matchExact, names: []string{"export"}, check: checkExportedExpansion},
	},
}

var (
	declaringCommands  = []string{"local", "declare", "export", "readonly", "typeset"}
	declaringOrLet     = setOf("local", "declare", "export", "readonly", "typeset", "let")
	scopedDeclarations = setOf("local", "declare", "typeset")
)

func (p *pass) inFunction() bool {
	if p.bats {
		return true
	}
	for _, n := range p.stack {
		if fn, ok := n.(*syntax.FuncDecl); ok && (fn.RsrvWord || p.dialect != dialect.Ksh) {
			return true
		}
	}
	return false
}

func checkMaskedStatus(p *pass, c command) {
	if c.decl == nil {
		return
	}
	flags := parseFlags(c.args, "")
	local := scopedDeclarations[c.name] && !flags.has('g', "") && p.inFunction()
	if local && (c.name == "readonly" || flags.has('r', "")) {
		return
	}
	for _, a := range c.decl.Args {
		if !a.Naked && a.Value != nil && substitutes(a.Value) {
			p.reportNode(2155, a, "Declare and assign "+a.Name.Value+" separately; "+c.name+" returns its own status and masks the status of the command substitution.")
		}
	}
}

func substitutes(w *syntax.Word) bool {
	for _, part := range w.Parts {
		if dq, ok := part.(*syntax.DblQuoted); ok && slices.ContainsFunc(dq.Parts, isSubstitution) {
			return true
		}
		if isSubstitution(part) {
			return true
		}
	}
	return false
}

func isSubstitution(part syntax.WordPart) bool {
	_, ok := part.(*syntax.CmdSubst)
	return ok
}

func checkDeclaringCommandAsName(p *pass, c command) {
	report := func(n syntax.Node, name string) {
		p.reportNode(2316, n, name+" is parsed as a variable name to "+c.name+", not as a modifier; use flags such as "+c.name+" -r instead.")
	}
	if c.decl == nil {
		for _, arg := range c.args {
			if lit, ok := arg.Parts[0].(*syntax.Lit); ok && len(arg.Parts) == 1 && declaringOrLet[lit.Value] {
				report(arg, lit.Value)
			}
		}
		return
	}
	for _, a := range c.decl.Args {
		if a.Naked && a.Name != nil && declaringOrLet[a.Name.Value] {
			report(a, a.Name.Value)
		}
	}
}

func checkSpacedEquals(p *pass, c command) {
	for _, arg := range c.args {
		lead, ok := arg.Parts[0].(*syntax.Lit)
		switch {
		case !ok:
		case strings.HasPrefix(lead.Value, "="):
			p.reportNode(2290, lead, "Remove the space before = ; with it, "+c.name+" sees a separate word instead of an assignment.")
		case strings.HasPrefix(lead.Value, "+="):
			p.reportNode(2290, lead, "Remove the space before += ; with it, "+c.name+" sees a separate word instead of an append.")
		}
	}
}

func checkLocalOutsideFunction(p *pass, c command) {
	if (p.dialect != dialect.Bash && p.dialect != dialect.Dash) || p.inFunction() {
		return
	}
	p.reportNode(2168, c.nameNode(), "local is only valid inside a function.")
}

func checkExportedExpansion(p *pass, c command) {
	for _, arg := range c.args {
		if pe, ok := soleBareExpansion(arg); ok {
			name := pe.Param.Value
			p.reportNode(2163, arg, "This exports the variable named by the value of "+name+"; to export "+name+" itself, drop the $. Write ${"+name+"?} if the indirection is intended.")
		}
	}
}

func checkReadInSameDeclaration(p *pass, c command) {
	if c.decl == nil || p.flow == nil {
		return
	}
	var assigned []*syntax.Assign
	read := func(name string) {
		for _, earlier := range assigned {
			if earlier.Name.Value == name {
				p.reportNode(2318, earlier, name+" is assigned in this same "+c.name+" and read again later in it; the read does not see the new value. Use a separate "+c.name+".")
			}
		}
	}
	for _, a := range c.decl.Args {
		if a.Index != nil {
			arithmeticOperandRead(a.Index, read)
			variableReads(a.Index, read)
		}
		if a.Value != nil {
			variableReads(a.Value, read)
		}
		if a.Array != nil {
			variableReads(a.Array, read)
		}
		if !a.Naked && a.Name != nil {
			assigned = append(assigned, a)
		}
	}
}

func arithmeticOperandRead(expr syntax.ArithmExpr, read func(name string)) {
	w, ok := expr.(*syntax.Word)
	if !ok || len(w.Parts) != 1 {
		return
	}
	if lit, ok := w.Parts[0].(*syntax.Lit); ok && parser.IsName(lit.Value) {
		read(lit.Value)
	}
}

func variableReads(root syntax.Node, read func(name string)) {
	syntax.Walk(root, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.ParamExp:
			if n.Param != nil {
				read(n.Param.Value)
			}
		case *syntax.ArithmExp:
			arithmeticOperandRead(n.X, read)
		case *syntax.BinaryArithm:
			arithmeticOperandRead(n.X, read)
			arithmeticOperandRead(n.Y, read)
		case *syntax.UnaryArithm:
			arithmeticOperandRead(n.X, read)
		case *syntax.ParenArithm:
			arithmeticOperandRead(n.X, read)
		}
		return true
	})
}
