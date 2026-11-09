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
	groupRigid
)

type lookahead struct {
	compact     int32
	first       int32
	nest        int32
	compactHard bool
}

type item struct {
	tag    itemTag
	kind   textKind
	space  spacing
	group  groupKind
	hard   bool
	broken bool
	opens  bool
	trials bool
	follow bool
	nest   int32
	offset int32
	count  int32
	id     int32
	width  int32
	prefix int32
	first  int32
	floor  int32
	next   lookahead
	text   string
	items  []item
}

const noFloor = int32(^uint32(0) >> 1)

func (it *item) lowerFloor(child *item) {
	switch child.tag {
	case tagText:
		it.floor = min(it.floor, child.nest)
		if child.kind == textTrailingComment && child.next.nest >= 0 {
			it.floor = min(it.floor, child.next.nest)
		}
	case tagGroup:
		it.floor = min(it.floor, child.floor)
	case tagSpacing:
	}
}

func (it *item) measure() {
	it.hard, it.broken, it.width, it.prefix, it.first, it.floor = false, false, 0, 0, 0, noFloor
	for i := range it.items {
		child := &it.items[i]
		it.lowerFloor(child)
		switch child.tag {
		case tagText:
			if child.counted() {
				it.width += child.width
				if !it.hard {
					it.prefix += child.width
				}
				if !it.broken {
					it.first += child.width
				}
			}
		case tagSpacing:
			it.measureSpacing(child.space)
		case tagGroup:
			if !it.hard {
				it.prefix += child.prefix
			}
			if !it.broken {
				it.first += child.first
			}
			it.hard = it.hard || child.hard
			it.broken = it.broken || child.broken
			it.width += child.width
		}
	}
}

func (it *item) measureSpacing(s spacing) {
	switch s {
	case lineHard, lineEmpty, lineCount:
		it.hard, it.broken = true, true
	case spaceHard:
		it.width++
		if !it.hard {
			it.prefix++
		}
		if !it.broken {
			it.first++
		}
	case spacePlain, spaceSoft:
		it.width++
		if !it.hard {
			it.prefix++
		}
		it.broken = true
	case breakSoft, breakPlain:
		it.broken = true
	}
}

func (it *item) counted() bool { return it.kind == textRegular }

type annotator struct{ groups int32 }

func (a *annotator) pass(items []item, f lookahead) lookahead {
	for i := len(items) - 1; i >= 0; i-- {
		it := &items[i]
		it.next = f
		switch it.tag {
		case tagText:
			it.width = int32(utf8.RuneCountInString(it.text)) //nolint:gosec // G115: a file over 2 GiB is refused before this point
			f.nest = it.nest
			if it.counted() {
				f.compact += it.width
				f.first += it.width
			}
		case tagSpacing:
			if it.space == spaceHard {
				f.compact++
				f.first++
				continue
			}
			f.compact, f.first, f.compactHard = 0, 0, false
		case tagGroup:
			it.id = a.groups
			a.groups++
			inner := a.pass(it.items, f)
			it.measure()
			f.first, f.nest = inner.first, inner.nest
			if it.hard {
				f.compact, f.compactHard = it.prefix, true
			} else {
				f.compact += it.width
			}
		}
	}
	return f
}

type level struct {
	parent *level
	nest   int
	indent int
}

type state struct {
	stack     *level
	current   int
	column    int
	lineStart bool
	headOpen  bool
}

type outcome struct {
	state state
	head  int
}

const noHead = -1

type planKey struct {
	id     int32
	forced bool
	beyond int
	state  state
}

type plan struct {
	choice int
	result outcome
}

const expand = -1

type prioritySlot struct {
	index       int
	beforeWidth int
	tailWidth   int
	tailFirst   int
	beforeHard  bool
	tailHard    bool
	tailBroken  bool
}

type priorityPlan struct {
	flat  []item
	slots []prioritySlot
}

type levelKey struct {
	parent *level
	nest   int
	indent int
}

