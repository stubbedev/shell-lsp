package lint

import (
	"cmp"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

const uppercaseOptional = "check-unassigned-uppercase"

var variablesFamily = family{
	codes: map[Code]meta{
		2034: {severity: SeverityWarning},
		2153: {severity: SeverityInfo},
		2154: {severity: SeverityWarning},
	},
	rules: []rule{{node: collectVariables, finish: reportVariables}},
}

var presetVariables = setOf(
	"_", "BASH", "BASHOPTS", "BASHPID", "BASH_ALIASES", "BASH_ARGC", "BASH_ARGV", "BASH_ARGV0", "BASH_CMDS",
	"BASH_COMMAND", "BASH_COMPAT", "BASH_ENV", "BASH_EXECUTION_STRING", "BASH_LINENO", "BASH_LOADABLES_PATH",
	"BASH_MONOSECONDS", "BASH_REMATCH", "BASH_SOURCE", "BASH_SUBSHELL", "BASH_TRAPSIG", "BASH_VERSINFO",
	"BASH_VERSION", "BASH_XTRACEFD", "CDPATH", "CHILD_MAX", "COLUMNS", "COMP_CWORD", "COMP_KEY", "COMP_LINE",
	"COMP_POINT", "COMP_TYPE", "COMP_WORDBREAKS", "COMP_WORDS", "COMPREPLY", "COPROC", "DIRSTACK", "DISPLAY",
	"EMACS", "ENV", "EPOCHREALTIME", "EPOCHSECONDS", "EUID", "EXECIGNORE", "FCEDIT", "FIGNORE", "FUNCNAME",
	"FUNCNEST", "GLOBIGNORE", "GLOBSORT", "GROUPS", "HISTCMD", "HISTCONTROL", "HISTFILE", "HISTFILESIZE",
	"HISTIGNORE", "HISTSIZE", "HISTTIMEFORMAT", "HOME", "HOSTFILE", "HOSTNAME", "HOSTTYPE", "IFS", "IGNOREEOF",
	"INPUTRC", "INSIDE_EMACS", "LANG", "LANGUAGE", "LC_ALL", "LC_COLLATE", "LC_CTYPE", "LC_MESSAGES",
	"LC_MONETARY", "LC_NUMERIC", "LC_TIME", "LINENO", "LINES", "LOGNAME", "MACHTYPE", "MAIL", "MAILCHECK",
	"MAILPATH", "MAPFILE", "OLDPWD", "OPTARG", "OPTERR", "OPTIND", "OSTYPE", "PATH", "PIPESTATUS",
	"POSIXLY_CORRECT", "PPID", "PROMPT_COMMAND", "PROMPT_DIRTRIM", "PS0", "PS1", "PS2", "PS3", "PS4", "PWD",
	"RANDOM", "READLINE_ARGUMENT", "READLINE_LINE", "READLINE_MARK", "READLINE_POINT", "REPLY", "SECONDS",
	"SHELL", "SHELLOPTS", "SHLVL", "SRANDOM", "TERM", "TIMEFORMAT", "TMOUT", "TMPDIR", "TZ", "UID", "USER",
	"XAUTHORITY", "auto_resume", "histchars", "rest", "REST",
	"FLAGS_ARGV", "FLAGS_ERROR", "FLAGS_FALSE", "FLAGS_HELP", "FLAGS_PARENT", "FLAGS_RESERVED", "FLAGS_TRUE",
	"FLAGS_VERSION",
)

var promptVariables = setOf("PS1", "PS2", "PS3", "PS4", "PROMPT_COMMAND")

var batsVariables = []string{"status", "output", "lines", "stderr", "stderr_lines"}

var shflagsDefiners = setOf("DEFINE_string", "DEFINE_boolean", "DEFINE_integer", "DEFINE_float")

type variableSite struct {
	name string
	span parser.Span
}

type variableRead struct {
	variableSite
	exit  int
	array string
	guard bool
}

type variableUsage struct {
	sites    []variableSite
	reads    []variableRead
	assigned map[string]bool
	used     map[string]bool
	loops    map[*syntax.WordIter]int
	bats     bool
}

func (u *variableUsage) assign(name string) {
	if u.assigned == nil {
		u.assigned = map[string]bool{}
	}
	u.assigned[name] = true
}

func (u *variableUsage) use(name string) {
	if u.used == nil {
		u.used = map[string]bool{}
	}
	u.used[name] = true
}

func collectVariables(p *pass, n syntax.Node) {
	switch n := n.(type) {
	case *syntax.ParamExp:
		p.collectExpansion(n)
	case *syntax.Word:
		p.collectWord(n)
	case *syntax.Assign:
		p.collectAssign(n)
	case *syntax.ArrayElem:
		if owner, ok := p.ancestor(1).(*syntax.Assign); ok && owner.Name != nil {
			p.subscript(n.Index, owner.Name.Value, true)
		}
	case *syntax.ForClause:
		if iter, ok := n.Loop.(*syntax.WordIter); ok {
			if p.variables.loops == nil {
				p.variables.loops = map[*syntax.WordIter]int{}
			}
			p.variables.loops[iter] = parser.Offset(n.Pos())
		}
	case *syntax.LetClause:
		for _, expr := range n.Exprs {
			p.collectQuotedLet(expr)
		}
	case *syntax.DeclClause:
		p.collectPrinted(n)
	case *syntax.CallExpr:
		p.collectCall(n)
	case *syntax.TestClause, *syntax.ParenTest, *syntax.UnaryTest, *syntax.BinaryTest:
		doubleBracketOperands(n, p.collectTestOperand)
	case *syntax.TestDecl:
		p.variables.bats = true
	}
}

func (p *pass) collectExpansion(pe *syntax.ParamExp) {
	if pe.Param == nil {
		return
	}
	name := pe.Param.Value
	if isDefaultAssignment(pe) {
		p.variables.assign(name)
	}
	p.subscript(pe.Index, name, pe.Short)
	if pe.Slice != nil {
		p.subscript(pe.Slice.Offset, "", false)
		p.subscript(pe.Slice.Length, "", false)
	}
	if pe.Names != 0 || !parser.IsName(name) || p.shielded(name) {
		return
	}
	if span, ok := p.span(pe); ok {
		p.record(variableRead{name: name, span: span, guard: guarded(pe)})
	}
}

func (p *pass) collectWord(w *syntax.Word) {
	if name, ok := parser.ArithmeticRead(p.parent(), w); ok && !p.shielded(name) {
		p.read(name, w, p.subscriptOwner())
	}
}

func (p *pass) subscript(expr syntax.ArithmExpr, array string, dereferenced bool) {
	if expr == nil {
		return
	}
	name, ok := parser.ArithmeticName(expr)
	if !ok {
		return
	}
	p.variables.use(name)
	if dereferenced && !p.shielded(name) {
		p.read(name, expr, array)
	}
}

func (p *pass) read(name string, n syntax.Node, array string) {
	if span, ok := p.span(n); ok {
		p.readAt(name, span, array)
	}
}

func (p *pass) readAt(name string, span parser.Span, array string) {
	p.record(variableRead{name: name, span: span, array: array})
}

func (p *pass) record(r variableRead) {
	r.exit = r.span.End
	if start, ok := p.hoistedRedirect(); ok {
		r.exit = start
	}
	p.variables.reads = append(p.variables.reads, r)
}

func (p *pass) subscriptOwner() string {
	for i, n := range slices.Backward(p.stack) {
		switch n := n.(type) {
		case *syntax.BinaryArithm, *syntax.UnaryArithm, *syntax.ParenArithm:
			continue
		case *syntax.ArrayElem:
			if owner, ok := p.ancestor(len(p.stack) - i + 1).(*syntax.Assign); ok && owner.Name != nil {
				return owner.Name.Value
			}
		case *syntax.Assign:
			if n.Name != nil && n.Index != nil {
				return n.Name.Value
			}
		case *syntax.ParamExp:
			if n.Param != nil && n.Index != nil {
				return n.Param.Value
			}
		}
		return ""
	}
	return ""
}

func (p *pass) hoistedRedirect() (int, bool) {
	for i := 0; i+1 < len(p.stack); i++ {
		stmt, ok := p.stack[i].(*syntax.Stmt)
		if !ok {
			continue
		}
		if _, ok := p.stack[i+1].(*syntax.Redirect); ok {
			return parser.Offset(stmt.Pos()), true
		}
	}
	return 0, false
}

func (p *pass) shielded(name string) bool {
	for _, n := range slices.Backward(p.stack) {
		pe, ok := n.(*syntax.ParamExp)
		if !ok || pe.Short || pe.Param == nil {
			continue
		}
		if pe.Param.Value != name || alternativeOnName(pe) {
			return true
		}
	}
	return false
}

func alternativeOnName(pe *syntax.ParamExp) bool {
	return isAlternativeExpansion(pe) && pe.Index == nil
}

func guarded(pe *syntax.ParamExp) bool {
	if pe.Exp == nil {
		return false
	}
	switch pe.Exp.Op {
	case syntax.DefaultUnset, syntax.DefaultUnsetOrNull, syntax.ErrorUnset, syntax.ErrorUnsetOrNull:
		return true
	}
	return alternativeOnName(pe)
}

const nameChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_"

func leadingName(s string) string {
	end := len(s) - len(strings.TrimLeft(s, nameChars))
	if !parser.IsName(s[:end]) {
		return ""
	}
	return s[:end]
}

func (p *pass) collectAssign(a *syntax.Assign) {
	if a.Name == nil {
		return
	}
	name := a.Name.Value
	if a.Append {
		p.variables.use(name)
	}
	p.subscript(a.Index, name, true)
	if promptVariables[name] && a.Value != nil {
		if span, ok := p.span(a); ok {
			p.scanWord(a.Value, span)
		}
	}
}

func (p *pass) scanWord(w *syntax.Word, at parser.Span) {
	for _, part := range w.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			p.scanText(part.Value, at)
		case *syntax.SglQuoted:
			p.scanText(part.Value, at)
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				if lit, ok := inner.(*syntax.Lit); ok {
					p.scanText(lit.Value, at)
				}
			}
		}
	}
}

