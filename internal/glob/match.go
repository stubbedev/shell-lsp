package glob

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

var (
	ErrUnsupported = errors.New("pattern cannot be evaluated")
	ErrTooComplex  = errors.New("pattern needs too many steps to evaluate")
)

const maxSteps = 1 << 18

type Match struct {
	Start, End int
	Captures   [][2]int
}

type Matcher struct {
	pattern   Pattern
	wholeOnly string
}

func (p Pattern) Matcher() (Matcher, error) {
	if p.Unevaluable != "" {
		return Matcher{}, fmt.Errorf("%s: %w", p.Unevaluable, ErrUnsupported)
	}
	var err error
	Walk(p.Root, func(n Node) bool {
		switch n := n.(type) {
		case *Unknown:
			err = fmt.Errorf("%s has no known value: %w", n.Source, ErrUnsupported)
		case *Recursive, *Qualifiers:
			err = fmt.Errorf("filename generation: %w", ErrUnsupported)
		case *Bracket:
			for _, item := range n.Items {
				if item.Class != "" && classes[item.Class] == nil {
					err = fmt.Errorf("character class [:%s:]: %w", item.Class, ErrUnsupported)
				}
			}
		}
		return err == nil
	})
	if err != nil {
		return Matcher{}, err
	}
	m := Matcher{pattern: p}
	if p.Lazy {
		m.wholeOnly = "a non-greedy ksh group"
	}
	Walk(p.Root, func(n Node) bool {
		if _, ok := n.(*Search); ok {
			m.wholeOnly = "a ksh ~(E), ~(F) or ~(G) search"
		}
		return m.wholeOnly == ""
	})
	return m, nil
}

func (m Matcher) Whole(s string) (Match, bool, error) {
	r := m.run(s)
	ok := r.whole(0, len(r.text))
	return r.result(0, len(r.text)), ok, r.err()
}

func (m Matcher) substrings() error {
	if m.wholeOnly != "" {
		return fmt.Errorf("matching part of a value with %s: %w", m.wholeOnly, ErrUnsupported)
	}
	return nil
}

func (m Matcher) RemovePrefix(s string, longest bool) (string, error) {
	if err := m.substrings(); err != nil {
		return "", err
	}
	r := m.run(s)
	n := len(r.text)
	for step := range n + 1 {
		j := step
		if longest {
			j = n - step
		}
		if r.whole(0, j) {
			return string(r.text[j:]), r.err()
		}
	}
	return s, r.err()
}

func (m Matcher) RemoveSuffix(s string, longest bool) (string, error) {
	if err := m.substrings(); err != nil {
		return "", err
	}
	r := m.run(s)
	n := len(r.text)
	for step := range n + 1 {
		i := n - step
		if longest {
			i = step
		}
		if r.whole(i, n) {
			return string(r.text[:i]), r.err()
		}
	}
	return s, r.err()
}

type Anchoring uint8

const (
	Anywhere Anchoring = iota
	AtStart
	AtEnd
	WholeValue
)

func (m Matcher) Search(s string) (Match, bool, error) {
	r := m.run(s)
	for i := range len(r.text) + 1 {
		if end, ok := r.longestAt(i, Anywhere); ok {
			return r.result(i, end), true, r.err()
		}
	}
	return Match{}, false, r.err()
}

func (m Matcher) Replace(s string, anchoring Anchoring, all bool, with func(text string, found Match) string) (string, error) {
	if err := m.substrings(); err != nil {
		return "", err
	}
	r := m.run(s)
	n := len(r.text)
	var out strings.Builder
	copied := 0
	for i := 0; i < n || i == 0 || anchoring == AtEnd && i == n; {
		end, ok := r.longestAt(i, anchoring)
		if !ok {
			if anchoring == AtStart || anchoring == WholeValue {
				break
			}
			i++
			continue
		}
		out.WriteString(string(r.text[copied:i]))
		out.WriteString(with(s, r.result(i, end)))
		copied = end
		if !all || anchoring != Anywhere {
			break
		}
		if end == i {
			if i < n {
				out.WriteRune(r.text[i])
			}
			copied = i + 1
			i++
			continue
		}
		i = end
	}
	if copied < n {
		out.WriteString(string(r.text[copied:]))
	}
	return out.String(), r.err()
}

func (r *run) longestAt(i int, anchoring Anchoring) (int, bool) {
	n := len(r.text)
	if (anchoring == AtStart || anchoring == WholeValue) && i != 0 {
		return 0, false
	}
	if anchoring == AtEnd || anchoring == WholeValue {
		return n, r.whole(i, n)
	}
	for j := n; j >= i; j-- {
		if r.whole(i, j) {
			return j, true
		}
	}
	return 0, false
}

type run struct {
	m         Matcher
	text      []rune
	byteAt    []int
	caps      [][2]int
	steps     int
	exhausted bool
}

