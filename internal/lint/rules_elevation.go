package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var elevationFamily = family{
	codes: map[Code]meta{
		2024: {severity: SeverityWarning},
		2029: {severity: SeverityInfo},
		2117: {severity: SeverityInfo},
		2232: {severity: SeverityWarning},
	},
	commands: []commandRule{
		{match: matchBase, names: elevationCommands, check: checkElevatedRedirection},
		{match: matchBase, names: elevationCommands, check: checkElevatedBuiltin},
		{match: matchBase, names: []string{"su"}, check: checkInteractiveSu},
		{match: matchBase, names: []string{"ssh"}, check: checkSSHExpansion},
	},
}

var elevationCommands = []string{"sudo", "doas", "run0"}

func checkElevatedRedirection(p *pass, c command) {
	stmt, ok := p.parent().(*syntax.Stmt)
	if !ok {
		return
	}
	for _, r := range stmt.Redirs {
		if r.N != nil || r.Word == nil || parser.LiteralIs(r.Word, "/dev/null") {
			continue
		}
		severity, advice := p.meta(2024).severity, ""
		switch r.Op {
		case syntax.RdrIn:
			severity, advice = SeverityInfo, "read the file with '"+c.name+" cat file |' instead."
		case syntax.RdrOut, syntax.RdrAll:
			advice = "write through '| " + c.name + " tee file >/dev/null' instead."
		case syntax.AppOut, syntax.AppAll:
			advice = "append through '| " + c.name + " tee -a file >/dev/null' instead."
		default:
			continue
		}
		at := parser.Offset(r.OpPos)
		p.reportWithSeverity(2024, severity, parser.Span{Start: at, End: at + len(r.Op.String())},
			c.name+" does not apply to this redirection; the shell opens the file with your own permissions. To "+advice)
	}
}

var (
	elevationOptions  = optionGrammar{short: "vAknSbEHPa:g:h:p:u:c:T:r:", stopAtOperand: true}
	unelevatableNames = setOf("cd", "command", "declare", "eval", "exec", "exit", "export", "hash", "history", "local", "popd",
		"pushd", "read", "readonly", "return", "set", "source", "trap", "type", "typeset", "ulimit", "umask", "unset", "wait")
)

func checkElevatedBuiltin(p *pass, c command) {
	options, ok := elevationOptions.parse(c.args)
	if !ok || len(options.operands) == 0 {
		return
	}
	if builtin, ok := parser.Literal(options.operands[0]); ok && unelevatableNames[builtin] {
		p.reportNode(2232, c.call, c.name+" runs programs, but "+builtin+" is a shell builtin with no program to run; use "+c.name+" sh -c '...' if it must run elevated.")
	}
}

func checkInteractiveSu(p *pass, c command) {
	if len(c.args) > 1 {
		return
	}
	for _, n := range p.stack {
		switch n := n.(type) {
		case *syntax.BinaryCmd:
			if pipes(n.Op) {
				return
			}
		case *syntax.Stmt:
			if len(n.Redirs) > 0 {
				return
			}
		}
	}
	p.reportNode(2117, c.call, "su starts an interactive shell and the rest of the script continues only after it exits; use su -c or sudo to run commands as another user.")
}

func checkSSHExpansion(p *pass, c command) {
	if len(c.args) < 2 {
		return
	}
	for _, arg := range c.args {
		if text, _ := parser.Text(arg, "_"); strings.HasPrefix(text, "-") {
			return
		}
	}
	last := c.args[len(c.args)-1]
	if len(last.Parts) != 1 {
		return
	}
	dq, ok := last.Parts[0].(*syntax.DblQuoted)
	if !ok {
		return
	}
	for _, part := range dq.Parts {
		if isExpansion(part) {
			p.reportNode(2029, part, "This expands on the client before ssh sends the command; escape it or single-quote the command to expand it on the server.")
			return
		}
	}
}
