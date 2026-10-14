package glob

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelloptions"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type Role uint8

const (
	RoleFilenames Role = iota
	RoleTest
	RoleCase
	RoleRemoveShortestPrefix
	RoleRemoveLongestPrefix
	RoleRemoveShortestSuffix
	RoleRemoveLongestSuffix
	RoleReplaceFirst
	RoleReplaceAll
	RoleReplacePrefix
	RoleReplaceSuffix
	RoleReplaceWhole
	RoleFilter
	RoleCaseConversion
	RoleSubscript
	RoleRegex
)

type Site struct {
	Word    *syntax.Word
	Span    parser.Span
	Role    Role
	Exp     *syntax.ParamExp
	Subject *syntax.Word
	Negated bool
	Pattern Pattern
	Options shelloptions.Options
}

type Sites struct {
	tree    *parser.Tree
	options shelloptions.PerShell
}

func For(tree *parser.Tree, options shelloptions.PerShell) Sites {
	return Sites{tree: tree, options: options}
}

func (s Sites) All() []Site {
	var sites []Site
	parser.Walk(s.tree.File, func(node syntax.Node) bool {
		sites = s.appendAt(sites, node, false)
		return true
	})
	return sites
}

func (s Sites) At(offset int) (Site, bool) {
	var found []Site
	for _, located := range s.tree.PathAt(offset) {
		found = s.appendAt(found, located.Node, true)
	}
	found = slices.DeleteFunc(found, func(site Site) bool { return !site.Span.Touches(offset) })
	if len(found) == 0 {
		return Site{}, false
	}
	return slices.MinFunc(found, func(a, b Site) int { return (a.Span.End - a.Span.Start) - (b.Span.End - b.Span.Start) }), true
}

func (s Sites) Expansion(exp *syntax.ParamExp) []Site {
	return s.appendAt(nil, exp, true)
}

func (s Sites) appendAt(sites []Site, node syntax.Node, literal bool) []Site {
	add := func(w *syntax.Word, role Role, exp *syntax.ParamExp) {
		if site, ok := s.site(w, role, exp, literal); ok {
			sites = append(sites, site)
		}
	}
	addMatch := func(w, subject *syntax.Word, role Role, negated bool) {
		if site, ok := s.site(w, role, nil, literal); ok {
			site.Subject, site.Negated = subject, negated
			sites = append(sites, site)
		}
	}
	switch n := node.(type) {
	case *syntax.ParamExp:
		if w, role, ok := expansionPattern(n); ok {
			add(w, role, n)
		}
		if fa, ok := n.Index.(*syntax.FlagsArithm); ok && parser.SubscriptPattern(fa) {
			if w, ok := fa.X.(*syntax.Word); ok {
				add(w, RoleSubscript, n)
			}
		}
	case *syntax.BinaryTest:
		switch n.Op {
		case syntax.TsMatch, syntax.TsMatchShort, syntax.TsNoMatch:
			if w, ok := n.Y.(*syntax.Word); ok {
				subject, _ := n.X.(*syntax.Word)
				addMatch(w, subject, RoleTest, n.Op == syntax.TsNoMatch)
			}
		case syntax.TsReMatch:
			if w, ok := n.Y.(*syntax.Word); ok {
				subject, _ := n.X.(*syntax.Word)
				addMatch(w, subject, RoleRegex, false)
			}
		}
	case *syntax.CaseClause:
		for _, item := range n.Items {
			for _, w := range item.Patterns {
				addMatch(w, n.Word, RoleCase, false)
			}
		}
	case *syntax.CallExpr:
		if name, ok := parser.CallName(n); ok && name == "noglob" && s.tree.Dialect == dialect.Zsh {
			return sites
		}
		for _, w := range n.Args {
			add(w, RoleFilenames, nil)
		}
	case *syntax.WordIter:
		for _, w := range n.Items {
			add(w, RoleFilenames, nil)
		}
	case *syntax.ArrayExpr:
		for _, elem := range n.Elems {
			if elem.Value != nil {
				add(elem.Value, RoleFilenames, nil)
			}
		}
	}
	return sites
}

func expansionPattern(n *syntax.ParamExp) (*syntax.Word, Role, bool) {
	if n.Repl != nil {
		role := RoleReplaceFirst
		switch {
		case n.Repl.Whole:
			role = RoleReplaceWhole
		case n.Repl.All:
			role = RoleReplaceAll
		}
		return n.Repl.Orig, role, n.Repl.Orig != nil
	}
	if n.Exp == nil || n.Exp.Word == nil {
		return nil, 0, false
	}
	role, ok := OperatorRole(n.Exp.Op)
	return n.Exp.Word, role, ok
}

