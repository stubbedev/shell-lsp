package lint

import (
	"cmp"
	"slices"
	"sort"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const allCodes = "all"

type placement uint8

const (
	placedBeforeCommand placement = iota
	placedAfterCommand
	placedInContinuation
	placedBeforeBranch
	placedBeforeCloser
	placedBeforeCaseBranch
)

func (pl placement) inert() bool { return pl == placedBeforeCloser || pl == placedBeforeCaseBranch }

type directiveComment struct {
	span        parser.Span
	parsed      directive.Directive
	placement   placement
	interrupted parser.Span
}

func (d directiveComment) at(offset int) int { return d.span.Start + offset }

func parseDirectives(tree *parser.Tree) []directiveComment {
	var out []directiveComment
	for _, c := range tree.Comments {
		parsed, ok := directive.Parse("#" + c.Text)
		if !ok {
			continue
		}
		out = append(out, directiveComment{span: c.Span, parsed: parsed, placement: lexicalPlacement(tree, c.Span)})
	}
	slices.SortFunc(out, func(a, b directiveComment) int { return cmp.Compare(a.span.Start, b.span.Start) })
	return out
}

func lexicalPlacement(tree *parser.Tree, comment parser.Span) placement {
	text := tree.Text()
	start := logicalLineStart(tree, comment.Start)
	before := strings.TrimSpace(text[start:comment.Start])
	if before != "" && !opensCommand(tree, before, start+strings.Index(text[start:comment.Start], before)) {
		return placedAfterCommand
	}
	_, pl := followingKeyword(text[nextToken(text, comment.End):])
	return pl
}

func logicalLineStart(tree *parser.Tree, offset int) int {
	text := tree.Text()
	start := lineStart(text, offset)
	for start > 1 && text[start-2] == '\\' && !escaped(text, start-2) && !insideComment(tree, start-2) {
		start = lineStart(text, start-1)
	}
	return start
}

func escaped(text string, offset int) bool {
	backslashes := 0
	for i := offset - 1; i >= 0 && text[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func insideComment(tree *parser.Tree, offset int) bool {
	i := sort.Search(len(tree.Comments), func(i int) bool { return tree.Comments[i].Span.End > offset })
	return i < len(tree.Comments) && tree.Comments[i].Span.Start <= offset
}

var commandOpeningKeywords = setOf("then", "do", "else", "elif", "if", "while", "until")

func opensCommand(tree *parser.Tree, code string, start int) bool {
	last := len(code) - 1
	switch {
	case strings.HasSuffix(code, "&&"), strings.HasSuffix(code, "|&"), strings.HasSuffix(code, "=("):
		return false
	case code[last] == ';' || code[last] == '&' || code[last] == '(':
		return !escaped(code, last)
	}
	field := code[strings.LastIndexAny(code, " \t\n;&|")+1:]
	if field == "{" {
		return true
	}
	return commandOpeningKeywords[field] && keywordAt(tree, field, start+len(code)-len(field))
}

func keywordAt(tree *parser.Tree, name string, offset int) bool {
	i := sort.Search(len(tree.Keywords), func(i int) bool { return tree.Keywords[i].Span.Start >= offset })
	return i < len(tree.Keywords) && tree.Keywords[i].Span.Start == offset && tree.Keywords[i].Name == name
}

func nextToken(text string, from int) int {
	for from < len(text) {
		switch text[from] {
		case ' ', '\t', '\r', '\n':
			from++
		case '#':
			end := strings.IndexByte(text[from:], '\n')
			if end < 0 {
				return len(text)
			}
			from += end
		default:
			return from
		}
	}
	return from
}

var keywordPlacements = []struct {
	keyword   string
	placement placement
}{
	{"then", placedBeforeBranch},
	{"else", placedBeforeBranch},
	{"elif", placedBeforeBranch},
	{"do", placedBeforeBranch},
	{"fi", placedBeforeCloser},
	{"done", placedBeforeCloser},
	{"esac", placedBeforeCloser},
	{"}", placedBeforeCloser},
}

func followingKeyword(rest string) (string, placement) {
	for _, token := range []string{";;", ")"} {
		if strings.HasPrefix(rest, token) {
			return token, placedBeforeCloser
		}
	}
	for _, k := range keywordPlacements {
		if after, ok := strings.CutPrefix(rest, k.keyword); ok && (after == "" || strings.ContainsRune(" \t\r\n;&|()<>", rune(after[0]))) {
			return k.keyword, k.placement
		}
	}
	return "", placedBeforeCommand
}

type codeRange struct {
	from, to Code
}

type suppression struct {
	span   parser.Span
	all    bool
	ranges []codeRange
}

func (s suppression) matches(code Code, at int) bool {
	if at < s.span.Start || at > s.span.End {
		return false
	}
	if s.all {
		return true
	}
	for _, r := range s.ranges {
		if r.from <= code && code < r.to {
			return true
		}
	}
	return false
}

type suppressions struct {
	list     []suppression
	heredocs []parser.Heredoc
}

func (s suppressions) covers(f Finding) bool {
	at := s.anchor(f.Span.Start)
	for _, one := range s.list {
		if one.matches(f.Code, at) {
			return true
		}
	}
	return false
}

func (s suppressions) anchor(offset int) int {
	i := sort.Search(len(s.heredocs), func(i int) bool { return s.heredocs[i].Body.End > offset })
	if i < len(s.heredocs) && s.heredocs[i].Body.Start <= offset {
		return s.heredocs[i].Span.Start
	}
	return offset
}

func outermostHeredocs(all []parser.Heredoc) []parser.Heredoc {
	sorted := slices.SortedFunc(slices.Values(all), func(a, b parser.Heredoc) int { return cmp.Compare(a.Body.Start, b.Body.Start) })
	out := sorted[:0]
	for _, h := range sorted {
		if h.Body.Start == h.Body.End || len(out) > 0 && h.Body.Start < out[len(out)-1].Body.End {
			continue
		}
		out = append(out, h)
	}
	return out
}

func (p *pass) suppressions() suppressions {
	var list []suppression
	for _, d := range p.directives {
		s := suppression{}
		for _, value := range d.parsed.Values(directive.Disable) {
			for item := range strings.SplitSeq(value, ",") {
				if all, r, ok := parseDisabled(item); ok {
					s.all = s.all || all
					s.ranges = append(s.ranges, r)
				}
			}
		}
		if !s.all && len(s.ranges) == 0 {
			continue
		}
		scope, ok := p.directiveScope(d)
		if !ok {
			continue
		}
		s.span = scope
		list = append(list, s)
	}
	if len(list) == 0 {
		return suppressions{}
	}
	return suppressions{list: list, heredocs: outermostHeredocs(p.tree.Heredocs)}
}

func parseDisabled(item string) (bool, codeRange, bool) {
	if item == allCodes {
		return true, codeRange{}, true
	}
	from, to, isRange := strings.Cut(item, "-")
	first, err := ParseCode(from)
	if err != nil {
		return false, codeRange{}, false
	}
	if !isRange {
		return false, codeRange{from: first, to: first + 1}, true
	}
	last, err := ParseCode(to)
	if err != nil {
		return false, codeRange{}, false
	}
	return false, codeRange{from: first, to: last}, true
}

type fileWide struct {
	optional map[string]bool
	extended bool
	shell    bool
}

func fileWideSettings(directives []directiveComment, firstCommand int, opts Options) fileWide {
	out := fileWide{optional: setOf(opts.Optional...), extended: !opts.NoExtendedAnalysis, shell: namesShell(directives)}
	extendedSet := false
	for _, d := range directives {
		if d.span.End > firstCommand {
			continue
		}
		for _, value := range d.parsed.Values(directive.Enable) {
			for name := range strings.SplitSeq(value, ",") {
				out.optional[name] = true
			}
		}
		if value, found := d.parsed.Value(directive.ExtendedAnalysis); found && !extendedSet {
			if extended, ok := parseBool(value); ok {
				out.extended, extendedSet = extended, true
			}
		}
	}
	return out
}

func namesShell(directives []directiveComment) bool {
	return slices.ContainsFunc(directives, func(d directiveComment) bool {
		_, found := d.parsed.Value(directive.Shell)
		return found
	})
}

func parseBool(value string) (bool, bool) {
	switch value {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

func (f fileWide) enabled(name string) bool { return f.optional[name] || f.optional[allCodes] }

func firstCommandStart(tree *parser.Tree) int {
	if len(tree.File.Stmts) > 0 {
		if span, ok := tree.NodeSpan(tree.File.Stmts[0]); ok {
			return span.Start
		}
	}
	return len(tree.Text())
}

func (p *pass) directiveScope(d directiveComment) (parser.Span, bool) {
	switch {
	case d.placement.inert():
		return parser.Span{}, false
	case d.span.End <= p.firstCommand:
		return parser.Span{Start: 0, End: len(p.text)}, true
	case d.placement == placedAfterCommand:
		return parser.Span{Start: logicalLineStart(p.tree, d.span.Start), End: d.span.Start}, true
	case d.placement == placedInContinuation:
		return d.interrupted, true
	}
	i := sort.Search(len(p.stmts), func(i int) bool { return p.stmts[i].Start >= d.span.End })
	if i == len(p.stmts) {
		return parser.Span{}, false
	}
	return p.stmts[i], true
}