type renderer struct {
	out    []byte
	plans  map[planKey]plan
	levels map[levelKey]*level
	prio   []*priorityPlan
	beyond int
}

func render(doc []item, hint int) string {
	a := &annotator{}
	a.pass(doc, lookahead{nest: -1})
	root := &level{}
	r := &renderer{
		out:    make([]byte, 0, hint+hint/8),
		plans:  make(map[planKey]plan),
		levels: make(map[levelKey]*level),
		prio:   make([]*priorityPlan, a.groups),
	}
	r.sequence(doc, outcome{state: state{stack: root, lineStart: true}, head: noHead}, true)
	return strings.Trim(string(r.out), "\n") + "\n"
}

func (r *renderer) enter(st *state, nest32 int32, indent int) {
	nest := int(nest32)
	stack := st.stack
	for stack.nest > nest {
		stack = stack.parent
	}
	if stack.nest < nest {
		if indent < 0 {
			indent = stack.indent + indentWidth
		}
		stack = r.intern(stack, nest, indent)
	}
	st.stack = stack
}

func (r *renderer) spaces(n int, emit bool) {
	if !emit {
		return
	}
	for range n {
		r.out = append(r.out, ' ')
	}
}

func (r *renderer) write(it *item, o outcome, register, emit bool) outcome {
	st := &o.state
	midLine := !st.lineStart
	switch {
	case st.lineStart:
		r.enter(st, it.nest, -1)
		st.current = st.stack.indent
		r.spaces(st.current+int(it.offset), emit)
		st.column = 0
		st.lineStart = false
	case register && it.kind != textComment:
		r.enter(st, it.nest, st.current)
	}
	if midLine && it.kind == textTrailingComment && r.alignsWithNextLine(it, st) {
		r.spaces(1, emit)
		st.column = advance(st.column, 1)
	}
	if emit {
		r.out = append(r.out, it.text...)
	}
	if o.head == noHead {
		if at := strings.IndexByte(it.text, '\n'); at >= 0 {
			o.head = st.column + utf8.RuneCountInString(it.text[:at])
		}
	}
	st.column = advance(st.column, int(it.width))
	return o
}

func advance(column, by int) int { return min(column+by, lineWidth+1) }

func (r *renderer) alignsWithNextLine(it *item, st *state) bool {
	if it.next.nest < 0 {
		return false
	}
	nest := int(it.next.nest)
	stack := st.stack
	for stack.nest > nest {
		stack = stack.parent
	}
	indent := stack.indent
	if stack.nest < nest {
		indent += indentWidth
	}
	return st.current+st.column == indent
}

func (r *renderer) newline(o outcome, count int, emit bool) outcome {
	st := &o.state
	if st.lineStart {
		return o
	}
	if o.head == noHead {
		o.head = st.column
	}
	if emit {
		for range count {
			r.out = append(r.out, '\n')
		}
	}
	st.lineStart = true
	st.column = 0
	return o
}

func (r *renderer) blank(o outcome, emit bool) outcome {
	if o.state.lineStart {
		return o
	}
	if emit {
		r.out = append(r.out, ' ')
	}
	o.state.column = advance(o.state.column, 1)
	return o
}

func (r *renderer) compact(doc []item, o outcome, register, emit bool) outcome {
	for i := range doc {
		it := &doc[i]
		switch it.tag {
		case tagText:
			if it.kind != textTrailing {
				o = r.write(it, o, register, emit)
			}
		case tagSpacing:
			if it.space == spacePlain || it.space == spaceHard || it.space == spaceSoft {
				o = r.blank(o, emit)
			}
		case tagGroup:
			o = r.compact(it.items, o, register && it.group == groupTransparent, emit)
		}
	}
	return o
}

func (r *renderer) sequence(doc []item, o outcome, emit bool) outcome {
	for i := range doc {
		it := &doc[i]
		switch it.tag {
		case tagText:
			o = r.write(it, o, true, emit)
		case tagSpacing:
			o = r.spaced(it, o, emit)
		case tagGroup:
			o = r.placedAt(it, o, emit)
		}
	}
	return o
}

