package lint

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var builtinFamily = family{
	codes: map[Code]meta{
		2064: {severity: SeverityWarning},
		2121: {severity: SeverityWarning},
		2139: {severity: SeverityWarning},
		2142: {severity: SeverityError},
		2151: {severity: SeverityError},
		2152: {severity: SeverityError},
		2172: {severity: SeverityWarning},
		2173: {severity: SeverityError},
		2184: {severity: SeverityWarning},
		2229: {severity: SeverityWarning},
		2240: {severity: SeverityWarning},
		2241: {severity: SeverityError},
		2242: {severity: SeverityError},
		2293: {severity: SeverityStyle},
		2294: {severity: SeverityWarning},
		2313: {severity: SeverityWarning},
	},
	commands: []commandRule{
		{match: matchExact, names: []string{"trap"}, check: checkTrapHandler},
		{match: matchExact, names: []string{"trap"}, check: checkTrapSignalNumbers},
		{match: matchExact, names: []string{"return"}, check: checkStatusArgument(2151, 2152)},
		{match: matchExact, names: []string{"exit"}, check: checkStatusArgument(2241, 2242)},
		{match: matchExact, names: []string{"set"}, check: checkSetAssignment},
		{match: matchExact, names: []string{"alias"}, check: checkAliasBody},
		{match: matchExact, names: []string{"eval"}, check: checkEvalArrays},
		{match: matchExact, names: []string{"unset"}, check: checkUnsetIndex},
		{match: matchExact, names: []string{"read"}, check: checkReadTargets},
		{match: matchExact, names: []string{"."}, check: checkDotArguments},
	},
}

func checkTrapHandler(p *pass, c command) {
	if len(c.args) == 0 || len(c.args[0].Parts) != 1 {
		return
	}
	dq, ok := c.args[0].Parts[0].(*syntax.DblQuoted)
	if !ok {
		return
	}
	for _, part := range dq.Parts {
		switch part.(type) {
		case *syntax.CmdSubst, *syntax.ParamExp, *syntax.ArithmExp:
			p.reportNode(2064, part, "This expands when the trap is set, not when it fires; single-quote the handler to expand it later.")
		}
	}
}

var (
	untrappableSignals = setOf("kill", "sigkill", "stop", "sigstop", "9")
	portableSignals    = setOf("0", "1", "2", "3", "6", "9", "14", "15")
)

func checkTrapSignalNumbers(p *pass, c command) {
	if len(c.args) == 0 {
		return
	}
	if lead, ok := c.args[0].Parts[0].(*syntax.Lit); ok && strings.HasPrefix(lead.Value, "-") {
		return
	}
	for _, arg := range c.args[1:] {
		signal, ok := parser.Literal(arg)
		switch {
		case !ok:
		case untrappableSignals[strings.ToLower(signal)]:
			p.reportNode(2173, arg, "SIGKILL and SIGSTOP cannot be trapped; this handler never runs for "+signal+".")
		case parser.IsDigits(signal) && !portableSignals[signal]:
			p.reportNode(2172, arg, "Signal number "+signal+" differs between systems; use its name instead.")
		}
	}
}

const maxStatus = 255

func checkStatusArgument(tooMany, outOfRange Code) func(p *pass, c command) {
	return func(p *pass, c command) {
		switch len(c.args) {
		case 0:
			return
		case 1:
		default:
			p.reportNode(tooMany, c.args[0], c.name+" takes at most one status; use echo or printf to output other values.")
			return
		}
		if !validStatus(c.args[0]) {
			p.reportNode(outOfRange, c.args[0], c.name+" only takes an integer status from 0 to 255; use echo or printf to output other values.")
		}
	}
}

func validStatus(w *syntax.Word) bool {
	status, _ := parser.Text(w, "0")
	if !parser.IsDigits(status) || len(status) > 5 {
		return false
	}
	n, err := strconv.Atoi(status)
	return err == nil && n <= maxStatus
}

func checkSetAssignment(p *pass, c command) {
	if len(c.args) == 0 {
		return
	}
	var sb strings.Builder
	for _, part := range c.args[0].Parts {
		if lit, ok := part.(*syntax.Lit); ok {
			sb.WriteString(lit.Value)
			continue
		}
		sb.WriteByte('*')
	}
	first := sb.String()
	if (len(c.args) > 1 && parser.IsName(first)) || strings.Contains(first, "=") {
		p.reportNode(2121, c.args[0], "set does not assign variables; use "+strings.TrimSuffix(first, "=")+"=value without set.")
	}
}

