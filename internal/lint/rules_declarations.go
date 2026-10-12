package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/textline"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const codeInvalidDeclaredName = nativeBase + 2003

var declarationFamily = family{
	codes: map[Code]meta{
		codeInvalidDeclaredName: {severity: SeverityError},
		2155:                    {severity: SeverityWarning, summary: "Declaring and assigning in one command masks the status of the command substitution."},
		2163:                    {severity: SeverityWarning, summary: "export of $name exports the variable named by its value, not the variable itself."},
		2168:                    {severity: SeverityError, summary: "local is used outside a function."},
		2290:                    {severity: SeverityError, summary: "A space before = or += in a declaration turns the assignment into a separate word."},
		2316:                    {severity: SeverityError, summary: "A word given to a declaration command is parsed as a variable name, not as a modifier."},
		2318:                    {severity: SeverityWarning, summary: "A variable assigned in a declaration is read again in the same declaration before it takes effect."},
	},
	commands: []commandRule{
		{match: matchExact, names: declaringCommands, check: checkMaskedStatus},
		{match: matchExact, names: declaringCommands, check: checkDeclaringCommandAsName},
		{match: matchExact, names: declaringCommands, check: checkReadInSameDeclaration},
		{match: matchExact, names: append([]string{"alias"}, declaringCommands...), check: checkSpacedEquals},
		{match: matchExact, names: []string{"local"}, check: checkLocalOutsideFunction},
		{match: matchExact, names: []string{"export"}, check: checkExportedExpansion},
		{match: matchExact, names: declaringCommands, check: checkInvalidDeclaredName},
	},
}

func checkInvalidDeclaredName(p *pass, c command) {
	for _, arg := range c.args {
		lead, ok := arg.Parts[0].(*syntax.Lit)
		if !ok {
			continue
		}
		name, _, assigns := strings.Cut(lead.Value, "=")
		name = strings.TrimSuffix(name, "+")
		if !assigns || name == "" || strings.HasPrefix(name, "-") || strings.ContainsRune(name, '\\') || p.declarableName(name) {
			continue
		}
		p.reportNode(codeInvalidDeclaredName, lead, name+" is not a valid variable name, so "+c.name+" fails with \"not a valid identifier\" instead of assigning it.")
	}
}

func (p *pass) declarableName(name string) bool {
	if p.dialect.Features().Arrays {
		if open := strings.IndexByte(name, '['); open > 0 && strings.HasSuffix(name, "]") {
			name = name[:open]
		}
	}
	if p.dialect != dialect.Ksh {
		return syntax.ValidName(name)
	}
	for segment := range strings.SplitSeq(strings.TrimPrefix(name, "."), ".") {
		if !syntax.ValidName(segment) {
			return false
		}
	}
	return true
}

var (
	declaringCommands  = []string{"local", "declare", "export", "readonly", "typeset"}
	declaringOrLet     = setOf("local", "declare", "export", "readonly", "typeset", "let")
	scopedDeclarations = setOf("local", "declare", "typeset")
	localCheckedShells = dialect.SetOf(dialect.Bash, dialect.Dash, dialect.Busybox)
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
	flags := c.options()
	local := scopedDeclarations[c.name] && !flags.Has("-g") && p.inFunction()
	if local && (c.name == "readonly" || flags.Has("-r")) {
		return
	}
	for _, a := range c.decl.Args {
		if !a.Naked && a.Value != nil && substitutes(a.Value) {
			p.reportNode(2155, a, "Declare and assign "+a.Name.Value+" separately; "+c.name+" returns its own status and masks the status of the command substitution.", p.separateDeclarationFix(c, flags, a)...)
		}
	}
}

func (p *pass) separateDeclarationFix(c command, flags options, a *syntax.Assign) []Fix {
	assignments := 0
	for _, arg := range c.decl.Args {
		if !arg.Naked {
			assignments++
		}
	}
	stmt, ok := p.parent().(*syntax.Stmt)
	if !ok || stmt.Cmd != c.decl || assignments != 1 || a.Append || a.Index != nil || a.Array != nil || stmt.Negated || stmt.Background || len(stmt.Redirs) > 0 {
		return nil
	}
	if _, chained := p.ancestor(1).(*syntax.BinaryCmd); chained {
		return nil
	}
	declSpan, ok := p.span(c.decl)
	assignSpan, assignOK := p.span(a)
	if !ok || !assignOK || !onlyBlanksBefore(p.text, declSpan.Start) {
		return nil
	}
	indent := p.text[textline.Start(p.text, declSpan.Start):declSpan.Start]
	assignment := p.spanText(assignSpan)
	name := a.Name.Value
	switch c.name {
	case "export", "readonly":
		if len(c.decl.Args) != 1 {
			return nil
		}
		return []Fix{reviewFix("Assign "+name+" before "+c.name+" it", textedit.Edit{Start: declSpan.Start, End: declSpan.End, NewText: assignment + "\n" + indent + c.name + " " + name})}
	case "local", "declare", "typeset":
		if flags.Has("-r", "-n") {
			return nil
		}
		return []Fix{reviewFix("Declare "+name+" and assign it separately", textedit.Edit{Start: assignSpan.Start, End: assignSpan.End, NewText: name + "\n" + indent + assignment})}
	}
	return nil
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
	if !localCheckedShells.Has(p.dialect) || p.inFunction() {
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
	if c.decl == nil || !p.settings.extended || len(c.decl.Args) < 2 {
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
	parser.Walk(root, func(n syntax.Node) bool {
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
		case *syntax.FuncArithm:
			arithmeticOperandRead(n.X, read)
		case *syntax.FlagsArithm:
			if n.Brackets {
				arithmeticOperandRead(n.X, read)
			}
		}
		return true
	})
}