func (r *renderer) placedAt(g *item, o outcome, emit bool) outcome {
	if g.opens && o.state.lineStart {
		return r.forcedSequence(g, o, emit)
	}
	if g.trials {
		opened := !r.fits(g, &o.state)
		o = r.placed(g, o, emit)
		o.state.headOpen = opened
		return o
	}
	return r.placed(g, o, emit)
}

func (r *renderer) spaced(it *item, o outcome, emit bool) outcome {
	if it.follow && !o.state.headOpen {
		if it.space == breakPlain {
			return o
		}
		return r.blank(o, emit)
	}
	switch it.space {
	case spaceHard:
		return r.blank(o, emit)
	case spacePlain, breakPlain, lineHard:
		return r.newline(o, 1, emit)
	case lineEmpty:
		return r.newline(o, 2, emit)
	case lineCount:
		return r.newline(o, int(it.count), emit)
	case spaceSoft:
		if softStays(it.next, o.state.column+1) {
			return r.blank(o, emit)
		}
		return r.newline(o, 1, emit)
	case breakSoft:
		if softStays(it.next, o.state.column) {
			return o
		}
		return r.newline(o, 1, emit)
	}
	return o
}

func softStays(f lookahead, column int) bool {
	if !f.compactHard {
		if column+int(f.compact) <= lineWidth {
			return true
		}
		if f.compact <= lineWidth {
			return false
		}
	}
	return column+int(f.first) <= lineWidth
}

func (r *renderer) fits(g *item, st *state) bool {
	width := st.column + int(g.width)
	return !g.hard && width <= lineWidth && width+int(g.next.first) <= lineWidth
}

func (r *renderer) placed(g *item, o outcome, emit bool) outcome {
	if r.fits(g, &o.state) {
		return r.compact(g.items, o, g.group == groupTransparent, emit)
	}
	return r.planned(g, o, false, emit)
}

func (r *renderer) planned(g *item, o outcome, forced, emit bool) outcome {
	p := r.planFor(g, o.state, forced)
	if emit {
		return r.carry(o, r.execute(g, p.choice, outcome{state: o.state, head: noHead}, forced, true))
	}
	return r.carry(o, p.result)
}

func (r *renderer) carry(o, inner outcome) outcome {
	if o.head != noHead {
		inner.head = o.head
	}
	return inner
}

func (r *renderer) intern(parent *level, nest, indent int) *level {
	key := levelKey{parent, nest, indent}
	next, ok := r.levels[key]
	if !ok {
		next = &level{parent: parent, nest: nest, indent: indent}
		r.levels[key] = next
	}
	return next
}

func (r *renderer) rebased(stack, floor *level, base *level) *level {
	if stack == floor {
		return base
	}
	return r.intern(r.rebased(stack.parent, floor, base), stack.nest, stack.indent-floor.indent+base.indent)
}

func (r *renderer) planFor(g *item, st state, forced bool) plan {
	floor := st.stack
	for floor.nest > int(g.floor) {
		floor = floor.parent
	}
	relative := r.intern(nil, floor.nest, 0)
	inner := st
	inner.stack = r.rebased(st.stack, floor, relative)
	inner.current -= floor.indent
	key := planKey{id: g.id, forced: forced, beyond: r.beyond, state: inner}
	p, ok := r.plans[key]
	if !ok {
		choice := expand
		if forced || g.group != groupTransparent {
			choice = r.choose(g, inner)
		}
		p = plan{choice: choice, result: r.execute(g, choice, outcome{state: inner, head: noHead}, forced, false)}
		r.plans[key] = p
	}
	p.result.state.stack = r.rebased(p.result.state.stack, relative, floor)
	p.result.state.current += floor.indent
	return p
}