var operatorRoles = map[syntax.ParExpOperator]Role{
	syntax.RemSmallPrefix: RoleRemoveShortestPrefix, syntax.RemLargePrefix: RoleRemoveLongestPrefix,
	syntax.RemSmallSuffix: RoleRemoveShortestSuffix, syntax.RemLargeSuffix: RoleRemoveLongestSuffix,
	syntax.MatchEmpty: RoleFilter,
	syntax.UpperFirst: RoleCaseConversion, syntax.UpperAll: RoleCaseConversion,
	syntax.LowerFirst: RoleCaseConversion, syntax.LowerAll: RoleCaseConversion,
}

func OperatorRole(op syntax.ParExpOperator) (Role, bool) {
	role, ok := operatorRoles[op]
	return role, ok
}

func (s Sites) syntax(role Role, opts shelloptions.Options) Syntax {
	return Syntax{Dialect: s.tree.Dialect, Filenames: role == RoleFilenames, KshGlob: opts.KshGlob}
}

type Resolver func(part syntax.WordPart) (string, bool)

const patternBytes = "*?[]<>()|^~#@!+"

func mayGlob(w *syntax.Word) bool {
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.ExtGlob:
			return true
		case *syntax.Lit:
			if strings.ContainsAny(p.Value, patternBytes) {
				return true
			}
		}
	}
	return false
}

func (s Sites) site(w *syntax.Word, role Role, exp *syntax.ParamExp, literal bool) (Site, bool) {
	if (role == RoleFilenames || !literal) && role != RoleRegex && !mayGlob(w) {
		return Site{}, false
	}
	span, ok := s.tree.NodeSpan(w)
	if !ok {
		return Site{}, false
	}
	role, pattern, ok := s.Pattern(w, role, nil)
	if !ok || role == RoleFilenames && !pattern.Globs() || role == RoleRegex && len(w.Parts) == 0 {
		return Site{}, false
	}
	return Site{Word: w, Span: span, Role: role, Exp: exp, Pattern: pattern, Options: s.options.At(span.Start)}, true
}

func (s Sites) Pattern(w *syntax.Word, role Role, resolve Resolver) (Role, Pattern, bool) {
	if role == RoleRegex {
		segments, ok := s.regexSegments(w, resolve)
		return role, ParseRegex(segments, Syntax{Dialect: s.tree.Dialect, Regex: ExtendedRegex}), ok
	}
	opts := s.options.At(parser.Offset(w.Pos()))
	segments, ok := s.segments(w, role, resolve, opts)
	if !ok {
		return role, Pattern{}, false
	}
	role, segments = anchored(role, segments)
	pattern := Parse(segments, s.syntax(role, opts))
	if role == RoleTest && s.tree.Dialect == dialect.Bash {
		pattern.Needs = slices.DeleteFunc(pattern.Needs, func(n Need) bool { return n.Option == ExtGlob })
	}
	return role, pattern, true
}

func anchored(role Role, segments []Segment) (Role, []Segment) {
	if role != RoleReplaceFirst || len(segments) == 0 || segments[0].Kind != SegmentPattern {
		return role, segments
	}
	first := segments[0]
	switch {
	case strings.HasPrefix(first.Text, "#"):
		role = RoleReplacePrefix
	case strings.HasPrefix(first.Text, "%"):
		role = RoleReplaceSuffix
	default:
		return role, segments
	}
	first.Text = first.Text[1:]
	first.Span.Start++
	return role, append([]Segment{first}, segments[1:]...)
}

func (s Sites) globSubst(pe *syntax.ParamExp, opts shelloptions.Options) bool {
	if s.tree.Dialect.Features().GlobsUnquoted {
		return true
	}
	if pe == nil {
		return opts.GlobSubst
	}
	return pe.GlobSubst == syntax.OptOn || pe.GlobSubst == syntax.OptUnset && opts.GlobSubst
}

func expanded(part syntax.WordPart, span parser.Span, src string, glob bool, resolve Resolver) Segment {
	if resolve != nil {
		if value, ok := resolve(part); ok {
			if glob {
				return Segment{Span: span, Kind: SegmentText, Text: value}
			}
			return Segment{Span: span, Kind: SegmentLiteral, Text: value}
		}
	}
	return Segment{Span: span, Kind: SegmentUnknown, Text: src, Glob: glob}
}

