package lint

import (
	"sort"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const allCodes = "all"

type codeRange struct {
	from, to Code
}

type suppression struct {
	span   parser.Span
	all    bool
	ranges []codeRange
}

func (s suppression) matches(f Finding) bool {
	if f.Span.Start < s.span.Start || f.Span.Start > s.span.End {
		return false
	}
	if s.all {
		return true
	}
	for _, r := range s.ranges {
		if r.from <= f.Code && f.Code < r.to {
			return true
		}
	}
	return false
}

type suppressions []suppression

func (s suppressions) covers(f Finding) bool {
	for _, one := range s {
		if one.matches(f) {
			return true
		}
	}
	return false
}

func directivesOf(p *pass) suppressions {
	var out suppressions
	for _, c := range p.tree.Comments {
		d, ok := directive.Parse("#" + c.Text)
		if !ok {
			continue
		}
		s := suppression{}
		for _, value := range d.Values("disable") {
			for item := range strings.SplitSeq(strings.Trim(value, `"'`), ",") {
				s.add(item)
			}
		}
		if !s.all && len(s.ranges) == 0 {
			continue
		}
		scope, ok := p.directiveScope(c.Span)
		if !ok {
			continue
		}
		s.span = scope
		out = append(out, s)
	}
	return out
}

func (s *suppression) add(item string) {
	item = strings.TrimSpace(item)
	if strings.EqualFold(item, allCodes) {
		s.all = true
		return
	}
	from, to, isRange := strings.Cut(item, "-")
	first, err := ParseCode(from)
	if err != nil {
		return
	}
	last := first + 1
	if isRange {
		if last, err = ParseCode(to); err != nil {
			return
		}
	}
	s.ranges = append(s.ranges, codeRange{from: first, to: last})
}

type fileWide struct {
	optional map[string]bool
	extended bool
	shell    bool
}

func fileWideDirectives(tree *parser.Tree, enabled []string) fileWide {
	firstCommand := len(tree.Text())
	if len(tree.File.Stmts) > 0 {
		if span, ok := tree.NodeSpan(tree.File.Stmts[0]); ok {
			firstCommand = span.Start
		}
	}
	out := fileWide{optional: setOf(enabled...), extended: true}
	for _, c := range tree.Comments {
		d, ok := directive.Parse("#" + c.Text)
		if !ok {
			continue
		}
		if _, found := d.Value("shell"); found {
			out.shell = true
		}
		if c.Span.End > firstCommand {
			continue
		}
		for _, value := range d.Values("enable") {
			for name := range strings.SplitSeq(strings.Trim(value, `"'`), ",") {
				out.optional[strings.TrimSpace(name)] = true
			}
		}
		if value, found := d.Value("extended-analysis"); found {
			out.extended = strings.Trim(value, `"'`) != "false"
		}
	}
	return out
}

func (f fileWide) enabled(name string) bool { return f.optional[name] || f.optional[allCodes] }

func (p *pass) directiveScope(comment parser.Span) (parser.Span, bool) {
	if len(p.stmts) == 0 || comment.End <= p.stmts[0].Start {
		return parser.Span{Start: 0, End: len(p.text)}, true
	}
	lineStart := strings.LastIndexByte(p.text[:comment.Start], '\n') + 1
	if trailing := strings.TrimSpace(p.text[lineStart:comment.Start]); trailing != "" {
		return parser.Span{Start: lineStart, End: comment.Start}, true
	}
	i := sort.Search(len(p.stmts), func(i int) bool { return p.stmts[i].Start >= comment.End })
	if i == len(p.stmts) {
		return parser.Span{}, false
	}
	return p.stmts[i], true
}
