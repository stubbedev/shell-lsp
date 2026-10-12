package lint

import (
	"path"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var loopsFamily = family{
	codes: map[Code]meta{
		2013: {severity: SeverityInfo, bug: true, summary: "A for loop over command output splits it into words, not lines."},
		2041: {severity: SeverityWarning, summary: "A single-quoted loop item is one literal word, not a command whose output is iterated."},
		2042: {severity: SeverityWarning, summary: "Loop items are separated by spaces, not commas."},
		2043: {severity: SeverityWarning, summary: "A loop over a single constant item runs exactly once."},
		2044: {severity: SeverityWarning, summary: "Iterating over find output breaks on file names with spaces or newlines."},
		2045: {severity: SeverityError, summary: "Iterating over ls output breaks on file names with spaces or newlines."},
		2066: {severity: SeverityError, summary: "Double quotes make a loop list a single item, so the loop runs once."},
		2258: {severity: SeverityWarning, summary: "A trailing comma becomes part of a loop item."},
	},
	nodes: []nodeRule{onNode(checkForLoop)},
}

func checkForLoop(p *pass, loop *syntax.ForClause) {
	if loop.Select {
		return
	}
	iter, ok := loop.Loop.(*syntax.WordIter)
	if !ok || !parser.ListsWords(iter) || len(iter.Items) == 0 {
		return
	}
	if len(iter.Items) == 1 {
		p.checkLoopOutput(iter.Items[0])
		if p.checkSingleLoopItem(iter.Items[0]) {
			return
		}
	}
	for _, item := range iter.Items {
		p.checkTrailingComma(item)
	}
}

var lineFilters = []string{"grep", "fgrep", "egrep", "sed", "cat", "awk", "cut", "sort"}

func (p *pass) checkLoopOutput(item *syntax.Word) {
	for _, part := range item.Parts {
		cs, ok := part.(*syntax.CmdSubst)
		if !ok || len(cs.Stmts) != 1 {
			continue
		}
		span, ok := p.span(cs)
		if !ok {
			continue
		}
		stages := pipelineStages(cs.Stmts[0], nil)
		if len(item.Parts) == 1 && len(stages) == 1 {
			p.checkListingLoop(cs, span)
		}
		if allStagesAre(p, stages, lineFilters) {
			p.report(2013, span, "This loop splits the command's output into words, not lines; read lines with: while IFS= read -r line; do ...; done < file")
		}
	}
}

func allStagesAre(p *pass, stages []*syntax.Stmt, names []string) bool {
	for _, stage := range stages {
		call, ok := stage.Cmd.(*syntax.CallExpr)
		if !ok {
			return false
		}
		command, ok := p.resolved(call)
		if !ok || !command.literal || !slices.Contains(names, path.Base(command.name)) {
			return false
		}
	}
	return true
}

func (p *pass) checkListingLoop(cs *syntax.CmdSubst, span parser.Span) {
	call, ok := cs.Stmts[0].Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return
	}
	switch roughText(call.Args[0]) {
	case "ls":
		if hasDashArgument(call.Args[1:]) {
			p.reportWithSeverity(2045, SeverityWarning, span, "Iterating over ls output breaks on file names with spaces or newlines; loop over a glob such as for f in ./* instead.")
			return
		}
		p.report(2045, span, "Iterating over ls output breaks on file names with spaces or newlines; loop over a glob such as for f in *.txt instead.")
	case "find":
		p.report(2044, span, "Iterating over find output breaks on file names with spaces or newlines; use find -exec, or a while read loop over find -print0.")
	}
}

func hasDashArgument(args []*syntax.Word) bool {
	for _, a := range args {
		if strings.HasPrefix(roughText(a), "-") {
			return true
		}
	}
	return false
}

func (p *pass) checkSingleLoopItem(item *syntax.Word) bool {
	span, ok := p.span(item)
	if !ok {
		return false
	}
	var part syntax.WordPart
	if len(item.Parts) == 1 {
		part = item.Parts[0]
	}
	switch part := part.(type) {
	case *syntax.DblQuoted:
		if (expandsSplittably(part.Parts) || strings.Contains(literalText(item), "*")) && !mayExpandToSeveralWords(item) {
			p.report(2066, span, "Double quotes make this list a single item, so the loop runs exactly once; drop the quotes or iterate over an array.")
			return true
		}
	case *syntax.SglQuoted:
		if !part.Dollar {
			p.report(2041, span, "This single-quoted text is one literal item, not a command; use $(...) to iterate over a command's output.")
			return true
		}
	case *syntax.Lit:
		if strings.Contains(part.Value, ",") && !isPattern(item) {
			p.report(2042, span, "Loop items are separated by spaces, not commas; write them as separate words or use a brace expansion {a,b}.")
			return true
		}
	}
	if mayExpandToSeveralWords(item) || expandsSplittably(item.Parts) {
		return false
	}
	p.report(2043, span, "This loop has a single constant item, so it runs exactly once; did you mean to expand a variable or a glob?")
	return true
}

func expandsSplittably(parts []syntax.WordPart) bool {
	for _, part := range parts {
		switch part.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst:
			return true
		}
	}
	return false
}

func (p *pass) checkTrailingComma(item *syntax.Word) {
	last, ok := item.Parts[len(item.Parts)-1].(*syntax.Lit)
	if !ok || !strings.HasSuffix(last.Value, ",") {
		return
	}
	span, ok := p.span(item)
	lit, litOK := p.span(last)
	if !ok || !litOK {
		return
	}
	fix := reviewFix("Remove the trailing comma", textedit.Edit{Start: lit.End - 1, End: lit.End})
	p.report(2258, span, "The trailing comma becomes part of this loop item; separate items with spaces only.", fix)
}