func (s Sites) segments(w *syntax.Word, role Role, resolve Resolver, opts shelloptions.Options) ([]Segment, bool) {
	text := s.tree.Text()
	parts := w.Parts
	var qualifiers []syntax.WordPart
	if role == RoleFilenames && s.tree.Dialect == dialect.Zsh {
		cut := parser.GlobQualifiers(w, !opts.NoBareGlobQual)
		parts, qualifiers = w.Parts[:cut], w.Parts[cut:]
	}
	segments := make([]Segment, 0, len(w.Parts))
	source := func(n syntax.Node) (parser.Span, string, bool) {
		span, ok := s.tree.NodeSpan(n)
		if !ok {
			return parser.Span{}, "", false
		}
		return span, text[span.Start:span.End], true
	}
	for _, part := range parts {
		span, src, ok := source(part)
		if !ok {
			return nil, false
		}
		switch p := part.(type) {
		case *syntax.Lit:
			if s.tree.Unexpandable(p) {
				segments = append(segments, Segment{Span: span, Kind: SegmentLiteral, Text: src})
				continue
			}
			segments = append(segments, Segment{Span: span, Kind: SegmentPattern, Text: src})
		case *syntax.ExtGlob:
			segments = append(segments, Segment{Span: span, Kind: SegmentPattern, Text: src})
		case *syntax.SglQuoted:
			value, _ := parser.Text(&syntax.Word{Parts: []syntax.WordPart{p}}, "")
			segments = append(segments, Segment{Span: span, Kind: SegmentLiteral, Text: value})
		case *syntax.DblQuoted:
			for _, inner := range p.Parts {
				innerSpan, innerSrc, ok := source(inner)
				if !ok {
					return nil, false
				}
				if lit, ok := inner.(*syntax.Lit); ok {
					segments = append(segments, Segment{Span: innerSpan, Kind: SegmentLiteral, Text: parser.UnescapeLit(lit.Value, true)})
					continue
				}
				segments = append(segments, expanded(inner, innerSpan, innerSrc, false, resolve))
			}
		case *syntax.ParamExp:
			segments = append(segments, expanded(p, span, src, s.globSubst(p, opts), resolve))
		default:
			segments = append(segments, expanded(p, span, src, s.globSubst(nil, opts), resolve))
		}
	}
	for _, q := range qualifiers {
		span, src, ok := source(q)
		if !ok {
			return nil, false
		}
		segments = append(segments, Segment{Span: span, Kind: SegmentQualifiers, Text: src})
	}
	return segments, true
}

func (s Sites) regexSegments(w *syntax.Word, resolve Resolver) ([]Segment, bool) {
	literalQuotes := s.tree.Dialect.Features().QuotedRegexLiteral
	quoted := SegmentText
	if literalQuotes {
		quoted = SegmentLiteral
	}
	var segments []Segment
	addLit := func(lit *syntax.Lit, inQuotes bool) bool {
		span, ok := s.tree.NodeSpan(lit)
		if !ok {
			return false
		}
		if !literalQuotes {
			segments = append(segments, Segment{Span: span, Kind: SegmentText, Text: parser.UnescapeLit(lit.Value, inQuotes)})
			return true
		}
		value := lit.Value
		for i := 0; i < len(value); i++ {
			at := parser.Span{Start: span.Start + i, End: span.Start + i + 1}
			escapes := i+1 < len(value) && value[i] == '\\' && (!inQuotes || parser.IsDoubleQuoteEscapable(value[i+1]))
			switch {
			case escapes:
				segments = append(segments, Segment{Span: parser.Span{Start: at.Start, End: at.End + 1}, Kind: SegmentLiteral, Text: value[i+1 : i+2]})
				i++
			case inQuotes:
				segments = append(segments, Segment{Span: at, Kind: SegmentLiteral, Text: value[i : i+1]})
			default:
				segments = append(segments, Segment{Span: at, Kind: SegmentText, Text: value[i : i+1]})
			}
		}
		return true
	}
	for _, part := range w.Parts {
		span, ok := s.tree.NodeSpan(part)
		if !ok {
			return nil, false
		}
		src := s.tree.Text()[span.Start:span.End]
		switch p := part.(type) {
		case *syntax.Lit:
			if !addLit(p, false) {
				return nil, false
			}
		case *syntax.SglQuoted:
			value, _ := parser.Text(&syntax.Word{Parts: []syntax.WordPart{p}}, "")
			segments = append(segments, Segment{Span: span, Kind: quoted, Text: value})
		case *syntax.DblQuoted:
			for _, inner := range p.Parts {
				if lit, ok := inner.(*syntax.Lit); ok {
					if !addLit(lit, true) {
						return nil, false
					}
					continue
				}
				innerSpan, ok := s.tree.NodeSpan(inner)
				if !ok {
					return nil, false
				}
				segments = append(segments, expanded(inner, innerSpan, s.tree.Text()[innerSpan.Start:innerSpan.End], !literalQuotes, resolve))
			}
		default:
			segments = append(segments, expanded(p, span, src, true, resolve))
		}
	}
	return segments, true
}

func (p Pattern) Globs() bool {
	globs := false
	Walk(p.Root, func(n Node) bool {
		switch n.(type) {
		case *Seq, *Literal, *Unknown:
			return true
		}
		globs = true
		return false
	})
	return globs
}
