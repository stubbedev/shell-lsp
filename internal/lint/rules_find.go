package lint

import (
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

var findFamily = family{
	codes: map[Code]meta{
		2014: {severity: SeverityInfo},
		2061: {severity: SeverityWarning},
		2067: {severity: SeverityError},
		2146: {severity: SeverityWarning},
		2150: {severity: SeverityWarning},
		2156: {severity: SeverityWarning},
		2185: {severity: SeverityInfo},
		2227: {severity: SeverityWarning},
	},
	commands: []commandRule{
		{match: matchBase, names: []string{"find"}, check: checkFindPatterns},
		{match: matchBase, names: []string{"find"}, check: checkFindActionPrecedence},
		{match: matchBase, names: []string{"find"}, check: checkFindExec},
		{match: matchBase, names: []string{"find"}, check: checkFindExecArguments},
		{match: matchBase, names: []string{"find"}, check: checkFindInlineScript},
		{match: matchBase, names: []string{"find"}, check: checkFindStartingPoint},
		{match: matchBase, names: []string{"find"}, check: checkFindRedirection},
	},
}

var (
	findPatternPredicates = setOf("-name", "-iname", "-path", "-ipath", "-wholename", "-iwholename", "-lname", "-ilname", "-regex", "-iregex")
	findNamePredicates    = setOf("-name", "-iname", "-regex", "-iregex", "-wholename", "-iwholename")
	findActions           = setOf("-exec", "-execdir", "-ok", "-okdir", "-delete", "-print", "-print0", "-printf", "-fprint", "-fprint0", "-fprintf", "-fls", "-ls")
	findExecActions       = setOf("-exec", "-execdir", "-ok", "-okdir")
	findTerminatedActions = setOf("-exec", "-execdir")
	findExecTerminators   = setOf(";", "+")
	findInlineShells      = setOf("sh", "bash", "dash", "ksh")
	findOr                = setOf("-o", "-or")
)

func checkFindPatterns(p *pass, c command) {
	for i := 0; i+1 < len(c.args); i++ {
		predicate, ok := parser.Literal(c.args[i])
		if ok && findPatternPredicates[predicate] && isGlob(c.args[i+1]) {
			p.reportNode(2061, c.args[i+1], "Quote the pattern after "+predicate+" so the shell does not expand it before find sees it.")
		}
	}
}

func checkFindActionPrecedence(p *pass, c command) {
	for i := 0; i+5 < len(c.args); i++ {
		w := c.args[i : i+6]
		if literalIn(w[0], findNamePredicates) && literalIn(w[2], findOr) &&
			literalIn(w[3], findNamePredicates) && literalIn(w[5], findActions) {
			action, _ := parser.Literal(w[5])
			p.reportNode(2146, w[5], action+" only applies to the last -o branch; group the alternatives in \\( \\) so it applies to all of them.")
		}
	}
}

func checkFindExec(p *pass, c command) {
	for i := 0; i+2 < len(c.args); i++ {
		if !literalIn(c.args[i], findExecActions) || !literalIn(c.args[i+2], findExecTerminators) {
			continue
		}
		if command, _ := parser.Text(c.args[i+1], " "); strings.ContainsAny(command, " |;") {
			action, _ := parser.Literal(c.args[i])
			p.reportNode(2150, c.args[i], action+" runs a program directly, not a shell command line; pass the program and its arguments as separate words, or use sh -c.")
		}
	}
}

func checkFindInlineScript(p *pass, c command) {
	sawExec, sawShell := false, false
	for i, arg := range c.args {
		switch {
		case literalIn(arg, findExecActions):
			sawExec = true
		case sawExec && literalIn(arg, findInlineShells):
			sawShell = true
		case sawShell && literalIs(arg, "-c") && i+1 < len(c.args):
			if script, _ := parser.Text(c.args[i+1], ""); strings.Contains(script, "{}") {
				p.reportNode(2156, c.args[i+1], "Pass {} to the inline script as an argument and use \"$1\"; injecting file names into the script text breaks on special characters.")
			}
		}
	}
}

const findOptionLetters = "-EHLPXdfsxO0123456789"

func checkFindStartingPoint(p *pass, c command) {
	if parseFlags(c.args, "").has(0, "help") {
		return
	}
	for _, arg := range c.args {
		text, _ := parser.Text(arg, "_")
		if !strings.HasPrefix(text, "-") {
			return
		}
		if len(text) > 2 && strings.Trim(text, findOptionLetters) != "" {
			break
		}
	}
	p.reportNode(2185, c.word, "Give find an explicit starting path such as . ; some versions of find require one.")
}

func checkFindRedirection(p *pass, c command) {
	stmt, ok := p.parent().(*syntax.Stmt)
	if !ok || len(c.args) == 0 || len(stmt.Redirs) == 0 {
		return
	}
	first := slices.MinFunc(stmt.Redirs, func(a, b *syntax.Redirect) int {
		return parser.Offset(a.Pos()) - parser.Offset(b.Pos())
	})
	if parser.Offset(first.Pos()) < parser.Offset(c.args[len(c.args)-1].Pos()) {
		p.reportNode(2227, first, "This redirection applies to find itself, not to the command run by -exec; use sh -c to redirect inside the action.")
	}
}

func checkFindExecArguments(p *pass, c command) {
	inAction := false
	for _, arg := range c.args {
		if inAction {
			p.reportExpandedOnce(arg)
		}
		switch {
		case literalIn(arg, findExecTerminators):
			inAction = false
		case literalIn(arg, findTerminatedActions):
			inAction = true
		}
	}
	if inAction && len(c.args) > 0 {
		p.reportNode(2067, c.args[len(c.args)-1], "This find -exec has no separate ; or + argument; the shell consumed it as a command separator. End the action with \\; or +.")
	}
}

func (p *pass) reportExpandedOnce(w *syntax.Word) {
	const message = "The shell expands this once before find runs, not once per file; quote it, or expand it inside sh -c."
	for _, part := range w.Parts {
		switch part := part.(type) {
		case *syntax.CmdSubst, *syntax.ExtGlob:
			if span, ok := p.span(part); ok {
				p.report(2014, span, message)
			}
		case *syntax.Lit:
			lit, ok := p.span(part)
			if !ok {
				continue
			}
			for _, at := range globTokens(part.Value) {
				p.report(2014, parser.Span{Start: lit.Start + at.Start, End: lit.Start + at.End}, message)
			}
		}
	}
}

func globTokens(value string) []parser.Span {
	var out []parser.Span
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\\':
			i++
		case '?':
			out = append(out, parser.Span{Start: i, End: i + 1})
		case '*':
			start := i
			for i+1 < len(value) && value[i+1] == '*' {
				i++
			}
			out = append(out, parser.Span{Start: start, End: i + 1})
		case '[':
			if end := bracketEnd(value, i); end > 0 {
				out = append(out, parser.Span{Start: i, End: end + 1})
				i = end
			}
		}
	}
	return out
}