func (p *pass) scanText(text string, at parser.Span) {
	for {
		i := strings.IndexByte(text, '$')
		if i < 0 {
			return
		}
		text = strings.TrimPrefix(text[i+1:], "{")
		if name := leadingName(text); name != "" {
			p.variables.use(name)
			p.readAt(name, at, "")
		}
	}
}

func (p *pass) collectQuotedLet(expr syntax.ArithmExpr) {
	w, ok := expr.(*syntax.Word)
	if !ok || len(w.Parts) != 1 {
		return
	}
	var start syntax.Pos
	switch part := w.Parts[0].(type) {
	case *syntax.SglQuoted:
		start = part.Left
	case *syntax.DblQuoted:
		start = part.Left
	default:
		return
	}
	text, ok := parser.Literal(w)
	if !ok {
		return
	}
	parsed, err := syntax.NewParser().Arithmetic(strings.NewReader(text))
	if err != nil {
		return
	}
	p.collectArithmetic(parsed, parser.Offset(start)+1)
}

func (p *pass) collectArithmetic(expr syntax.ArithmExpr, base int) {
	var stack []syntax.Node
	at := func(pos syntax.Pos, length int) parser.Span {
		start := base + parser.Offset(pos)
		return parser.Span{Start: start, End: start + length}
	}
	syntax.Walk(expr, func(n syntax.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		var parent syntax.Node
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		switch n := n.(type) {
		case *syntax.Word:
			if name, ok := parser.ArithmeticRead(parent, n); ok {
				p.variables.use(name)
				p.readAt(name, at(n.Pos(), len(name)), "")
			}
		case *syntax.ParamExp:
			if n.Param != nil && parser.IsName(n.Param.Value) {
				p.variables.use(n.Param.Value)
				p.readAt(n.Param.Value, at(n.Pos(), len(n.Param.Value)), "")
			}
		case *syntax.BinaryArithm:
			if name, ok := parser.ArithmeticName(n.X); ok && parser.IsArithmeticAssignment(n.Op) {
				p.variables.sites = append(p.variables.sites, variableSite{name: name, span: at(n.OpPos, len(n.Op.String()))})
			}
		case *syntax.UnaryArithm:
			if name, ok := parser.ArithmeticName(n.X); ok && (n.Op == syntax.Inc || n.Op == syntax.Dec) {
				p.variables.sites = append(p.variables.sites, variableSite{name: name, span: at(n.OpPos, len(n.Op.String()))})
			}
		}
		stack = append(stack, n)
		return true
	})
}

