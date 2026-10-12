package lint

import (
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const codeDownloadedScript = nativeBase + 1005

var downloadFamily = family{
	codes: map[Code]meta{
		codeDownloadedScript: {severity: SeverityWarning},
	},
	rules: []rule{
		{on: nodes(&syntax.CallExpr{}), node: checkDownloadedScript},
	},
}

const stdinPath = "/dev/stdin"

func checkDownloadedScript(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok {
		return
	}
	runner, args, name, ok := runCommand(call.Args)
	if !ok {
		return
	}
	if inv, ok := shellInvocationOf(name, args); ok {
		p.checkShellScriptSource(runner, inv)
		return
	}
	switch {
	case name == "eval":
		for _, arg := range args {
			if downloader, ok := downloadedOutput(arg); ok {
				p.reportDownloaded(arg, downloader, "the "+name+" command")
				return
			}
		}
	case parser.IsSourceName(name) && len(args) > 0:
		p.checkScriptOperand(runner, args[0], "the "+name+" command")
	}
}

func (p *pass) checkShellScriptSource(runner *syntax.Word, inv shellInvocation) {
	switch inv.mode {
	case runsCommandString:
		if downloader, ok := downloadedOutput(inv.operand); ok {
			p.reportDownloaded(inv.operand, downloader, "the shell")
		}
	case runsScriptFile:
		p.checkScriptOperand(runner, inv.operand, "the shell")
	case runsStdin:
		p.checkPipedScript(runner, "the shell")
	}
}

func (p *pass) checkScriptOperand(at, operand *syntax.Word, runner string) {
	if lit, ok := parser.Literal(operand); ok && lit == stdinPath {
		p.checkPipedScript(at, runner)
		return
	}
	ps, ok := solePart(operand).(*syntax.ProcSubst)
	if !ok || ps.Op != syntax.CmdIn || len(ps.Stmts) != 1 {
		return
	}
	if downloader, ok := downloadingStmt(ps.Stmts[0]); ok {
		p.reportDownloaded(operand, downloader, runner)
	}
}

func (p *pass) checkPipedScript(at *syntax.Word, runner string) {
	stages, index := p.enclosingPipeline()
	if index < 1 {
		return
	}
	if downloader, ok := downloadingStmt(stages[index-1]); ok {
		p.reportDownloaded(at, downloader, runner)
	}
}

func (p *pass) reportDownloaded(at syntax.Node, downloader, runner string) {
	p.reportNode(codeDownloadedScript, at, "Code fetched by "+downloader+" goes straight to "+runner+", so whatever the server sends runs unchecked; save it to a file and verify its checksum or signature before running it.")
}

func runCommand(words []*syntax.Word) (*syntax.Word, []*syntax.Word, string, bool) {
	start := parser.Wrappers{Programs: true}.Start(words)
	if start < 0 {
		return nil, nil, "", false
	}
	name, ok := parser.Literal(words[start])
	return words[start], words[start+1:], name, ok
}

func downloadedOutput(w *syntax.Word) (string, bool) {
	cs, ok := solePart(w).(*syntax.CmdSubst)
	if !ok || len(cs.Stmts) != 1 {
		return "", false
	}
	return downloadingStmt(cs.Stmts[0])
}

func downloadingStmt(stmt *syntax.Stmt) (string, bool) {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(stmt.Redirs) > 0 {
		return "", false
	}
	_, args, name, ok := runCommand(call.Args)
	if !ok {
		return "", false
	}
	switch name {
	case "curl":
		return name, curlWritesStdout(args)
	case "wget":
		return name, wgetWritesStdout(args)
	}
	return "", false
}

func curlWritesStdout(args []*syntax.Word) bool {
	stdout := true
	for _, opt := range parseOptions("curl", args).Options {
		switch opt.Name {
		case "-O", "--remote-name", "--remote-name-all", "--output-dir":
			return false
		case "-o", "--output":
			stdout = argumentIs(opt, "-")
		}
	}
	return stdout
}

func wgetWritesStdout(args []*syntax.Word) bool {
	for _, opt := range parseOptions("wget", args).Options {
		if opt.Name == "-O" || opt.Name == "--output-document" {
			return argumentIs(opt, "-", "/dev/stdout")
		}
	}
	return false
}
