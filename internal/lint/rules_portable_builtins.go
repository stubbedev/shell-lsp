package lint

import (
	"path"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var portableBuiltinsFamily = family{
	codes: map[Code]meta{
		3036: {shells: ashShells, summary: "echo in dash and busybox sh does not support this flag."},
		3037: {shells: posixOnly, summary: "echo flags are undefined in POSIX sh."},
		3038: {shells: strictShells, summary: "exec options are undefined in POSIX sh."},
		3039: {shells: strictShells, summary: "let is undefined in POSIX sh."},
		3040: {shells: posixOnly, summary: "This set -o option is undefined in POSIX sh."},
		3041: {shells: posixOnly, summary: "This set flag is undefined in POSIX sh."},
		3042: {shells: posixOnly, summary: "This set option is undefined in POSIX sh."},
		3043: {shells: posixOnly, summary: "local is undefined in POSIX sh."},
		3044: {shells: strictShells, summary: "A bash builtin is used that is undefined in POSIX sh."},
		3045: {shells: strictShells, summary: "A builtin option is used that is undefined in POSIX sh."},
		3046: {shells: posixOrDash, summary: "source is undefined in POSIX sh and dash."},
		3047: {shells: strictShells, summary: "Trapping this signal or condition is undefined in POSIX sh."},
		3048: {shells: posixOrDash, summary: "The SIG prefix on a signal name is undefined in POSIX sh and dash."},
		3049: {shells: posixOnly, summary: "Lowercase signal names are undefined in POSIX sh."},
		3050: {shells: strictShells, summary: "printf %q is undefined in POSIX sh."},
		3051: {shells: posixOrDash, summary: "source is undefined in POSIX sh and dash, also when the file it loads is followed."},
		3061: {shells: strictShells, summary: "read without a variable name is undefined in POSIX sh."},
	},
	rules: []rule{{on: nodes(&syntax.CallExpr{}, &syntax.DeclClause{}, &syntax.LetClause{}), node: checkPortableBuiltin}},
}

var (
	bashOnlyBuiltins    = setOf("caller", "builtin", "complete", "compgen", "declare", "dirs", "disown", "enable", "mapfile", "readarray", "pushd", "popd", "shopt", "suspend", "typeset")
	flagCheckedBuiltins = setOf("cd", "export", "hash", "jobs", "printf", "read", "readonly", "trap", "type", "ulimit", "umask", "unset", "wait")
	bashTrapSignals     = setOf("ERR", "DEBUG", "RETURN")
	echoFlagLetters     = "eEsn"
)

func builtinTakes(name string, letter rune, d dialect.Dialect) bool {
	_, ok := shelldocs.BuiltinFlag(name, "-"+string(letter), d)
	return ok
}

func echoAccepts(flag string, d dialect.Dialect) bool {
	if d == dialect.Dash {
		_, ok := shelldocs.BuiltinFlag("echo", flag, d)
		return ok
	}
	return !slices.ContainsFunc([]rune(flag[1:]), func(letter rune) bool { return !builtinTakes("echo", letter, d) })
}

func echoFlagsIn(d dialect.Dialect) string {
	var names []string
	if flags, ok := shelldocs.BuiltinFlags("echo", d); ok {
		for flag := range flags {
			names = append(names, flag.Name)
		}
	}
	if len(names) == 1 {
		return "only " + names[0] + " is"
	}
	return "only " + strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1] + " are"
}

func posixSetOption(name string) bool {
	option, ok := shelldocs.Term(shelldocs.SetOptions, name)
	return ok && option.Dialects.Has(dialect.Posix)
}

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
		p.nonPortableNode(portabilityIssue{code: 3039, feature: "let", alternative: "$((...))"}, n)
	}
}