var positionalInAlias = regexp.MustCompile(`\$\{?[0-9*@]`)

func checkAliasBody(p *pass, c command) {
	for _, arg := range c.args {
		text, _ := parser.Text(arg, "_")
		if !strings.Contains(text, "=") {
			continue
		}
		if part := firstExpandingPart(arg); part != nil {
			p.reportNode(2139, part, "This expands when the alias is defined, not when it is used; single-quote the alias body to expand it later.")
		}
		if positionalInAlias.MatchString(text) {
			p.reportNode(2142, arg, "Aliases cannot take positional parameters; define a function instead.")
		}
	}
}

func firstExpandingPart(w *syntax.Word) syntax.Node {
	for _, part := range w.Parts {
		if dq, ok := part.(*syntax.DblQuoted); ok {
			if i := slices.IndexFunc(dq.Parts, isExpansion); i >= 0 {
				return dq.Parts[i]
			}
			continue
		}
		if isExpandingPart(part) {
			return part
		}
	}
	return nil
}

func isExpandingPart(part syntax.WordPart) bool {
	if lit, ok := part.(*syntax.Lit); ok {
		w := &syntax.Word{Parts: []syntax.WordPart{lit}}
		return isGlob(w) || hasBraceExpansion(w)
	}
	return isExpansion(part)
}

func isExpansion(part syntax.WordPart) bool {
	switch part.(type) {
	case *syntax.Lit, *syntax.SglQuoted:
		return false
	}
	return true
}

func checkEvalArrays(p *pass, c command) {
	for _, arg := range c.args {
		for _, part := range arg.Parts {
			if dq, ok := part.(*syntax.DblQuoted); ok {
				for _, inner := range dq.Parts {
					checkEvalArray(p, inner)
				}
				continue
			}
			checkEvalArray(p, part)
		}
	}
}

func checkEvalArray(p *pass, part syntax.WordPart) {
	pe, ok := part.(*syntax.ParamExp)
	if !ok || kindOf(pe) != expandsElements {
		return
	}
	if pe.Exp != nil && pe.Exp.Op == syntax.OtherParamOps && strings.Contains(pe.Exp.Word.Lit(), "Q") {
		p.reportNode(2293, pe, "eval joins its arguments with spaces anyway; use * instead of @ with the @Q operator to make that explicit.")
		return
	}
	p.reportNode(2294, pe, "eval joins the array elements into one string and re-splits it, losing their boundaries; run the command directly as \"${array[@]}\" instead.")
}

func checkUnsetIndex(p *pass, c command) {
	for _, arg := range c.args {
		if isGlob(arg) {
			p.reportNode(2184, arg, "Quote this argument to unset so the shell does not expand the index brackets as a glob.")
		}
	}
}

var readOptions = optionGrammar{short: "sreu:n:N:i:p:a:t:d:"}

func checkReadTargets(p *pass, c command) {
	for _, arg := range c.args {
		if hasBracketGlob(arg) {
			p.reportNode(2313, arg, "Quote the array index in this read target so the shell does not expand the brackets as a glob.")
		}
	}
	options, ok := readOptions.parse(c.args)
	if !ok {
		return
	}
	targets := options.operands
	for _, opt := range options.options {
		if opt.name == "a" && opt.value != opt.word {
			targets = append(targets, opt.value)
		}
	}
	for _, target := range targets {
		if pe, ok := soleBareExpansion(target); ok && parser.IsName(pe.Param.Value) {
			name := pe.Param.Value
			p.reportNode(2229, target, "This reads into the variable named by the value of "+name+"; to read into "+name+" itself, drop the $. Write ${"+name+"?} if the indirection is intended.")
		}
	}
}

func checkDotArguments(p *pass, c command) {
	if !posixOrDash.Has(p.dialect) || len(c.args) < 2 {
		return
	}
	p.reportNode(2240, c.args[1], "Arguments after the script name of . are not passed to it in sh; set them with set -- first.")
}