func (m Matcher) run(s string) *run {
	r := &run{m: m, text: []rune(s), caps: make([][2]int, m.pattern.Captures)}
	r.byteAt = make([]int, 0, len(r.text)+1)
	for i := range s {
		r.byteAt = append(r.byteAt, i)
	}
	r.byteAt = append(r.byteAt, len(s))
	r.resetCaptures()
	return r
}

func (r *run) resetCaptures() {
	for i := range r.caps {
		r.caps[i] = [2]int{-1, -1}
	}
}

func (r *run) err() error {
	if r.exhausted {
		return ErrTooComplex
	}
	return nil
}

func (r *run) result(i, j int) Match {
	found := Match{Start: r.byteAt[i], End: r.byteAt[j], Captures: make([][2]int, len(r.caps))}
	for k, c := range r.caps {
		found.Captures[k] = [2]int{-1, -1}
		if c[0] >= 0 {
			found.Captures[k] = [2]int{r.byteAt[c[0]], r.byteAt[c[1]]}
		}
	}
	return found
}

func (r *run) whole(i, j int) bool {
	r.resetCaptures()
	return r.match(r.m.pattern.Root, i, func(e int) bool { return e == j })
}

func (r *run) sub(n Node, i, j int) bool {
	saved := append([][2]int(nil), r.caps...)
	ok := r.match(n, i, func(e int) bool { return e == j })
	copy(r.caps, saved)
	return ok
}

func (r *run) match(n Node, i int, k func(int) bool) bool {
	r.steps++
	if r.steps > maxSteps {
		r.exhausted = true
	}
	if r.exhausted {
		return false
	}
	switch n := n.(type) {
	case *Seq:
		return r.seq(n.Items, i, k)
	case *Literal:
		end, ok := r.literal(n, i)
		return ok && k(end)
	case *AnyChar:
		return i < len(r.text) && k(i+1)
	case *AnyString:
		for j := len(r.text); j >= i; j-- {
			if k(j) {
				return true
			}
		}
		return false
	case *Bracket:
		return i < len(r.text) && r.bracket(n, r.text[i]) && k(i+1)
	case *Alternatives:
		for _, choice := range n.Choices {
			if r.match(choice, i, k) {
				return true
			}
		}
		return false
	case *Group:
		return r.group(n, i, k)
	case *Repeat:
		return r.repeat(n.X, n.Min, n.Max, 0, i, k)
	case *Not:
		return r.not(n.X, i, k)
	case *Exclude:
		return r.match(n.Base, i, func(j int) bool {
			for _, except := range n.Except {
				if r.sub(except, i, j) {
					return false
				}
			}
			return k(j)
		})
	case *NumRange:
		return r.numRange(n, i, k)
	case *Anchor:
		if n.Start {
			return i == 0 && k(i)
		}
		return i == len(r.text) && k(i)
	case *Flags:
		return k(i)
	case *Escape:
		return r.escape(n.Kind, i, k)
	case *Backref:
		return r.backref(n.N, i, k)
	case *KshOptions:
		if n.X == nil {
			return k(i)
		}
		return r.match(n.X, i, k)
	case *Search:
		return r.search(n, i, k)
	}
	panic(fmt.Sprintf("glob: cannot match %T", n))
}

func isWordRune(c rune) bool { return c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c) }

func (r *run) wordAt(i int) bool { return i >= 0 && i < len(r.text) && isWordRune(r.text[i]) }

func (r *run) escape(kind byte, i int, k func(int) bool) bool {
	char := func(test func(rune) bool) bool { return i < len(r.text) && test(r.text[i]) && k(i+1) }
	before, after := r.wordAt(i-1), r.wordAt(i)
	switch kind {
	case 'w':
		return char(isWordRune)
	case 'W':
		return char(func(c rune) bool { return !isWordRune(c) })
	case 'd':
		return char(func(c rune) bool { return c >= '0' && c <= '9' })
	case 'D':
		return char(func(c rune) bool { return c < '0' || c > '9' })
	case 's':
		return char(unicode.IsSpace)
	case 'S':
		return char(func(c rune) bool { return !unicode.IsSpace(c) })
	case 'b':
		return before != after && k(i)
	case 'B':
		return before == after && k(i)
	case '<':
		return !before && after && k(i)
	case '>':
		return before && !after && k(i)
	case '`':
		return i == 0 && k(i)
	case '\'':
		return i == len(r.text) && k(i)
	}
	panic(fmt.Sprintf("glob: unknown escape \\%c", kind))
}

func (r *run) backref(n, i int, k func(int) bool) bool {
	if n > len(r.caps) || r.caps[n-1][0] < 0 {
		return false
	}
	c := r.caps[n-1]
	width := c[1] - c[0]
	if i+width > len(r.text) || string(r.text[i:i+width]) != string(r.text[c[0]:c[1]]) {
		return false
	}
	return k(i + width)
}

func (r *run) search(n *Search, i int, k func(int) bool) bool {
	for j := len(r.text); j >= i; j-- {
		for from := i; from <= j; from++ {
			if n.Left && from != i {
				break
			}
			if r.sub(n.X, from, j) || !n.Right && r.match(n.X, from, func(e int) bool { return e <= j }) {
				if k(j) {
					return true
				}
				break
			}
		}
	}
	return false
}