func checkPortableDeclaration(p *pass, decl *syntax.DeclClause) {
	name := decl.Variant.Value
	switch {
	case name == "local":
		p.nonPortableNode(portabilityIssue{code: 3043, feature: "local"}, decl)
	case bashOnlyBuiltins[name]:
		p.nonPortableNode(portabilityIssue{code: 3044, feature: "The builtin " + name}, decl)
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
	p.nonPortableNode(portabilityIssue{code: 3038, feature: "exec " + flag, alternative: "exec without options (env -i for a clean environment)"}, call.Args[1])
	return true
}

func checkResolvedBuiltin(p *pass, call *syntax.CallExpr) {
	command, ok := p.resolved(call)
	if !ok {
		command = resolveAt(call.Args, 0)
	}
	if !command.literal {
		return
	}
	word := command.word
	switch name := path.Base(command.name); {
	case name == "let":
		p.nonPortableNode(portabilityIssue{code: 3039, feature: "let", alternative: "$((...))"}, call)
	case name == "local":
		p.nonPortableNode(portabilityIssue{code: 3043, feature: "local"}, call)
	case name == "source":
		issue := portabilityIssue{code: 3046, feature: "source", alternative: "."}
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
		p.nonPortableNode(portabilityIssue{code: 3044, feature: "The builtin " + name}, call)
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
	if p.dialect == dialect.Posix {
		var fixes []Fix
		start, ok := p.span(word)
		end, endOK := p.span(args[0])
		if ok && endOK && flag == "-n" && len(args) == 2 && word.Lit() == "echo" {
			fixes = append(fixes, reviewFix("Print with printf '%s'", textedit.Edit{Start: start.Start, End: end.End, NewText: "printf '%s'"}))
		}
		p.nonPortableNode(portabilityIssue{code: 3037, feature: "echo " + flag, alternative: "printf"}, args[0], fixes...)
		return
	}
	if !ashShells.Has(p.dialect) || echoAccepts(flag, p.dialect) {
		return
	}
	p.nonPortableNode(portabilityIssue{code: 3036, feature: "echo " + flag + " (" + echoFlagsIn(p.dialect) + ")", alternative: "printf"}, args[0])
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
			p.nonPortableNode(portabilityIssue{code: 3042, feature: "set " + word}, args[i])
			continue
		case !strings.HasPrefix(word, "+") && (!strings.HasPrefix(word, "-") || len(word) < 2):
			return
		}
		for _, letter := range word[1:] {
			if !builtinTakes("set", letter, dialect.Posix) {
				p.nonPortableNode(portabilityIssue{code: 3041, feature: "set " + word[:1] + string(letter)}, args[i])
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
		if !posixSetOption(option) {
			p.nonPortableNode(portabilityIssue{code: 3040, feature: "set -o " + option}, args[i])
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
			p.nonPortableNode(portabilityIssue{code: 3047, feature: "Trapping " + upper}, arg)
		}
		span, spanned := p.span(arg)
		if !spanned {
			continue
		}
		if strings.HasPrefix(upper, "SIG") {
			issue := portabilityIssue{code: 3048, feature: "The SIG prefix in " + signal, alternative: upper[len("SIG"):], replacement: upper[len("SIG"):]}
			p.nonPortable(issue, span, p.replacementFix(issue, span)...)
		}
		if signal != upper {
			issue := portabilityIssue{code: 3049, feature: "The lowercase signal name " + signal, alternative: upper, replacement: upper}
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
	p.nonPortableNode(portabilityIssue{code: 3050, feature: "printf %q"}, args[0])
}

func checkReadOperands(p *pass, call *syntax.CallExpr) {
	if slices.ContainsFunc(call.Args[1:], func(w *syntax.Word) bool { return !startsWithDash(w) }) {
		return
	}
	p.nonPortableNode(portabilityIssue{code: 3061, feature: "read without a variable name", alternative: "read -r REPLY"}, call.Args[0])
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
	if !flagCheckedBuiltins[name] {
		return
	}
	for _, arg := range args {
		word, ok := parser.Literal(arg)
		if !ok || word == "--" || !strings.HasPrefix(word, "-") {
			return
		}
		if long, isLong := strings.CutPrefix(word, "--"); isLong {
			flag, _, _ := strings.Cut(long, "=")
			p.nonPortableNode(portabilityIssue{code: 3045, feature: name + " --" + flag}, arg)
			return
		}
		for _, letter := range word[1:] {
			if !builtinTakes(name, letter, p.dialect) {
				p.nonPortableNode(portabilityIssue{code: 3045, feature: name + " -" + string(letter)}, arg)
				return
			}
		}
	}
}