func (r *renderer) priorityPlan(g *item) *priorityPlan {
	if p := r.prio[g.id]; p != nil {
		return p
	}
	p := &priorityPlan{flat: unfolded(g.items)}
	before, hard := 0, false
	for i := range p.flat {
		it := &p.flat[i]
		if it.tag == tagGroup && it.group == groupPriority {
			p.slots = append(p.slots, prioritySlot{index: i, beforeWidth: before, beforeHard: hard})
		}
		width, breaks := flatMeasure(it)
		before += width
		hard = hard || breaks
	}
	tail, tailHard, first, broken := 0, false, 0, false
	at := len(p.flat) - 1
	for s := len(p.slots) - 1; s >= 0; s-- {
		slot := &p.slots[s]
		for ; at > slot.index; at-- {
			it := &p.flat[at]
			width, breaks := flatMeasure(it)
			tail += width
			tailHard = tailHard || breaks
			first, broken = tailLine(it, first, broken)
		}
		slot.tailWidth, slot.tailHard, slot.tailFirst, slot.tailBroken = tail, tailHard, first, broken
		it := &p.flat[at]
		width, breaks := flatMeasure(it)
		tail += width
		tailHard = tailHard || breaks
		first, broken = tailLine(it, first, broken)
		at--
	}
	r.prio[g.id] = p
	return p
}

func flatMeasure(it *item) (int, bool) {
	switch it.tag {
	case tagText:
		if it.counted() {
			return int(it.width), false
		}
	case tagSpacing:
		switch it.space {
		case lineHard, lineEmpty, lineCount:
			return 0, true
		case spacePlain, spaceHard, spaceSoft:
			return 1, false
		case breakSoft, breakPlain:
		}
	case tagGroup:
		return int(it.width), it.hard
	}
	return 0, false
}

func tailLine(it *item, first int, broken bool) (int, bool) {
	switch it.tag {
	case tagText:
		if it.counted() {
			first += int(it.width)
		}
	case tagSpacing:
		switch it.space {
		case lineHard, lineEmpty, lineCount:
			return 0, true
		case spacePlain, spaceHard, spaceSoft:
			first++
		case breakSoft, breakPlain:
		}
	case tagGroup:
		if it.broken {
			return int(it.first), true
		}
		first += int(it.first)
	}
	return first, broken
}

func unfolded(doc []item) []item {
	if !slices.ContainsFunc(doc, transparentGroup) {
		return doc
	}
	return appendUnfolded(make([]item, 0, len(doc)), doc)
}

func transparentGroup(it item) bool { return it.tag == tagGroup && it.group == groupTransparent }

func appendUnfolded(out, doc []item) []item {
	for i := range doc {
		if transparentGroup(doc[i]) {
			out = appendUnfolded(out, doc[i].items)
			continue
		}
		out = append(out, doc[i])
	}
	return out
}

func (r *renderer) choose(g *item, st state) int {
	p := r.priorityPlan(g)
	for s := len(p.slots) - 1; s >= 0; s-- {
		if r.trial(g, p, &p.slots[s], st) {
			return s
		}
	}
	return expand
}

func (r *renderer) trial(g *item, p *priorityPlan, slot *prioritySlot, st state) bool {
	if slot.beforeHard || slot.tailHard || st.column+slot.beforeWidth > lineWidth {
		return false
	}
	chosen := &p.flat[slot.index]
	beyondSlot := r.beyondSlot(g, slot)
	if !chosen.hard && st.column+slot.beforeWidth+int(chosen.width)+beyondSlot <= lineWidth {
		return false
	}
	start := outcome{state: st, head: noHead}
	start = r.compact(p.flat[:slot.index], start, true, false)
	start.head = noHead
	following := r.beyond
	r.beyond = beyondSlot
	result := r.forcedSequence(chosen, start, false)
	r.beyond = following
	head := result.head
	if head == noHead {
		head = result.state.column
	}
	if head > lineWidth {
		return false
	}
	return slot.index == len(p.flat)-1 || result.state.column+slot.tailWidth+r.beyondGroup(g) <= lineWidth
}

func (r *renderer) beyondGroup(g *item) int { return max(int(g.next.first), r.beyond) }

