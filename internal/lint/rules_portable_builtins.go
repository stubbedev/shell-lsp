package lint

import (
	"path"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var portableBuiltinsFamily = family{
	codes: map[Code]meta{
		3036: {severity: SeverityError},
		3037: {severity: SeverityWarning},
		3038: {severity: SeverityWarning},
		3039: {severity: SeverityWarning},
		3040: {severity: SeverityWarning},
		3041: {severity: SeverityWarning},
		3042: {severity: SeverityWarning},
		3043: {severity: SeverityWarning},
		3044: {severity: SeverityWarning},
		3045: {severity: SeverityWarning},
		3046: {severity: SeverityWarning},
		3047: {severity: SeverityWarning},
		3048: {severity: SeverityWarning},
		3049: {severity: SeverityWarning},
		3050: {severity: SeverityWarning},
		3051: {severity: SeverityWarning},
		3061: {severity: SeverityWarning},
	},
	rules: []rule{{on: nodes(&syntax.CallExpr{}, &syntax.DeclClause{}, &syntax.LetClause{}), node: checkPortableBuiltin}},
}

type allowedFlags struct {
	posix, dash, busybox string
}

func (a allowedFlags) in(d dialect.Dialect) string {
	switch d {
	case dialect.Dash:
		return a.dash
	case dialect.Busybox:
		return a.busybox
	}
	return a.posix
}

var (
	bashOnlyBuiltins = setOf("caller", "builtin", "complete", "compgen", "declare", "dirs", "disown", "enable", "mapfile", "readarray", "pushd", "popd", "shopt", "suspend", "typeset")
	portableFlags    = map[string]allowedFlags{
		"cd":       {posix: "LP", dash: "LP", busybox: "LP"},
		"export":   {posix: "p", dash: "p", busybox: "p"},
		"hash":     {posix: "r", dash: "rv", busybox: "rv"},
		"jobs":     {posix: "lp", dash: "lp", busybox: "lp"},
		"printf":   {},
		"read":     {posix: "r", dash: "rp", busybox: "rp"},
		"readonly": {posix: "p", dash: "p", busybox: "p"},
		"trap":     {},
		"type":     {busybox: "p"},
		"ulimit":   {posix: "HSacdfnstv", dash: "HSacdflmnprstvw", busybox: "HSacdflmnprstvw"},
		"umask":    {posix: "S", dash: "S", busybox: "S"},
		"unset":    {posix: "fv", dash: "fv", busybox: "fv"},
		"wait":     {},
	}
	posixSetOptions  = setOf("allexport", "errexit", "ignoreeof", "monitor", "noclobber", "noexec", "noglob", "nolog", "notify", "nounset", "verbose", "vi", "xtrace")
	bashTrapSignals  = setOf("ERR", "DEBUG", "RETURN")
	posixSetLetters  = "abCefhmnuvxo"
	echoFlagLetters  = "eEsn"
	busyboxEchoFlags = "Es"
)

func checkPortableBuiltin(p *pass, n syntax.Node) {
	if !strictShells.Has(p.dialect) {
		return
	}
	switch n := n.(type) {
	case *syntax.CallExpr:
		checkPortableCall(p, n)
	case *syntax.DeclClause:
		checkPortableDeclaration(p, n)
	case *syntax.LetClause:
		p.nonPortableNode(portabilityIssue{code: 3039, shells: strictShells, feature: "let", alternative: "$((...))"}, n)
	}
}

func checkPortableDeclaration(p *pass, decl *syntax.DeclClause) {
	name := decl.Variant.Value
	switch {
	case name == "local":
		p.nonPortableNode(portabilityIssue{code: 3043, shells: posixOnly, feature: "local"}, decl)
	case bashOnlyBuiltins[name]:
		p.nonPortableNode(portabilityIssue{code: 3044, shells: strictShells, feature: "The builtin " + name}, decl)
	}
	var flags []*syntax.Word
	for _, a := range decl.Args {
		if !a.Naked || a.Name != nil || a.Value == nil {
			break
		}
		flags = append(flags, a.Value)
	}
	checkPortableFlags(p, name, flags)
}

func checkPortableCall(p *pass, call *syntax.CallExpr) {
	if len(call.Args) == 0 {
		return
	}
	literal, ok := parser.Literal(call.Args[0])
	if !ok {
		return
	}
	if literal == "exec" && checkExecFlags(p, call) {
		return
	}
	checkResolvedBuiltin(p, call)
	name, args := path.Base(literal), call.Args[1:]
	switch name {
	case "echo":
		checkEchoFlags(p, call.Args[0], args)
	case "set":
		checkSetOptions(p, args)
	case "trap":
		checkTrapSignals(p, args)
	case "printf":
		checkPrintfFormat(p, args)
	case "read":
		checkReadOperands(p, call)
	}
	checkPortableFlags(p, name, args)
}

func checkExecFlags(p *pass, call *syntax.CallExpr) bool {
	if len(call.Args) < 2 {
		return false
	}
	flag, ok := parser.Literal(call.Args[1])
	if !ok || !strings.HasPrefix(flag, "-") {
		return false
	}
	p.nonPortableNode(portabilityIssue{code: 3038, shells: strictShells, feature: "exec " + flag, alternative: "exec without options (env -i for a clean environment)"}, call.Args[1])
	return true
}

func checkResolvedBuiltin(p *pass, call *syntax.CallExpr) {
	word := p.effectiveCommandWord(call)
	if word == nil {
		word = call.Args[0]
	}
	literal, ok := parser.Literal(word)
	if !ok {
		return
	}
	switch name := path.Base(literal); {
	case name == "let":
		p.nonPortableNode(portabilityIssue{code: 3039, shells: strictShells, feature: "let", alternative: "$((...))"}, call)
	case name == "local":
		p.nonPortableNode(portabilityIssue{code: 3043, shells: posixOnly, feature: "local"}, call)
	case name == "source":
		issue := portabilityIssue{code: 3046, shells: posixOrDash, feature: "source", alternative: "."}
		var fixes []Fix
		if span, ok := p.span(word); ok && word.Lit() == name {
			fixes = append(fixes, safeFix("Replace source with .", textedit.Edit{Start: span.Start, End: span.End, NewText: "."}))
		}
		p.nonPortableNode(issue, call, fixes...)
		if p.followsSource(call, word) {
			issue.code = 3051
			p.nonPortableNode(issue, call)
		}
	case bashOnlyBuiltins[name]:
		p.nonPortableNode(portabilityIssue{code: 3044, shells: strictShells, feature: "The builtin " + name}, call)
	}
}

func (p *pass) followsSource(call *syntax.CallExpr, word *syntax.Word) bool {
	i := slices.Index(call.Args, word)
	if i < 0 || i+1 >= len(call.Args) {
		return false
	}
	target, ok := parser.Literal(call.Args[i+1])
	if !ok {
		return false
	}
	return target == "/dev/null" || slices.ContainsFunc(p.imports, func(imp parser.Import) bool {
		return path.Clean(imp.Name) == path.Clean(target)
	})
}

func checkEchoFlags(p *pass, word *syntax.Word, args []*syntax.Word) {
	if len(args) == 0 {
		return
	}
	flag, ok := parser.Literal(args[0])
	if !ok || len(flag) < 2 || flag[0] != '-' || strings.Trim(flag[1:], echoFlagLetters) != "" {
		return
	}
	switch p.dialect {
	case dialect.Posix:
		var fixes []Fix
		start, ok := p.span(word)
		end, endOK := p.span(args[0])
		if ok && endOK && flag == "-n" && len(args) == 2 && word.Lit() == "echo" {
			fixes = append(fixes, reviewFix("Print with printf '%s'", textedit.Edit{Start: start.Start, End: end.End, NewText: "printf '%s'"}))
		}
		p.nonPortableNode(portabilityIssue{code: 3037, shells: posixOnly, feature: "echo " + flag, alternative: "printf"}, args[0], fixes...)
	case dialect.Dash:
		if flag != "-n" {
			p.nonPortableNode(portabilityIssue{code: 3036, shells: ashShells, feature: "echo " + flag + " (only -n is)", alternative: "printf"}, args[0])
		}
	case dialect.Busybox:
		if strings.ContainsAny(flag, busyboxEchoFlags) {
			p.nonPortableNode(portabilityIssue{code: 3036, shells: ashShells, feature: "echo " + flag + " (only -n and -e are)", alternative: "printf"}, args[0])
		}
	}
}

func checkSetOptions(p *pass, args []*syntax.Word) {
	if p.dialect != dialect.Posix {
		return
	}
	for i := 0; i < len(args); i++ {
		word, ok := parser.Literal(args[i])
		switch {
		case !ok, word == "--":
			return
		case strings.HasPrefix(word, "--"):
			p.nonPortableNode(portabilityIssue{code: 3042, shells: posixOnly, feature: "set " + word}, args[i])
			continue
		case !strings.HasPrefix(word, "+") && (!strings.HasPrefix(word, "-") || len(word) < 2):
			return
		}
		for _, letter := range word[1:] {
			if !strings.ContainsRune(posixSetLetters, letter) {
				p.nonPortableNode(portabilityIssue{code: 3041, shells: posixOnly, feature: "set " + word[:1] + string(letter)}, args[i])
			}
		}
		if !strings.HasSuffix(word, "o") || i+1 >= len(args) {
			continue
		}
		option, ok := parser.Literal(args[i+1])
		if !ok {
			return
		}
		i++
		if !posixSetOptions[option] {
			p.nonPortableNode(portabilityIssue{code: 3040, shells: posixOnly, feature: "set -o " + option}, args[i])
		}
	}
}

func checkTrapSignals(p *pass, args []*syntax.Word) {
	if len(args) < 2 {
		return
	}
	for _, arg := range args[1:] {
		signal, ok := parser.Literal(arg)
		if !ok {
			continue
		}
		upper := strings.ToUpper(signal)
		if bashTrapSignals[upper] {
			p.nonPortableNode(portabilityIssue{code: 3047, shells: strictShells, feature: "Trapping " + upper}, arg)
		}
		span, spanned := p.span(arg)
		if !spanned {
			continue
		}
		if strings.HasPrefix(upper, "SIG") {
			issue := portabilityIssue{code: 3048, shells: posixOrDash, feature: "The SIG prefix in " + signal, alternative: upper[len("SIG"):], replacement: upper[len("SIG"):]}
			p.nonPortable(issue, span, p.replacementFix(issue, span)...)
		}
		if signal != upper {
			issue := portabilityIssue{code: 3049, shells: posixOnly, feature: "The lowercase signal name " + signal, alternative: upper, replacement: upper}
			p.nonPortable(issue, span, p.replacementFix(issue, span)...)
		}
	}
}

func checkPrintfFormat(p *pass, args []*syntax.Word) {
	if len(args) == 0 {
		return
	}
	format := parser.LiteralChars(args[0])
	if strings.HasPrefix(format, "-") || !strings.Contains(format, "%q") {
		return
	}
	p.nonPortableNode(portabilityIssue{code: 3050, shells: strictShells, feature: "printf %q"}, args[0])
}

func checkReadOperands(p *pass, call *syntax.CallExpr) {
	if slices.ContainsFunc(call.Args[1:], func(w *syntax.Word) bool { return !startsWithDash(w) }) {
		return
	}
	p.nonPortableNode(portabilityIssue{code: 3061, shells: strictShells, feature: "read without a variable name", alternative: "read -r REPLY"}, call.Args[0])
}

func startsWithDash(w *syntax.Word) bool {
	first := w.Parts[0]
	if dq, ok := first.(*syntax.DblQuoted); ok && len(dq.Parts) > 0 {
		first = dq.Parts[0]
	}
	lit, ok := first.(*syntax.Lit)
	return ok && strings.HasPrefix(lit.Value, "-")
}

func checkPortableFlags(p *pass, name string, args []*syntax.Word) {
	table, ok := portableFlags[name]
	if !ok {
		return
	}
	allowed := table.in(p.dialect)
	for _, arg := range args {
		word, ok := parser.Literal(arg)
		if !ok || word == "--" || !strings.HasPrefix(word, "-") {
			return
		}
		if long, isLong := strings.CutPrefix(word, "--"); isLong {
			flag, _, _ := strings.Cut(long, "=")
			p.nonPortableNode(portabilityIssue{code: 3045, shells: strictShells, feature: name + " --" + flag}, arg)
			return
		}
		for _, letter := range word[1:] {
			if !strings.ContainsRune(allowed, letter) {
				p.nonPortableNode(portabilityIssue{code: 3045, shells: strictShells, feature: name + " -" + string(letter)}, arg)
				return
			}
		}
	}
}