func (r *run) seq(items []Node, i int, k func(int) bool) bool {
	if len(items) == 0 {
		return k(i)
	}
	return r.match(items[0], i, func(j int) bool { return r.seq(items[1:], j, k) })
}

func (r *run) group(g *Group, i int, k func(int) bool) bool {
	switch g.Op {
	case GroupPlain, GroupOne:
		if g.Capture == 0 {
			return r.match(g.X, i, k)
		}
		return r.match(g.X, i, func(j int) bool {
			saved := r.caps[g.Capture-1]
			r.caps[g.Capture-1] = [2]int{i, j}
			if k(j) {
				return true
			}
			r.caps[g.Capture-1] = saved
			return false
		})
	case GroupZeroOrOne:
		return r.match(g.X, i, k) || k(i)
	case GroupZeroOrMore:
		return r.repeat(g.X, 0, unbounded, 0, i, k)
	case GroupOneOrMore:
		return r.repeat(g.X, 1, unbounded, 0, i, k)
	case GroupCounted:
		return r.repeat(g.X, g.Min, g.Max, 0, i, k)
	case GroupNot:
		return r.not(g.X, i, k)
	}
	panic(fmt.Sprintf("glob: unknown group %d", g.Op))
}

func (r *run) repeat(x Node, lo, hi, count, i int, k func(int) bool) bool {
	if hi == unbounded || count < hi {
		more := r.match(x, i, func(j int) bool {
			if j == i {
				return k(j)
			}
			return r.repeat(x, lo, hi, count+1, j, k)
		})
		if more {
			return true
		}
	}
	return count >= lo && k(i)
}

func (r *run) not(x Node, i int, k func(int) bool) bool {
	for j := len(r.text); j >= i; j-- {
		if !r.sub(x, i, j) && k(j) {
			return true
		}
	}
	return false
}

func (r *run) numRange(n *NumRange, i int, k func(int) bool) bool {
	end := i
	for end < len(r.text) && r.text[end] >= '0' && r.text[end] <= '9' {
		end++
	}
	for j := end; j > i; j-- {
		v, err := strconv.ParseUint(string(r.text[i:j]), 10, 64)
		overflow := err != nil
		if n.HasLo && !overflow && v < n.Lo {
			continue
		}
		if n.HasHigh && (overflow || v > n.Hi) {
			continue
		}
		if k(j) {
			return true
		}
	}
	return false
}

func (r *run) literal(n *Literal, i int) (int, bool) {
	for _, want := range n.Text {
		if i >= len(r.text) || !sameRune(want, r.text[i], n.Case) {
			return 0, false
		}
		i++
	}
	return i, true
}

func sameRune(pattern, text rune, cs Case) bool {
	if pattern == text {
		return true
	}
	switch cs {
	case CaseInsensitive:
		return unicode.ToLower(pattern) == unicode.ToLower(text)
	case CaseLowerFolds:
		return unicode.IsLower(pattern) && unicode.ToUpper(pattern) == text
	case CaseSensitive:
	}
	return false
}

func (r *run) bracket(n *Bracket, c rune) bool {
	cs := n.Case
	in := bracketHas(n.Items, c)
	if !in && cs != CaseSensitive {
		for _, alt := range []rune{unicode.ToLower(c), unicode.ToUpper(c)} {
			if alt != c && bracketHas(n.Items, alt) && (cs == CaseInsensitive || unicode.IsUpper(c)) {
				in = true
			}
		}
	}
	return in != n.Negated
}

func bracketHas(items []BracketItem, c rune) bool {
	for _, item := range items {
		if item.Class != "" {
			if classes[item.Class](c) {
				return true
			}
			continue
		}
		if item.Lo <= c && c <= item.Hi {
			return true
		}
	}
	return false
}

var classes = map[string]func(rune) bool{
	"alpha":    unicode.IsLetter,
	"digit":    func(c rune) bool { return c >= '0' && c <= '9' },
	"alnum":    func(c rune) bool { return unicode.IsLetter(c) || unicode.IsDigit(c) },
	"upper":    unicode.IsUpper,
	"lower":    unicode.IsLower,
	"space":    unicode.IsSpace,
	"blank":    func(c rune) bool { return c == ' ' || c == '\t' },
	"punct":    func(c rune) bool { return unicode.IsPunct(c) || unicode.IsSymbol(c) },
	"print":    unicode.IsPrint,
	"graph":    func(c rune) bool { return unicode.IsGraphic(c) && !unicode.IsSpace(c) },
	"cntrl":    unicode.IsControl,
	"xdigit":   func(c rune) bool { return strings.ContainsRune("0123456789abcdefABCDEF", c) },
	"word":     func(c rune) bool { return c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c) },
	"IDENT":    func(c rune) bool { return c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c) },
	"IFS":      func(c rune) bool { return strings.ContainsRune(" \t\n\x00", c) },
	"IFSSPACE": func(c rune) bool { return strings.ContainsRune(" \t\n", c) },
}