func (p *pass) collectPrinted(d *syntax.DeclClause) {
	if d.Variant.Value != "declare" && d.Variant.Value != "typeset" {
		return
	}
	printing := false
	for _, arg := range d.Args {
		if arg.Name == nil {
			flags, _ := parser.Literal(arg.Value)
			if strings.HasPrefix(flags, "-") {
				printing = printing || strings.Contains(flags, "p")
			}
			continue
		}
		if printing {
			p.variables.use(arg.Name.Value)
			p.read(arg.Name.Value, arg, "")
		}
	}
}

func (p *pass) collectCall(call *syntax.CallExpr) {
	if words, ok := p.bracketTail(call); ok {
		p.collectBracketTest(words)
		return
	}
	c, ok := commandOf(call)
	if !ok {
		return
	}
	switch {
	case c.name == "@test":
		p.variables.bats = true
	case c.is("trap") && len(c.args) > 0:
		if span, ok := p.span(c.args[0]); ok {
			p.scanWord(c.args[0], span)
		}
	case c.is("alias"):
		for _, arg := range c.args {
			if span, ok := p.span(arg); ok {
				p.scanAlias(arg, span)
			}
		}
	case shflagsDefiners[c.name] && len(c.args) > 0:
		if flag, ok := parser.Literal(c.args[0]); ok && parser.IsName(flag) {
			if span, ok := p.span(c.args[0]); ok {
				p.variables.sites = append(p.variables.sites, variableSite{name: "FLAGS_" + flag, span: span})
			}
		}
	}
	if words, _, bracket := p.bracketOperandWords(call); bracket {
		p.collectBracketTest(words)
	}
}

