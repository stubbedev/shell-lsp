package lint

import (
	"path"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

type command struct {
	call    *syntax.CallExpr
	name    string
	word    *syntax.Word
	args    []*syntax.Word
	builtin bool
}

func commandOf(call *syntax.CallExpr) (command, bool) {
	if len(call.Args) == 0 {
		return command{}, false
	}
	name, ok := parser.Literal(call.Args[0])
	if !ok {
		return command{}, false
	}
	c := command{call: call, name: name, word: call.Args[0], args: call.Args[1:]}
	if name == "builtin" && len(call.Args) > 1 {
		if inner, ok := parser.Literal(call.Args[1]); ok {
			c.name, c.word, c.args, c.builtin = inner, call.Args[1], call.Args[2:], true
		}
	}
	return c, true
}

func (c command) is(names ...string) bool {
	return !strings.Contains(c.name, "/") && slices.Contains(names, c.name)
}

func (c command) isBase(names ...string) bool {
	return !c.builtin && slices.Contains(names, path.Base(c.name))
}

func (c command) literalArgs() ([]string, bool) {
	out := make([]string, len(c.args))
	for i, a := range c.args {
		lit, ok := parser.Literal(a)
		if !ok {
			return nil, false
		}
		out[i] = lit
	}
	return out, true
}

type flagSet struct {
	short    map[byte]bool
	long     map[string]bool
	operands []*syntax.Word
}

func (f flagSet) has(short byte, long string) bool {
	return (short != 0 && f.short[short]) || (long != "" && f.long[long])
}

func parseFlags(args []*syntax.Word, withArgument string) flagSet {
	f := flagSet{short: map[byte]bool{}, long: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		lit, ok := parser.Literal(args[i])
		switch {
		case !ok || lit == "-" || !strings.HasPrefix(lit, "-"):
			f.operands = append(f.operands, args[i])
		case lit == "--":
			f.operands = append(f.operands, args[i+1:]...)
			return f
		case strings.HasPrefix(lit, "--"):
			name, _, _ := strings.Cut(lit[2:], "=")
			f.long[name] = true
		default:
			for j := 1; j < len(lit); j++ {
				f.short[lit[j]] = true
				if strings.IndexByte(withArgument, lit[j]) >= 0 {
					if j == len(lit)-1 {
						i++
					}
					break
				}
			}
		}
	}
	return f
}

var commandWrappers = map[string]string{
	"command": "",
	"builtin": "",
	"busybox": "",
	"exec":    "a",
	"time":    "fo",
}

func (p *pass) effectiveCommandWord(call *syntax.CallExpr) *syntax.Word {
	for i := 0; i < len(call.Args); {
		lit, ok := parser.Literal(call.Args[i])
		withArgument, wrapper := commandWrappers[lit]
		if lit == "run" && p.bats {
			wrapper = true
		}
		if !ok || !wrapper {
			return call.Args[i]
		}
		i++
		for i < len(call.Args) {
			opt, ok := parser.Literal(call.Args[i])
			if !ok || !strings.HasPrefix(opt, "-") || opt == "-" {
				break
			}
			i++
			if opt == "--" {
				break
			}
			if strings.ContainsAny(opt[1:], withArgument) {
				i++
			}
		}
	}
	return nil
}

func isGlob(w *syntax.Word) bool {
	sawOpen := false
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.ExtGlob:
			return true
		case *syntax.Lit:
			for i := 0; i < len(p.Value); i++ {
				switch p.Value[i] {
				case '\\':
					i++
				case '*', '?':
					return true
				case '[':
					sawOpen = true
				case ']':
					if sawOpen {
						return true
					}
				}
			}
		}
	}
	return false
}

func pipelineStages(stmt *syntax.Stmt, into []*syntax.Stmt) []*syntax.Stmt {
	if bin, ok := stmt.Cmd.(*syntax.BinaryCmd); ok && (bin.Op == syntax.Pipe || bin.Op == syntax.PipeAll) {
		return pipelineStages(bin.Y, pipelineStages(bin.X, into))
	}
	return append(into, stmt)
}

func (p *pass) enclosingPipeline() ([]*syntax.Stmt, int) {
	stmt, ok := p.parent().(*syntax.Stmt)
	if !ok {
		return nil, -1
	}
	top := stmt
	for depth := 1; ; depth++ {
		bin, ok := p.ancestor(depth).(*syntax.BinaryCmd)
		if !ok || (bin.Op != syntax.Pipe && bin.Op != syntax.PipeAll) {
			break
		}
		next, ok := p.ancestor(depth + 1).(*syntax.Stmt)
		if !ok {
			break
		}
		top = next
		depth++
	}
	stages := pipelineStages(top, nil)
	return stages, slices.Index(stages, stmt)
}