func (r *renderer) beyondSlot(g *item, slot *prioritySlot) int {
	if slot.tailBroken {
		return slot.tailFirst
	}
	return min(slot.tailFirst+r.beyondGroup(g), lineWidth+1)
}

func (r *renderer) forcedSequence(g *item, o outcome, emit bool) outcome {
	return r.planned(g, o, true, emit)
}

func (r *renderer) execute(g *item, choice int, o outcome, forced, emit bool) outcome {
	following := r.beyond
	defer func() { r.beyond = following }()
	if choice == expand {
		if forced && len(g.items) == 1 && g.items[0].tag == tagGroup {
			return r.planned(&g.items[0], o, true, emit)
		}
		r.beyond = 0
		at, after := closingGroup(g.items)
		if !forced || following == 0 || at < 0 {
			return r.sequence(g.items, o, emit)
		}
		o = r.sequence(g.items[:at], o, emit)
		o = r.placedBefore(&g.items[at], following+after, o, emit)
		return r.sequence(g.items[at+1:], o, emit)
	}
	p := r.prio[g.id]
	slot := &p.slots[choice]
	o = r.compact(p.flat[:slot.index], o, true, emit)
	r.beyond = r.beyondSlot(g, slot)
	o = r.forcedSequence(&p.flat[slot.index], o, emit)
	return r.compact(p.flat[slot.index+1:], o, false, emit)
}

func closingGroup(doc []item) (int, int) {
	after := 0
	for i := len(doc) - 1; i >= 0; i-- {
		switch doc[i].tag {
		case tagGroup:
			return i, after
		case tagSpacing:
			return -1, 0
		case tagText:
			if doc[i].counted() {
				after += int(doc[i].width)
			}
		}
	}
	return -1, 0
}

func (r *renderer) placedBefore(g *item, following int, o outcome, emit bool) outcome {
	if g.opens && o.state.lineStart {
		return r.forcedSequence(g, o, emit)
	}
	if g.hard || o.state.column+int(g.width)+following <= lineWidth {
		return r.placed(g, o, emit)
	}
	r.beyond = min(following, lineWidth+1)
	defer func() { r.beyond = 0 }()
	return r.planned(g, o, false, emit)
}

func lines(count int) []item {
	return []item{{tag: tagSpacing, space: lineCount, count: int32(count)}} //nolint:gosec // G115: a line count never approaches 2^31
}

func text(nest int, s string) []item {
	return []item{{tag: tagText, nest: int32(nest), kind: textRegular, text: s}} //nolint:gosec // G115: nesting is bounded by the parser depth
}

func textOf(nest int, s string, kind textKind) []item {
	return []item{{tag: tagText, nest: int32(nest), kind: kind, text: s}} //nolint:gosec // G115: nesting is bounded by the parser depth
}

var (
	hardSpace = []item{{tag: tagSpacing, space: spaceHard}}
	softSpace = []item{{tag: tagSpacing, space: spaceSoft}}
	softBreak = []item{{tag: tagSpacing, space: breakSoft}}
	plainBrk  = []item{{tag: tagSpacing, space: breakPlain}}
	space     = []item{{tag: tagSpacing, space: spacePlain}}
	following = []item{{tag: tagSpacing, space: spacePlain, follow: true}}
	followBrk = []item{{tag: tagSpacing, space: breakPlain, follow: true}}
	hardLine  = []item{{tag: tagSpacing, space: lineHard}}
	emptyLine = []item{{tag: tagSpacing, space: lineEmpty}}
)

func seq(parts ...[]item) []item {
	total, filled := 0, -1
	for i, p := range parts {
		total += len(p)
		if len(p) > 0 && filled < 0 {
			filled = i
		}
	}
	if total == 0 {
		return nil
	}
	if len(parts[filled]) == total {
		return parts[filled]
	}
	parts = parts[filled:]
	out := parts[0]
	if cap(out) < total {
		out = append(make([]item, 0, max(total, 2*len(out))), out...)
	}
	for _, p := range parts[1:] {
		out = append(out, p...)
	}
	return out
}

