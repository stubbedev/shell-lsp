package lint

import (
	"path"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/getopt"
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

type options = getopt.Parsed[*syntax.Word]

func parseOptions(name string, args []*syntax.Word) options {
	return getopt.Parse(getopt.Commands[path.Base(name)], args, parser.Literal)
}

func (c command) options() options { return parseOptions(c.name, c.args) }

func argumentIs(opt getopt.Option[*syntax.Word], values ...string) bool {
	value, ok := opt.Argument.Text(parser.Literal)
	return ok && slices.Contains(values, value)
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

type resolvedCommand struct {
	word    *syntax.Word
	name    string
	literal bool
	args    []*syntax.Word
}

func resolveAt(words []*syntax.Word, i int) resolvedCommand {
	name, literal := parser.Literal(words[i])
	return resolvedCommand{word: words[i], name: name, literal: literal, args: words[i+1:]}
}

func resolveThrough(wrappers parser.Wrappers, words []*syntax.Word) (resolvedCommand, bool) {
	i := wrappers.Start(words)
	if i < 0 {
		return resolvedCommand{}, false
	}
	return resolveAt(words, i), true
}

type resolvedCall struct {
	call     *syntax.CallExpr
	command  resolvedCommand
	resolved bool
}

func (p *pass) resolved(call *syntax.CallExpr) (resolvedCommand, bool) {
	if p.lastResolved.call != call {
		command, ok := resolveThrough(parser.Wrappers{Bats: p.bats}, call.Args)
		p.lastResolved = resolvedCall{call: call, command: command, resolved: ok}
	}
	return p.lastResolved.command, p.lastResolved.resolved
}

func (p *pass) runs(words []*syntax.Word) (resolvedCommand, bool) {
	command, ok := resolveThrough(p.tree.Runners(), words)
	return command, ok && command.literal
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
