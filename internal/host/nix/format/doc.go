package format

import (
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	lineWidth   = 100
	indentWidth = 2
)

type itemTag uint8

const (
	tagText itemTag = iota
	tagSpacing
	tagGroup
)

type textKind uint8

const (
	textRegular textKind = iota
	textTrailing
	textComment
	textTrailingComment
)

type spacing uint8

const (
	spaceHard spacing = iota
	spaceSoft
	breakSoft
	breakPlain
	spacePlain
	lineHard
	lineEmpty
	lineCount
)

type groupKind uint8

const (
	groupRegular groupKind = iota
	groupPriority
	groupTransparent
)

type item struct {
	tag    itemTag
	nest   int
	offset int
	kind   textKind
	text   string
	space  spacing
	count  int
	group  groupKind
	items  []item
	hard   bool
	width  int
}

var (
	hardSpace = []item{{tag: tagSpacing, space: spaceHard}}
	softSpace = []item{{tag: tagSpacing, space: spaceSoft}}
	softBreak = []item{{tag: tagSpacing, space: breakSoft}}
	plainBrk  = []item{{tag: tagSpacing, space: breakPlain}}
	space     = []item{{tag: tagSpacing, space: spacePlain}}
	hardLine  = []item{{tag: tagSpacing, space: lineHard}}
	emptyLine = []item{{tag: tagSpacing, space: lineEmpty}}
)

func lines(count int) []item {
	return []item{{tag: tagSpacing, space: lineCount, count: count}}
}

func text(nest int, s string) []item {
	return []item{{tag: tagText, nest: nest, kind: textRegular, text: s}}
}

func textOf(nest int, s string, kind textKind) []item {
	return []item{{tag: tagText, nest: nest, kind: kind, text: s}}
}

func seq(parts ...[]item) []item {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]item, 0, n)
	for _, p := range parts {
		for _, it := range p {
			if last := len(out) - 1; it.tag == tagText && it.kind == textRegular && last >= 0 && out[last].tag == tagText && out[last].kind == textRegular {
				out[last].text += it.text
				continue
			}
			out = append(out, it)
		}
	}
	return out
}

func grouped(kind groupKind, parts ...[]item) []item {
	items := seq(parts...)
	return []item{measured(item{tag: tagGroup, group: kind, items: items})}
}

func group(parts ...[]item) []item { return grouped(groupRegular, parts...) }

func priority(parts ...[]item) []item { return grouped(groupPriority, parts...) }

func transparent(parts ...[]item) []item { return grouped(groupTransparent, parts...) }

func measured(g item) item {
	g.hard, g.width = false, 0
	for _, it := range g.items {
		switch it.tag {
		case tagText:
			if it.kind == textRegular || it.kind == textComment {
				g.width += utf8.RuneCountInString(it.text)
			}
		case tagSpacing:
			switch it.space {
			case lineHard, lineEmpty, lineCount:
				g.hard = true
			case spacePlain, spaceHard, spaceSoft:
				g.width++
			case breakSoft, breakPlain:
			}
		case tagGroup:
			g.hard = g.hard || it.hard
			g.width += it.width
		}
	}
	return g
}

func appended(doc []item, extra []item) []item {
	if len(doc) == 0 || doc[len(doc)-1].tag != tagGroup {
		return seq(doc, extra)
	}
	out := slices.Clone(doc)
	g := out[len(out)-1]
	g.items = seq(g.items, extra)
	out[len(out)-1] = measured(g)
	return out
}

func shifted(doc []item, by int) []item {
	out := make([]item, len(doc))
	for i, it := range doc {
		switch it.tag {
		case tagText:
			it.offset += by
		case tagGroup:
			it.items = shifted(it.items, by)
		case tagSpacing:
		}
		out[i] = it
	}
	return out
}

type rest struct {
	items []item
	next  *rest
}

type level struct {
	nest   int
	indent int
}

type renderer struct {
	out       []byte
	column    int
	lineStart bool
	stack     []level
	current   int
	newlines  int
}

func render(doc []item) string {
	r := &renderer{lineStart: true, stack: []level{{}}}
	r.expanded(doc, nil)
	return strings.Trim(string(r.out), "\n") + "\n"
}

func (r *renderer) pop(nest int) {
	for r.stack[len(r.stack)-1].nest > nest {
		r.stack = r.stack[:len(r.stack)-1]
	}
}

func (r *renderer) write(it item, register bool) {
	if r.lineStart {
		r.pop(it.nest)
		if top := r.stack[len(r.stack)-1]; top.nest < it.nest {
			r.stack = append(r.stack, level{nest: it.nest, indent: top.indent + indentWidth})
		}
		r.current = r.stack[len(r.stack)-1].indent
		r.out = append(r.out, strings.Repeat(" ", r.current+it.offset)...)
		r.column = it.offset
		r.lineStart = false
	} else if register {
		r.pop(it.nest)
		if top := r.stack[len(r.stack)-1]; top.nest < it.nest {
			r.stack = append(r.stack, level{nest: it.nest, indent: r.current})
		}
	}
	r.out = append(r.out, it.text...)
	r.column += utf8.RuneCountInString(it.text)
}

