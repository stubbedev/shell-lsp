package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const codeLeakedTempFile = nativeBase + 2006

var tempFileFamily = family{
	codes: map[Code]meta{
		codeLeakedTempFile: {severity: SeverityInfo, bug: true},
	},
	commands: []commandRule{
		{match: matchExact, names: []string{"mktemp"}, check: checkLeakedTempFile},
	},
}

var (
	disposingCommands = setOf("rm", "rmdir", "unlink", "shred", "mv", "srm", "trash", "trash-put")
	handOffCommands   = setOf("echo", "printf", "exec", "export", "readonly", "trap")
	exitSignals       = setOf("exit", "sigexit", "0")
	tempDirectories   = []string{"/tmp/", "/var/tmp/", "/dev/shm/"}
)

type tempRequest struct {
	directory bool
	tracked   bool
}

func checkLeakedTempFile(p *pass, c command) {
	request := tempRequestOf(c.args)
	if !request.tracked {
		return
	}
	name, stmt, depth, ok := p.tempFileVariable()
	if !ok || !p.executedScript() || p.disposesOf(name, c.call) {
		return
	}
	kind, remove := "file", "rm -f"
	if request.directory {
		kind, remove = "directory", "rm -rf"
	}
	p.reportNode(codeLeakedTempFile, c.word, "The temporary "+kind+" in $"+name+" is never removed: nothing runs rm on it or cleans it up from a trap, so every run leaves it behind.", p.cleanupTrapFix(stmt, depth, name, remove)...)
}

func tempRequestOf(args []*syntax.Word) tempRequest {
	request := tempRequest{tracked: true}
	inTempDir, template := false, ""
	for i := 0; i < len(args); i++ {
		lit, ok := parser.Literal(args[i])
		switch {
		case ok && lit == "--suffix":
			i++
		case !ok:
			request.tracked = false
		case lit == "--dry-run" || strings.HasPrefix(lit, "--tmpdir="):
			request.tracked = false
		case lit == "--directory":
			request.directory = true
		case lit == "--tmpdir":
			inTempDir = true
		case strings.HasPrefix(lit, "--"):
		case strings.HasPrefix(lit, "-") && len(lit) > 1:
			if strings.ContainsAny(lit, "up") {
				request.tracked = false
			}
			request.directory = request.directory || strings.ContainsRune(lit, 'd')
			inTempDir = inTempDir || strings.ContainsRune(lit, 't')
		default:
			template = lit
		}
	}
	if template != "" && !inTempDir && !hasAnyPrefix(template, tempDirectories) {
		request.tracked = false
	}
	return request
}

func (p *pass) tempFileVariable() (string, *syntax.Stmt, int, bool) {
	cs, ok := p.ancestor(1).(*syntax.CmdSubst)
	if !ok || len(cs.Stmts) != 1 {
		return "", nil, 0, false
	}
	name := ""
	for depth := 2; p.ancestor(depth) != nil; depth++ {
		switch n := p.ancestor(depth).(type) {
		case *syntax.Word, *syntax.DblQuoted:
		case *syntax.Assign:
			if n.Name == nil || n.Index != nil || n.Append {
				return "", nil, 0, false
			}
			name = n.Name.Value
		case *syntax.CallExpr, *syntax.DeclClause:
			if name == "" {
				return "", nil, 0, false
			}
		case *syntax.Stmt:
			return name, n, depth, name != ""
		default:
			return "", nil, 0, false
		}
	}
	return "", nil, 0, false
}

func (p *pass) disposesOf(name string, creator *syntax.CallExpr) bool {
	return parser.Any(p.tree.File, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.CallExpr:
			return n != creator && p.callDisposes(n, name)
		case *syntax.Assign:
			reassigns := n.Name != nil && n.Name.Value == name && !n.Naked
			if !assignsCreator(n, creator) && (reassigns || (n.Value != nil && copiesVariable(n.Value, name))) {
				return true
			}
			return n.Array != nil && slices.ContainsFunc(n.Array.Elems, func(elem *syntax.ArrayElem) bool {
				return elem.Value != nil && copiesVariable(elem.Value, name)
			})
		case *syntax.WordIter:
			return slices.ContainsFunc(n.Items, func(item *syntax.Word) bool { return referencesVariable(item, name) })
		}
		return false
	})
}

func (p *pass) callDisposes(call *syntax.CallExpr, name string) bool {
	command, ok := p.runs(call.Args)
	if !ok {
		return false
	}
	if command.name == "trap" {
		return mentionsVariable(p.wordsText(command.args), name)
	}
	if !disposingCommands[command.name] && !handOffCommands[command.name] {
		if _, defined := p.tree.FunctionNamed(command.name); !defined {
			return false
		}
	}
	for _, arg := range command.args {
		if referencesVariable(arg, name) {
			return true
		}
	}
	return false
}

func assignsCreator(a *syntax.Assign, creator *syntax.CallExpr) bool {
	if a.Value == nil {
		return false
	}
	return parser.Any(a.Value, func(n syntax.Node) bool { return n == creator })
}

func referencesVariable(w *syntax.Word, name string) bool {
	return findsVariable(w, name, true)
}

func copiesVariable(w *syntax.Word, name string) bool {
	return findsVariable(w, name, false)
}

func findsVariable(w *syntax.Word, name string, inCommands bool) bool {
	found := false
	parser.Walk(w, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.CmdSubst, *syntax.ProcSubst:
			return inCommands
		case *syntax.ParamExp:
			found = found || (n.Param != nil && n.Param.Value == name)
		}
		return !found
	})
	return found
}

func (p *pass) wordsText(words []*syntax.Word) string {
	var sb strings.Builder
	for _, w := range words {
		if span, ok := p.span(w); ok {
			sb.WriteString(p.text[span.Start:span.End])
			sb.WriteByte(' ')
		}
	}
	return sb.String()
}

func mentionsVariable(text, name string) bool {
	for _, lead := range []string{"$", "${"} {
		needle := lead + name
		for at := 0; ; {
			i := strings.Index(text[at:], needle)
			if i < 0 {
				break
			}
			end := at + i + len(needle)
			if end == len(text) || !parser.IsNameChar(text[end]) {
				return true
			}
			at = end
		}
	}
	return false
}

func (p *pass) cleanupTrapFix(stmt *syntax.Stmt, depth int, name, remove string) []Fix {
	if _, top := p.ancestor(depth + 1).(*syntax.File); !top || p.trapsExit() {
		return nil
	}
	span, ok := p.span(stmt)
	if !ok {
		return nil
	}
	return []Fix{reviewFix("Remove it when the script exits", textedit.Edit{Start: span.End, End: span.End, NewText: "\ntrap '" + remove + " -- \"$" + name + "\"' EXIT"})}
}

func (p *pass) trapsExit() bool {
	return parser.Any(p.tree.File, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok {
			return false
		}
		command, ok := p.runs(call.Args)
		if !ok || command.name != "trap" {
			return false
		}
		_, signals, ok := trapOperands(command.args)
		return ok && slices.ContainsFunc(signals, func(signal *syntax.Word) bool {
			lit, literal := parser.Literal(signal)
			return literal && exitSignals[strings.ToLower(lit)]
		})
	})
}