func grouped(kind groupKind, parts ...[]item) []item {
	return []item{{tag: tagGroup, group: kind, items: seq(parts...)}}
}

func group(parts ...[]item) []item { return grouped(groupRegular, parts...) }

func priority(parts ...[]item) []item { return grouped(groupPriority, parts...) }

func openingPriority(parts ...[]item) []item {
	out := priority(parts...)
	out[0].opens = true
	return out
}

func trialHead(parts ...[]item) []item {
	out := group(parts...)
	out[0].trials = true
	return out
}

func transparent(parts ...[]item) []item { return grouped(groupTransparent, parts...) }

func rankOf(s spacing) int {
	switch s {
	case breakSoft:
		return 0
	case breakPlain:
		return 1
	case spaceHard:
		return 2
	case spaceSoft:
		return 3
	case spacePlain:
		return 4
	case lineHard:
		return 5
	case lineEmpty:
		return 6
	case lineCount:
		return 7
	}
	return 0
}

func mergeSpacing(a, b item) item {
	if a.space == breakPlain && (b.space == spaceHard || b.space == spaceSoft) || b.space == breakPlain && (a.space == spaceHard || a.space == spaceSoft) {
		return item{tag: tagSpacing, space: spacePlain}
	}
	if a.space == lineCount && b.space == lineCount {
		return item{tag: tagSpacing, space: lineCount, count: max(a.count, b.count)}
	}
	if rankOf(b.space) > rankOf(a.space) {
		return b
	}
	return a
}

func hardOrBlank(it *item) bool {
	if it.tag != tagSpacing {
		return false
	}
	switch it.space {
	case lineHard, lineEmpty, lineCount, spaceHard:
		return true
	case spaceSoft, spacePlain, breakSoft, breakPlain:
	}
	return false
}

func leadsOut(it *item) bool {
	return hardOrBlank(it) || (it.tag == tagText && it.kind == textComment)
}

func pushItem(out []item, it item) []item {
	last := len(out) - 1
	if last >= 0 {
		prev := &out[last]
		if it.tag == tagSpacing && prev.tag == tagSpacing {
			*prev = mergeSpacing(*prev, it)
			return out
		}
		if it.tag == tagText && it.kind == textRegular && prev.tag == tagText && prev.kind == textRegular {
			prev.text += it.text
			return out
		}
	}
	return append(out, it)
}

func rigid(doc []item) []item { return grouped(groupRigid, doc) }

func fixup(doc []item) []item { return fixupInto(make([]item, 0, len(doc)), doc, false) }

func fixupInto(out, doc []item, flat bool) []item {
	for i := range doc {
		it := doc[i]
		switch it.tag {
		case tagText:
			out = pushItem(out, it)
		case tagSpacing:
			if flat {
				switch it.space {
				case spacePlain, spaceSoft:
					it.space = spaceHard
				case breakPlain, breakSoft:
					continue
				case spaceHard, lineHard, lineEmpty, lineCount:
				}
			}
			out = pushItem(out, it)
		case tagGroup:
			if flat || it.group == groupRigid {
				out = fixupInto(out, it.items, true)
				continue
			}
			out = fixupGroup(out, it)
		}
	}
	return out
}

func fixupGroup(out []item, it item) []item {
	inner := fixupInto(make([]item, 0, len(it.items)), it.items, false)
	for len(inner) > 0 && leadsOut(&inner[0]) {
		out = pushItem(out, inner[0])
		inner = inner[1:]
	}
	end := len(inner)
	for end > 0 && hardOrBlank(&inner[end-1]) {
		end--
	}
	moved := inner[end:]
	inner = inner[:end]
	if len(inner) == 1 && it.group == groupRegular && inner[0].tag == tagGroup && inner[0].group == groupRegular {
		inner = inner[0].items
	}
	if len(inner) > 0 {
		it.items = inner
		out = append(out, it)
	}
	for j := range moved {
		out = pushItem(out, moved[j])
	}
	return out
}
