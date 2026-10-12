package lint

import (
	"fmt"
	"sort"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var directivesFamily = family{
	codes: map[Code]meta{
		1103: {severity: SeverityError, summary: "A shell directive names a shell this linter does not know."},
		1107: {severity: SeverityWarning, summary: "A directive uses a key that does not exist."},
		1123: {severity: SeverityError, summary: "A directive stands in front of a keyword where it has no command to apply to."},
		1124: {severity: SeverityError, summary: "A directive in front of a case branch pattern has no effect."},
		1125: {severity: SeverityError, summary: "A disable list contains something that is not a check code or range."},
		1126: {severity: SeverityError, summary: "A directive placed after a command's words or inside a continued list is rejected by ShellCheck."},
		1144: {severity: SeverityError, summary: "external-sources=true only takes effect in a .shellcheckrc file."},
		1145: {severity: SeverityError, summary: "external-sources is set to something other than true or false."},
		1146: {severity: SeverityError, summary: "extended-analysis is set to something other than true or false."},
	},
	rules: []rule{
		{on: nodes(&syntax.CaseClause{}, &syntax.BinaryCmd{}), node: placeDirectivesInGaps},
		{finish: checkDirectives},
	},
}

func placeDirectivesInGaps(p *pass, n syntax.Node) {
	if len(p.directives) == 0 {
		return
	}
	switch n := n.(type) {
	case *syntax.CaseClause:
		start := parser.Offset(n.In) + len("in")
		if n.Braces {
			start = parser.Offset(n.In) + len("{")
		}
		for _, item := range n.Items {
			p.placeBeforeCaseBranch(start, parser.Offset(item.Pos()))
			if !item.OpPos.IsValid() {
				return
			}
			start = parser.Offset(item.End())
		}
		p.placeBeforeCaseBranch(start, parser.Offset(n.Esac))
	case *syntax.BinaryCmd:
		if stmt, ok := p.parent().(*syntax.Stmt); ok {
			p.placeInContinuation(stmt, parser.Offset(n.OpPos), parser.Offset(n.Y.Pos()))
		}
	}
}

func (p *pass) directivesBetween(start, end int) []directiveComment {
	from := sort.Search(len(p.directives), func(i int) bool { return p.directives[i].span.Start >= start })
	to := sort.Search(len(p.directives), func(i int) bool { return p.directives[i].span.Start >= end })
	return p.directives[from:max(from, to)]
}

func (p *pass) placeBeforeCaseBranch(start, end int) {
	gap := p.directivesBetween(start, end)
	for i := range gap {
		gap[i].placement = placedBeforeCaseBranch
	}
}

func (p *pass) placeInContinuation(stmt *syntax.Stmt, start, end int) {
	gap := p.directivesBetween(start, end)
	if len(gap) == 0 {
		return
	}
	list, ok := p.span(p.outermostJoined(stmt, anyOperator))
	if !ok {
		return
	}
	for i := range gap {
		if gap[i].placement != placedAfterCommand {
			gap[i].placement, gap[i].interrupted = placedInContinuation, list
		}
	}
}

func anyOperator(syntax.BinCmdOperator) bool { return true }

func checkDirectives(p *pass) {
	for i, d := range p.directives {
		for _, pair := range d.parsed.Pairs {
			p.checkDirectivePair(d, pair)
		}
		if problem := d.parsed.Problem; problem != nil {
			p.report(1125, parser.Span{Start: d.at(problem.Offset), End: d.span.End}, malformedDirectiveMessage(problem.Kind))
		}
		p.checkPlacement(d, i > 0 && p.directives[i-1].placement == d.placement && sameRun(p.text, p.directives[i-1], d))
	}
}

func malformedDirectiveMessage(kind directive.ProblemKind) string {
	switch kind {
	case directive.MissingEquals:
		return "This word has no =value, so the rest of the directive is ignored; write key=value pairs and start any explanation with #."
	case directive.MalformedQuote:
		return "This quoted directive value is empty or never closed, so the rest of the directive is ignored."
	}
	return "This text after the directive's key=value pairs is not understood and is ignored; start a trailing explanation with #."
}

func (d directiveComment) valueSpan(pair directive.Pair) parser.Span {
	return parser.Span{Start: d.at(pair.ValueOffset), End: d.at(pair.ValueEnd)}
}

func (p *pass) checkDirectivePair(d directiveComment, pair directive.Pair) {
	value := d.valueSpan(pair)
	key, _ := pair.Key()
	switch key {
	case directive.Disable:
		p.checkDisabledCodes(value, pair.Value)
	case directive.Shell:
		if _, ok := dialect.FromName(pair.Value); !ok {
			p.report(1103, value, fmt.Sprintf("%q is not a shell this linter knows, so the directive is ignored; use sh, bash, dash, ksh, zsh or busybox.", pair.Value))
		}
	case directive.ExternalSources:
		switch pair.Value {
		case "true":
			p.report(1144, value, "external-sources=true is only honoured in a .shellcheckrc file; in a script it is ignored.")
		case "false":
		default:
			p.report(1145, value, fmt.Sprintf("external-sources must be true or false, not %q; the setting is ignored.", pair.Value))
		}
	case directive.ExtendedAnalysis:
		if _, ok := parseBool(pair.Value); !ok {
			p.report(1146, value, fmt.Sprintf("extended-analysis must be true or false, not %q; the setting is ignored.", pair.Value))
		}
	default:
		if !pair.Known() {
			span := parser.Span{Start: d.at(pair.KeyOffset), End: d.at(pair.KeyOffset) + len(pair.Name)}
			p.report(1107, span, fmt.Sprintf("%q is not a directive key, so it is ignored; known keys are disable, enable, source, source-path, shell, external-sources and extended-analysis.", pair.Name))
		}
	}
}

func (p *pass) checkDisabledCodes(span parser.Span, value string) {
	if value == "" {
		return
	}
	for item := range strings.SplitSeq(value, ",") {
		if _, err := parseCodeItem(item); err != nil {
			p.report(1125, span, fmt.Sprintf("%q is not a check code or range, so this disable list is ignored from here on; write codes like SC2086 or ranges like SC2000-SC2099.", item))
			return
		}
	}
}

func sameRun(text string, previous, current directiveComment) bool {
	return nextToken(text, previous.span.End) == nextToken(text, current.span.End)
}

func (p *pass) checkPlacement(d directiveComment, continuesRun bool) {
	switch d.placement {
	case placedAfterCommand:
		p.report(1126, d.span, "Put this directive on its own line before the command; ShellCheck rejects directives that follow a command's words. Here it only applies to its own line.")
	case placedInContinuation:
		p.report(1126, d.span, "This directive interrupts a pipeline or &&/|| list that continues on the next line, which ShellCheck rejects; put it before the whole list. Here it applies to the whole list.")
	case placedBeforeCaseBranch:
		p.report(1124, d.span, "A directive cannot stand in front of a case branch pattern and has no effect here; put it before the case statement or before a command inside the branch.")
	case placedBeforeBranch, placedBeforeCloser:
		if continuesRun {
			return
		}
		keyword, _ := followingKeyword(p.text[nextToken(p.text, d.span.End):])
		p.report(1123, d.span, misplacedBeforeKeywordMessage(keyword, d.placement))
	}
}

func misplacedBeforeKeywordMessage(keyword string, pl placement) string {
	if pl == placedBeforeBranch {
		return fmt.Sprintf("ShellCheck rejects a directive in front of %q; move it after %q, before the command it is meant for. Here it applies to the first command of that branch.", keyword, keyword)
	}
	return fmt.Sprintf("A directive in front of %q has no command to apply to, so it is ignored (and ShellCheck rejects it); put it before the command it is meant for.", keyword)
}
