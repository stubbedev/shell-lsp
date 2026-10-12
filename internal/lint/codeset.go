package lint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/directive"
)

type codeRange struct {
	from, to Code
}

func (r codeRange) has(code Code) bool { return r.from <= code && code < r.to }

type CodeSet struct {
	all    bool
	ranges []codeRange
}

func CodesOf(codes ...Code) CodeSet {
	set := CodeSet{ranges: make([]codeRange, len(codes))}
	for i, code := range codes {
		set.ranges[i] = codeRange{from: code, to: code + 1}
	}
	return set
}

func ParseCodeSet(list string) (CodeSet, error) {
	var set CodeSet
	for item := range strings.SplitSeq(list, ",") {
		one, err := parseCodeItem(item)
		if err != nil {
			return CodeSet{}, err
		}
		set = set.Union(one)
	}
	return set, nil
}

func parseCodeItem(item string) (CodeSet, error) {
	item = strings.TrimSpace(item)
	if item == directive.All {
		return CodeSet{all: true}, nil
	}
	from, to, isRange := strings.Cut(item, "-")
	first, err := ParseCode(from)
	if err != nil {
		return CodeSet{}, err
	}
	if !isRange {
		return CodesOf(first), nil
	}
	last, err := ParseCode(to)
	if err != nil {
		return CodeSet{}, fmt.Errorf("range %q: %w", item, err)
	}
	return CodeSet{ranges: []codeRange{{from: first, to: last}}}, nil
}

func (s CodeSet) Has(code Code) bool {
	return s.all || slices.ContainsFunc(s.ranges, func(r codeRange) bool { return r.has(code) })
}

func (s CodeSet) Union(other CodeSet) CodeSet {
	return CodeSet{all: s.all || other.all, ranges: slices.Concat(s.ranges, other.ranges)}
}

func (s CodeSet) single() (Code, bool) {
	if s.all || len(s.ranges) != 1 || s.ranges[0].to-s.ranges[0].from != 1 {
		return 0, false
	}
	return s.ranges[0].from, true
}