func (r *renderer) newline(count int) {
	if r.lineStart {
		if count > r.newlines {
			r.out = append(r.out, strings.Repeat("\n", count-r.newlines)...)
			r.newlines = count
		}
		return
	}
	r.out = append(r.out, strings.Repeat("\n", count)...)
	r.lineStart = true
	r.newlines = count
	r.column = 0
}

func (r *renderer) blank() {
	if r.lineStart {
		return
	}
	r.out = append(r.out, ' ')
	r.column++
}

func (r *renderer) compact(doc []item, register bool) {
	for _, it := range doc {
		switch it.tag {
		case tagText:
			if it.kind != textTrailing {
				r.write(it, register)
			}
		case tagSpacing:
			if it.space == spacePlain || it.space == spaceHard || it.space == spaceSoft {
				r.blank()
			}
		case tagGroup:
			r.compact(it.items, register && it.group == groupTransparent)
		}
	}
}

func firstLine(doc []item) (int, bool) {
	width := 0
	for _, it := range doc {
		switch it.tag {
		case tagText:
			if it.kind == textRegular || it.kind == textComment {
				width += utf8.RuneCountInString(it.text)
			}
		case tagSpacing:
			if it.space == spaceHard {
				width++
				continue
			}
			return width, true
		case tagGroup:
			n, stopped := groupLine(it.items)
			width += n
			if stopped {
				return width, true
			}
		}
	}
	return width, false
}

func groupLine(doc []item) (int, bool) {
	width := 0
	for _, it := range doc {
		switch it.tag {
		case tagText:
			if it.kind == textRegular || it.kind == textComment {
				width += utf8.RuneCountInString(it.text)
			}
		case tagSpacing:
			switch it.space {
			case lineHard, lineEmpty, lineCount:
				return width, true
			case spacePlain, spaceHard, spaceSoft:
				width++
			case breakSoft, breakPlain:
			}
		case tagGroup:
			n, stopped := groupLine(it.items)
			width += n
			if stopped {
				return width, true
			}
		}
	}
	return width, false
}

func (s *rest) line() int {
	width := 0
	for at := s; at != nil; at = at.next {
		n, stopped := firstLine(at.items)
		width += n
		if stopped {
			break
		}
	}
	return width
}

func (r *renderer) expanded(doc []item, after *rest) {
	for i, it := range doc {
		following := &rest{items: doc[i+1:], next: after}
		switch it.tag {
		case tagText:
			r.write(it, true)
		case tagSpacing:
			r.spaced(it, following)
		case tagGroup:
			r.laidOut(it, following)
		}
	}
}

func (r *renderer) spaced(it item, following *rest) {
	switch it.space {
	case spaceHard:
		r.blank()
	case spacePlain, breakPlain, lineHard:
		r.newline(1)
	case lineEmpty:
		r.newline(2)
	case lineCount:
		r.newline(it.count)
	case spaceSoft:
		if r.column+1+following.line() > lineWidth {
			r.newline(1)
			return
		}
		r.blank()
	case breakSoft:
		if r.column+following.line() > lineWidth {
			r.newline(1)
		}
	}
}

func (r *renderer) fits(g item, after *rest) bool {
	if g.hard {
		return false
	}
	width := r.column + g.width
	return width <= lineWidth && width+after.line() <= lineWidth
}

func (r *renderer) laidOut(g item, after *rest) {
	if r.fits(g, after) {
		r.compact(g.items, g.group == groupTransparent)
		return
	}
	if g.group != groupTransparent && r.prioritised(g.items, after) {
		return
	}
	r.expanded(g.items, after)
}

func unfolded(doc []item) []item {
	out := make([]item, 0, len(doc))
	for _, it := range doc {
		if it.tag == tagGroup && it.group == groupTransparent {
			out = append(out, unfolded(it.items)...)
			continue
		}
		out = append(out, it)
	}
	return out
}

func (r *renderer) forced(doc []item, after *rest) {
	if len(doc) == 1 && doc[0].tag == tagGroup {
		if r.prioritised(doc[0].items, after) {
			return
		}
		r.forced(doc[0].items, after)
		return
	}
	r.expanded(doc, after)
}

func measuredRun(doc []item) item { return measured(item{tag: tagGroup, items: doc}) }

func (r *renderer) prioritised(doc []item, after *rest) bool {
	flat := unfolded(doc)
	for i := len(flat) - 1; i >= 0; i-- {
		if flat[i].tag != tagGroup || flat[i].group != groupPriority {
			continue
		}
		before, tail := measuredRun(flat[:i]), measuredRun(flat[i+1:])
		if before.hard || tail.hard || r.column+before.width > lineWidth {
			continue
		}
		saved := *r
		saved.stack = slices.Clone(r.stack)
		r.compact(before.items, false)
		start, column := len(r.out), r.column
		r.forced(flat[i].items, &rest{items: tail.items, next: after})
		head, _, _ := strings.Cut(string(r.out[start:]), "\n")
		if column+utf8.RuneCountInString(head) <= lineWidth && r.column+tail.width+after.line() <= lineWidth {
			r.compact(tail.items, false)
			return true
		}
		*r = saved
		r.out = r.out[:len(saved.out)]
	}
	return false
}

