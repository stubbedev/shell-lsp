package lint

import (
	"path"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var redirectSafetyFamily = family{
	codes: map[Code]meta{
		2069: {severity: SeverityWarning, summary: "2>&1 placed before a file redirection still points stderr at the old target."},
		2094: {severity: SeverityInfo, bug: true, summary: "A pipeline reads and writes the same file, which the output redirection truncates first."},
	},
	rules: []rule{
		{on: nodes(&syntax.Stmt{}), node: checkRedirectOrder},
		{on: nodes(&syntax.Stmt{}), node: checkSameFileInPipeline},
	},
}

func checkRedirectOrder(p *pass, n syntax.Node) {
	stmt, ok := n.(*syntax.Stmt)
	if !ok || len(stmt.Redirs) != 2 {
		return
	}
	dup, file := stmt.Redirs[0], stmt.Redirs[1]
	if dup.Op != syntax.DplOut || dup.N == nil || dup.N.Value != "2" || dup.Word.Lit() != "1" {
		return
	}
	if (file.Op != syntax.RdrOut && file.Op != syntax.AppOut) || p.outputCaptured(stmt) {
		return
	}
	span, ok := p.span(dup)
	target, targetOK := p.span(file)
	if !ok || !targetOK {
		return
	}
	p.report(2069, span, "Redirections apply left to right, so 2>&1 here still points stderr at the terminal; put it after the file redirection: > file 2>&1.",
		reviewFix("Move 2>&1 after the file redirection",
			textedit.Edit{Start: span.Start, End: span.End, NewText: p.text[target.Start:target.End]},
			textedit.Edit{Start: target.Start, End: target.End, NewText: p.text[span.Start:span.End]}))
}

func (p *pass) outputCaptured(stmt *syntax.Stmt) bool {
	var child syntax.Node = stmt
	for depth := 0; ; depth++ {
		switch a := p.ancestor(depth).(type) {
		case nil:
			return false
		case *syntax.CmdSubst, *syntax.ProcSubst:
			return true
		case *syntax.BinaryCmd:
			if pipes(a.Op) && a.X == child {
				return true
			}
		}
		child = p.ancestor(depth)
	}
}

type fileReference struct {
	word     *syntax.Word
	text     string
	redirect *syntax.Redirect
	command  syntax.Node
}

func readsOrWritesFile(r *syntax.Redirect) bool {
	return r.Op == syntax.RdrIn || r.Op == syntax.RdrOut || r.Op == syntax.AppOut
}

func checkSameFileInPipeline(p *pass, n syntax.Node) {
	top, ok := p.pipelineTop(n)
	if !ok || (!isPipe(top) && !slices.ContainsFunc(top.Redirs, readsOrWritesFile)) {
		return
	}
	stages := pipelineStages(top, nil)
	hasTarget := false
	for _, s := range stages {
		for _, r := range s.Redirs {
			hasTarget = hasTarget || readsOrWritesFile(r)
		}
	}
	if !hasTarget {
		return
	}
	var refs []fileReference
	for _, s := range stages {
		refs = p.collectFileReferences(s, refs)
	}
	for _, target := range refs {
		if target.redirect == nil || !readsOrWritesFile(target.redirect) || !isStageRedirect(stages, target.redirect) {
			continue
		}
		if strings.HasPrefix(roughText(target.word), "/dev/") || isHarmlessCommand(target.command) {
			continue
		}
		for _, other := range refs {
			if other.word == target.word || other.text != target.text || sameDirection(target, other) {
				continue
			}
			if isHarmlessCommand(other.command) || isAssignmentOnly(other.command) {
				continue
			}
			p.reportSameFile(target.word)
			p.reportSameFile(other.word)
		}
	}
}

func isStageRedirect(stages []*syntax.Stmt, r *syntax.Redirect) bool {
	return slices.ContainsFunc(stages, func(s *syntax.Stmt) bool { return slices.Contains(s.Redirs, r) })
}

func (p *pass) reportSameFile(w *syntax.Word) {
	p.reportNode(2094, w, "This pipeline reads and writes the same file; the output redirection truncates it before it is read. Write to a temporary file and move it into place.")
}

func sameDirection(a, b fileReference) bool {
	if b.redirect == nil || !readsOrWritesFile(b.redirect) {
		return false
	}
	return (a.redirect.Op == syntax.RdrIn) == (b.redirect.Op == syntax.RdrIn)
}

func (p *pass) collectFileReferences(stage *syntax.Stmt, refs []fileReference) []fileReference {
	var stack []syntax.Node
	parser.Walk(stage, func(n syntax.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if w, ok := n.(*syntax.Word); ok {
			if span, ok := p.span(w); ok {
				ref := fileReference{word: w, text: p.text[span.Start:span.End], command: nearestCommand(stack)}
				if r, ok := stack[len(stack)-1].(*syntax.Redirect); ok && r.Word == w {
					ref.redirect = r
				}
				refs = append(refs, ref)
			}
		}
		stack = append(stack, n)
		return true
	})
	return refs
}

func nearestCommand(stack []syntax.Node) syntax.Node {
	for _, n := range slices.Backward(stack) {
		switch n := n.(type) {
		case *syntax.CallExpr, *syntax.DeclClause:
			return n
		case *syntax.Stmt:
			return n.Cmd
		}
	}
	return nil
}

var harmlessReaders = setOf("echo", "mapfile", "printf", "sponge")

func isHarmlessCommand(n syntax.Node) bool {
	call, ok := n.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return false
	}
	name, ok := parser.Literal(call.Args[0])
	return ok && harmlessReaders[path.Base(name)]
}

func isAssignmentOnly(n syntax.Node) bool {
	call, ok := n.(*syntax.CallExpr)
	return ok && len(call.Args) == 0
}