func (p *pass) scanAlias(w *syntax.Word, at parser.Span) {
	for i, part := range w.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			continue
		}
		if _, body, found := strings.Cut(lit.Value, "="); found {
			p.scanText(body, at)
			p.scanWord(&syntax.Word{Parts: w.Parts[i+1:]}, at)
			return
		}
	}
}

func (p *pass) collectBracketTest(words []*syntax.Word) {
	t, ok := parseBracketTest(words)
	if !ok {
		for _, w := range words {
			p.checkedOperand(w)
		}
		return
	}
	for _, o := range t.operands {
		p.collectTestOperand(o)
	}
}

func (p *pass) collectTestOperand(o testOperand) {
	switch {
	case o.shape == shapeNullary, o.shape == shapeUnary && (o.op == "-n" || o.op == "-z"):
		p.checkedOperand(o.word)
	case o.shape == shapeUnary && o.op == "-v":
		if name := leadingName(leadingLiteral(o.word)); name != "" {
			p.variables.use(name)
			p.variables.assign(name)
		}
	case o.shape == shapeUnary && o.op == "-R":
		if name := leadingName(leadingLiteral(o.word)); name != "" {
			p.variables.use(name)
			p.read(name, o.word, "")
		}
	case o.double && o.binary() && arithmeticTestOperators[o.op]:
		p.comparedName(o.word)
	}
}

func leadingLiteral(w *syntax.Word) string {
	if len(w.Parts) == 0 {
		return ""
	}
	part := w.Parts[0]
	if dq, ok := part.(*syntax.DblQuoted); ok && len(dq.Parts) > 0 {
		part = dq.Parts[0]
	}
	switch part := part.(type) {
	case *syntax.Lit:
		return part.Value
	case *syntax.SglQuoted:
		return part.Value
	}
	return ""
}

func (p *pass) checkedOperand(w *syntax.Word) {
	for _, part := range w.Parts {
		if dq, ok := part.(*syntax.DblQuoted); ok {
			for _, inner := range dq.Parts {
				p.checkedPart(inner)
			}
			continue
		}
		p.checkedPart(part)
	}
}

func (p *pass) checkedPart(part syntax.WordPart) {
	if pe, ok := part.(*syntax.ParamExp); ok && pe.Param != nil && pe.Names == 0 {
		p.variables.assign(pe.Param.Value)
	}
}

func (p *pass) comparedName(w *syntax.Word) {
	lit, ok := parser.Literal(w)
	if !ok {
		return
	}
	if name := leadingName(lit); name != "" {
		p.variables.use(name)
		if !p.shielded(name) {
			p.read(name, w, "")
		}
	}
}

type variableFacts struct {
	assigned map[string]bool
	used     map[string]bool
	assoc    map[string]bool
	last     map[string]parser.Span
}

