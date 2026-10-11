package lint

import (
	"path"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type command struct {
	call    *syntax.CallExpr
	decl    *syntax.DeclClause
	name    string
	word    *syntax.Word
	args    []*syntax.Word
	builtin bool
}

type matching uint8

const (
	matchExact matching = iota
	matchBase
)

type commandRule struct {
	match matching
	names []string
	check func(p *pass, c command)
}

type commandDispatch struct {
	exact map[string][]func(*pass, command)
	base  map[string][]func(*pass, command)
}

func newCommandDispatch(rules []commandRule) commandDispatch {
	d := commandDispatch{exact: map[string][]func(*pass, command){}, base: map[string][]func(*pass, command){}}
	for _, r := range rules {
		table := d.exact
		if r.match == matchBase {
			table = d.base
		}
		for _, name := range r.names {
			table[name] = append(table[name], r.check)
		}
	}
	return d
}

func (d commandDispatch) visit(p *pass, n syntax.Node) {
	var c command
	switch n := n.(type) {
	case *syntax.CallExpr:
		found, ok := commandOf(n)
		if !ok {
			return
		}
		c = found
	case *syntax.DeclClause:
		c = declarationOf(n)
	default:
		return
	}
	if !c.builtin {
		for _, check := range d.base[path.Base(c.name)] {
			check(p, c)
		}
	}
	if strings.Contains(c.name, "/") {
		return
	}
	for _, check := range d.exact[c.name] {
		check(p, c)
	}
}

func declarationOf(decl *syntax.DeclClause) command {
	c := command{decl: decl, name: decl.Variant.Value}
	for _, a := range decl.Args {
		if a.Naked && a.Name == nil {
			c.args = append(c.args, a.Value)
		}
	}
	return c
}

func (c command) nameNode() syntax.Node {
	if c.decl != nil {
		return c.decl.Variant
	}
	return c.word
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

func literalIn(w *syntax.Word, set map[string]bool) bool {
	lit, ok := parser.Literal(w)
	return ok && set[lit]
}

type shortFlags [4]uint64

func (s *shortFlags) add(c byte)          { s[c/64] |= 1 << (c % 64) }
func (s shortFlags) contains(c byte) bool { return s[c/64]&(1<<(c%64)) != 0 }

type flagSet struct {
	short    shortFlags
	long     []string
	operands []*syntax.Word
}

func (f flagSet) has(short byte, long string) bool {
	return (short != 0 && f.short.contains(short)) || (long != "" && slices.Contains(f.long, long))
}

func parseFlags(args []*syntax.Word, withArgument string) flagSet {
	var f flagSet
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
			f.long = append(f.long, name)
		default:
			for j := 1; j < len(lit); j++ {
				f.short.add(lit[j])
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

type optionGrammar struct {
	short         string
	long          map[string]bool
	anyLong       bool
	stopAtOperand bool
}

type parsedOption struct {
	name  string
	word  *syntax.Word
	value *syntax.Word
}

type parsedOptions struct {
	options  []parsedOption
	operands []*syntax.Word
}

func (o parsedOptions) has(names ...string) bool {
	return slices.ContainsFunc(o.options, func(opt parsedOption) bool { return slices.Contains(names, opt.name) })
}

func (g optionGrammar) parse(args []*syntax.Word) (parsedOptions, bool) {
	var out parsedOptions
	for i := 0; i < len(args); i++ {
		lit, ok := parser.Literal(args[i])
		switch {
		case !ok || lit == "-" || !strings.HasPrefix(lit, "-"):
			if g.stopAtOperand {
				out.operands = append(out.operands, args[i:]...)
				return out, true
			}
			out.operands = append(out.operands, args[i])
		case lit == "--":
			out.operands = append(out.operands, args[i+1:]...)
			return out, true
		case strings.HasPrefix(lit, "--"):
			name, _, attached := strings.Cut(lit[2:], "=")
			takesValue, known := g.long[name]
			if !known && !g.anyLong {
				return parsedOptions{}, false
			}
			opt := parsedOption{name: name, word: args[i]}
			switch {
			case attached:
				opt.value = args[i]
			case takesValue && i+1 == len(args):
				return parsedOptions{}, false
			case takesValue:
				i++
				opt.value = args[i]
			}
			out.options = append(out.options, opt)
		default:
			next, ok := g.shortCluster(args, i, lit, &out)
			if !ok {
				return parsedOptions{}, false
			}
			i = next
		}
	}
	return out, true
}

func (g optionGrammar) shortCluster(args []*syntax.Word, i int, lit string, out *parsedOptions) (int, bool) {
	for j := 1; j < len(lit); j++ {
		at := strings.IndexByte(g.short, lit[j])
		if lit[j] == ':' || at < 0 {
			return i, false
		}
		opt := parsedOption{name: lit[j : j+1], word: args[i]}
		takesValue := at+1 < len(g.short) && g.short[at+1] == ':'
		switch {
		case !takesValue:
			out.options = append(out.options, opt)
			continue
		case j+1 < len(lit):
			opt.value = args[i]
		case i+1 == len(args):
			return i, false
		default:
			i++
			opt.value = args[i]
		}
		out.options = append(out.options, opt)
		return i, true
	}
	return i, true
}

func mayExpandToSeveralWords(w *syntax.Word) bool {
	if isGlob(w) || qualifiedGlob(w, true) || hasBraceExpansion(w) {
		return true
	}
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.ParamExp:
			return true
		case *syntax.DblQuoted:
			if slices.ContainsFunc(p.Parts, isSeveralWordsInQuotes) {
				return true
			}
		}
	}
	return false
}

func isSeveralWordsInQuotes(part syntax.WordPart) bool {
	pe, ok := part.(*syntax.ParamExp)
	return ok && (kindOf(pe) == expandsElements || pe.Excl)
}

func solePart(w *syntax.Word) syntax.WordPart {
	if len(w.Parts) != 1 {
		return nil
	}
	if dq, ok := w.Parts[0].(*syntax.DblQuoted); ok && len(dq.Parts) == 1 {
		return dq.Parts[0]
	}
	return w.Parts[0]
}

func soleBareExpansion(w *syntax.Word) (*syntax.ParamExp, bool) {
	pe, ok := solePart(w).(*syntax.ParamExp)
	if !ok || !parser.IsPlainParam(pe) {
		return nil, false
	}
	return pe, true
}

func soleCommandOutput(w *syntax.Word) bool {
	cs, ok := solePart(w).(*syntax.CmdSubst)
	return ok && !cs.TempFile && !cs.ReplyVar && len(cs.Stmts) == 1 && cs.Stmts[0].Cmd != nil
}

func hasBraceExpansion(w *syntax.Word) bool {
	split := &syntax.Word{Parts: w.Parts}
	if !syntax.SplitBraces(split) {
		return false
	}
	return slices.ContainsFunc(split.Parts, func(part syntax.WordPart) bool {
		brace, ok := part.(*syntax.BraceExp)
		return ok && (brace.Sequence || len(brace.Elems) > 1)
	})
}

func (p *pass) effectiveCommandWord(call *syntax.CallExpr) *syntax.Word {
	if i := (parser.Wrappers{Bats: p.bats}).Start(call.Args); i >= 0 {
		return call.Args[i]
	}
	return nil
}

func isGlob(w *syntax.Word) bool { return scanGlob(w, true) }

func hasBracketGlob(w *syntax.Word) bool { return scanGlob(w, false) }

func qualifiedGlob(w *syntax.Word, bare bool) bool {
	return parser.GlobQualifiers(w, bare) < len(w.Parts)
}

func scanGlob(w *syntax.Word, wildcards bool) bool {
	sawOpen := false
	for _, part := range w.Parts[:parser.GlobQualifiers(w, true)] {
		switch p := part.(type) {
		case *syntax.ExtGlob:
			return wildcards
		case *syntax.Lit:
			for i := 0; i < len(p.Value); i++ {
				switch p.Value[i] {
				case '\\':
					i++
				case '*', '?':
					if wildcards {
						return true
					}
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
	if bin, ok := stmt.Cmd.(*syntax.BinaryCmd); ok && pipes(bin.Op) {
		return pipelineStages(bin.Y, pipelineStages(bin.X, into))
	}
	return append(into, stmt)
}

func pipes(op syntax.BinCmdOperator) bool { return op == syntax.Pipe || op == syntax.PipeAll }

func (p *pass) enclosingPipeline() ([]*syntax.Stmt, int) {
	stmt, ok := p.parent().(*syntax.Stmt)
	if !ok {
		return nil, -1
	}
	stages := pipelineStages(p.outermostJoined(stmt, pipes), nil)
	return stages, slices.Index(stages, stmt)
}

func (p *pass) outermostJoined(stmt *syntax.Stmt, joins func(syntax.BinCmdOperator) bool) *syntax.Stmt {
	for depth := 1; ; depth += 2 {
		bin, ok := p.ancestor(depth).(*syntax.BinaryCmd)
		if !ok || !joins(bin.Op) {
			return stmt
		}
		next, ok := p.ancestor(depth + 1).(*syntax.Stmt)
		if !ok {
			return stmt
		}
		stmt = next
	}
}
