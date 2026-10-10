package format

import (
	"slices"
	"sort"
	"strings"
)

type Edit struct {
	Start   int
	End     int
	NewText string
}

type pair struct {
	x, y int
}

type hunk struct {
	aStart, aEnd int
	bStart, bEnd int
}

func Edits(before, after string) []Edit {
	a, b := splitLines(before), splitLines(after)
	offsets := make([]int, len(a)+1)
	for i, line := range a {
		offsets[i+1] = offsets[i] + len(line)
	}
	hunks := diff(a, b)
	edits := make([]Edit, len(hunks))
	for i, h := range hunks {
		edits[i] = Edit{
			Start:   offsets[h.aStart],
			End:     offsets[h.aEnd],
			NewText: strings.Join(b[h.bStart:h.bEnd], ""),
		}
	}
	return edits
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

const lcsLimit = 1 << 20

func diff(a, b []string) []hunk {
	var hunks []hunk
	done := pair{}
	for _, m := range append(matchLines(a, b, pair{}, nil), pair{x: len(a), y: len(b)}) {
		if m.x > done.x || m.y > done.y {
			hunks = append(hunks, hunk{aStart: done.x, aEnd: m.x, bStart: done.y, bEnd: m.y})
		}
		done = pair{x: m.x + 1, y: m.y + 1}
	}
	return hunks
}

func matchLines(a, b []string, at pair, out []pair) []pair {
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		out = append(out, pair{x: at.x + prefix, y: at.y + prefix})
		prefix++
	}
	a, b = a[prefix:], b[prefix:]
	at = pair{x: at.x + prefix, y: at.y + prefix}
	suffix := 0
	for suffix < len(a) && suffix < len(b) && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	innerA, innerB := a[:len(a)-suffix], b[:len(b)-suffix]
	if len(innerA) > 0 && len(innerB) > 0 {
		out = matchInner(innerA, innerB, at, out)
	}
	for i := range suffix {
		out = append(out, pair{x: at.x + len(innerA) + i, y: at.y + len(innerB) + i})
	}
	return out
}

func matchInner(a, b []string, at pair, out []pair) []pair {
	anchored := anchors(a, b)
	if len(anchored) == 0 {
		if len(a)*len(b) > lcsLimit {
			return out
		}
		for _, p := range lcs(a, b) {
			out = append(out, pair{x: at.x + p.x, y: at.y + p.y})
		}
		return out
	}
	prev := pair{}
	for _, anchor := range anchored {
		out = matchLines(a[prev.x:anchor.x], b[prev.y:anchor.y], pair{x: at.x + prev.x, y: at.y + prev.y}, out)
		out = append(out, pair{x: at.x + anchor.x, y: at.y + anchor.y})
		prev = pair{x: anchor.x + 1, y: anchor.y + 1}
	}
	return matchLines(a[prev.x:], b[prev.y:], pair{x: at.x + prev.x, y: at.y + prev.y}, out)
}

func lcs(a, b []string) []pair {
	width := len(b) + 1
	table := make([]int32, (len(a)+1)*width)
	for i, lineA := range slices.Backward(a) {
		for j, lineB := range slices.Backward(b) {
			if lineA == lineB {
				table[i*width+j] = table[(i+1)*width+j+1] + 1
			} else {
				table[i*width+j] = max(table[(i+1)*width+j], table[i*width+j+1])
			}
		}
	}
	var out []pair
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case a[i] == b[j]:
			out = append(out, pair{x: i, y: j})
			i++
			j++
		case table[(i+1)*width+j] >= table[i*width+j+1]:
			i++
		default:
			j++
		}
	}
	return out
}

func anchors(a, b []string) []pair {
	const (
		onceInA   = -1
		onceInB   = -4
		uniqueAll = onceInA + onceInB
	)
	counts := map[string]int{}
	for _, line := range a {
		if c := counts[line]; c > 2*onceInA {
			counts[line] = c + onceInA
		}
	}
	for _, line := range b {
		if c := counts[line]; c > 2*onceInB {
			counts[line] = c + onceInB
		}
	}
	var bIndex []int
	for i, line := range b {
		if counts[line] == uniqueAll {
			counts[line] = len(bIndex)
			bIndex = append(bIndex, i)
		}
	}
	var aIndex, order []int
	for i, line := range a {
		if j, ok := counts[line]; ok && j >= 0 {
			aIndex = append(aIndex, i)
			order = append(order, j)
		}
	}
	n := len(order)
	tails := make([]int, n)
	lengths := make([]int, n)
	for i := range tails {
		tails[i] = n + 1
	}
	for i, j := range order {
		k := sort.SearchInts(tails, j)
		tails[k] = j
		lengths[i] = k + 1
	}
	longest := 0
	for _, l := range lengths {
		longest = max(longest, l)
	}
	seq := make([]pair, longest)
	k, last := longest, n
	for i := n - 1; i >= 0 && k > 0; i-- {
		if lengths[i] == k && order[i] < last {
			seq[k-1] = pair{x: aIndex[i], y: bIndex[order[i]]}
			last = order[i]
			k--
		}
	}
	return seq
}