func (p *pass) variableFacts() variableFacts {
	u := &p.variables
	f := variableFacts{assigned: map[string]bool{}, used: map[string]bool{}, assoc: p.associativeArrays(), last: map[string]parser.Span{}}
	site := func(name string, span parser.Span) {
		f.assigned[name] = true
		if prev, ok := f.last[name]; !ok || span.Start >= prev.Start {
			f.last[name] = span
		}
	}
	for i := range p.tree.Definitions {
		def := &p.tree.Definitions[i]
		if def.Kind == parser.DefCommandEnv {
			continue
		}
		site(def.Name, p.definitionSite(def))
		if def.Attrs.Has(parser.AttrExport) {
			f.used[def.Name] = true
		}
	}
	for _, s := range u.sites {
		site(s.name, s.span)
	}
	for _, r := range p.tree.References {
		f.used[r.Name] = true
	}
	for name := range u.assigned {
		f.assigned[name] = true
	}
	for name := range u.used {
		f.used[name] = true
	}
	if u.bats {
		for _, name := range batsVariables {
			f.assigned[name], f.used[name] = true, true
		}
	}
	for _, imp := range p.imports {
		for _, def := range imp.Tree.Definitions {
			if def.Kind != parser.DefCommandEnv {
				f.assigned[def.Name] = true
			}
		}
		for _, r := range imp.Tree.References {
			f.used[r.Name] = true
		}
	}
	return f
}

func (p *pass) associativeArrays() map[string]bool {
	if p.associative == nil {
		p.associative = map[string]bool{}
		for _, def := range p.tree.Definitions {
			if def.Attrs.Has(parser.AttrAssoc) {
				p.associative[def.Name] = true
			}
		}
	}
	return p.associative
}

func (p *pass) definitionSite(def *parser.Definition) parser.Span {
	switch node := def.Node.(type) {
	case *syntax.WordIter:
		if start, ok := p.variables.loops[node]; ok {
			return parser.Span{Start: start, End: start}
		}
	case *syntax.BinaryArithm:
		start := parser.Offset(node.OpPos)
		return parser.Span{Start: start, End: start + len(node.Op.String())}
	}
	return def.Span
}

func reportVariables(p *pass) {
	if len(p.tree.Errors) > 0 {
		return
	}
	f := p.variableFacts()
	p.reportUnused(f)
	p.reportUnassigned(f)
}

func (p *pass) reportUnused(f variableFacts) {
	names := make([]string, 0, len(f.last))
	for name := range f.last {
		if !f.used[name] && !strings.HasPrefix(name, "_") && !presetVariables[name] {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		p.report(2034, f.last[name], name+" is assigned but never read; remove it, or export it if another program needs it.")
	}
}

func (p *pass) reportUnassigned(f variableFacts) {
	reads := p.variables.reads
	slices.SortStableFunc(reads, func(a, b variableRead) int {
		return cmp.Or(cmp.Compare(a.exit, b.exit), cmp.Compare(a.span.End, b.span.End), cmp.Compare(b.span.Start, a.span.Start))
	})
	seen := map[string]bool{}
	for _, r := range reads {
		if seen[r.name] || f.assigned[r.name] || presetVariables[r.name] || (r.array != "" && f.assoc[r.array]) {
			continue
		}
		seen[r.name] = true
		if r.guard {
			continue
		}
		p.reportUnassignedRead(r.variableSite, f)
	}
}

func (p *pass) reportUnassignedRead(r variableSite, f variableFacts) {
	similar, found := closestName(r.name, f.assigned)
	if strings.ToUpper(r.name) == r.name && !p.settings.enabled(uppercaseOptional) {
		if found {
			p.report(2153, r.span, r.name+" is never assigned here; did you mean "+similar+"?")
		}
		return
	}
	message := r.name + " is read but never assigned in this script."
	switch {
	case posixUtilities[r.name]:
		message += " To run the " + r.name + " command, use $(" + r.name + ")."
	case found:
		message += " Did you mean " + similar + "?"
	}
	p.report(2154, r.span, message)
}

func closestName(name string, candidates map[string]bool) (string, bool) {
	best, bestDistance := "", 0
	for candidate := range candidates {
		distance, ok := nameDistance(name, candidate)
		if !ok {
			continue
		}
		if best == "" || distance < bestDistance || (distance == bestDistance && candidate < best) {
			best, bestDistance = candidate, distance
		}
	}
	return best, best != ""
}

func nameDistance(name, candidate string) (int, bool) {
	distance := 1
	if !strings.EqualFold(name, candidate) {
		distance = levenshtein(name, candidate)
	}
	switch {
	case len(candidate) > 7 && distance <= 2, len(candidate) > 3 && distance <= 1:
		return distance, distance > 0
	}
	return 0, false
}

func levenshtein(a, b string) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(a); i++ {
		diagonal := row[0]
		row[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			diagonal, row[j] = row[j], min(row[j]+1, row[j-1]+1, diagonal+cost)
		}
	}
	return row[len(b)]
}
