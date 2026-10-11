package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/getopt"
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

var (
	scriptShells     = setOf("sh", "bash", "zsh", "dash", "ksh", "mksh", "ash")
	sourcingCommands = setOf("source", ".")
)

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
	switch {
	case scriptShells[name]:
		p.checkShellScriptSource(runner, args)
	case name == "eval":
		for _, arg := range args {
			if downloader, ok := downloadedOutput(arg); ok {
				p.reportDownloaded(arg, downloader, "the "+name+" command")
				return
			}
		}
	case sourcingCommands[name] && len(args) > 0:
		p.checkScriptOperand(runner, args[0], "the "+name+" command")
	}
}

func (p *pass) checkShellScriptSource(runner *syntax.Word, args []*syntax.Word) {
	for i := 0; i < len(args); i++ {
		lit, literal := parser.Literal(args[i])
		switch {
		case literal && lit == getopt.EndOfOptions:
			if i+1 < len(args) {
				p.checkScriptOperand(runner, args[i+1], "the shell")
			}
			return
		case literal && shortOptions(lit):
			switch {
			case strings.ContainsRune(lit, 'c'):
				if i+1 < len(args) {
					if downloader, ok := downloadedOutput(args[i+1]); ok {
						p.reportDownloaded(args[i+1], downloader, "the shell")
					}
				}
				return
			case strings.ContainsRune(lit, 's'):
				p.checkPipedScript(runner, "the shell")
				return
			case strings.HasSuffix(lit, "o") || strings.HasSuffix(lit, "O"):
				i++
			}
		case literal && getopt.IsOption(lit):
		default:
			p.checkScriptOperand(runner, args[i], "the shell")
			return
		}
	}
	p.checkPipedScript(runner, "the shell")
}

func shortOptions(word string) bool {
	return len(word) > 1 && (word[0] == '-' || word[0] == '+') && word[1] != '-'
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

const curlValuedShort = "AbcCdDeEFHKmPQrtTuUwxXyYz"

func curlWritesStdout(args []*syntax.Word) bool {
	stdout := true
	for i := 0; i < len(args); i++ {
		lit, ok := parser.Literal(args[i])
		if !ok || !getopt.IsOption(lit) {
			continue
		}
		if long, isLong := strings.CutPrefix(lit, "--"); isLong {
			switch {
			case long == "remote-name" || long == "remote-name-all" || long == "output-dir":
				return false
			case long == "output":
				stdout = followedBy(args, i, "-")
				i++
			case strings.HasPrefix(long, "output="):
				stdout = long == "output=-"
			}
			continue
		}
		cluster := lit[1:]
		for j := 0; j < len(cluster); j++ {
			switch letter := cluster[j]; {
			case letter == 'O':
				return false
			case letter == 'o':
				if value := cluster[j+1:]; value != "" {
					stdout = value == "-"
				} else {
					stdout = followedBy(args, i, "-")
					i++
				}
				j = len(cluster)
			case strings.IndexByte(curlValuedShort, letter) >= 0:
				if j+1 == len(cluster) {
					i++
				}
				j = len(cluster)
			}
		}
	}
	return stdout
}

func wgetWritesStdout(args []*syntax.Word) bool {
	for i, arg := range args {
		lit, ok := parser.Literal(arg)
		if !ok || !getopt.IsOption(lit) {
			continue
		}
		switch {
		case lit == "--output-document=-" || lit == "--output-document=/dev/stdout":
			return true
		case lit == "--output-document" || (!strings.HasPrefix(lit, "--") && strings.HasSuffix(lit, "O")):
			return followedBy(args, i, "-") || followedBy(args, i, "/dev/stdout")
		case !strings.HasPrefix(lit, "--") && strings.HasSuffix(lit, "O-"):
			return true
		}
	}
	return false
}

func followedBy(args []*syntax.Word, i int, want string) bool {
	if i+1 >= len(args) {
		return false
	}
	lit, ok := parser.Literal(args[i+1])
	return ok && lit == want
}
