package gitignore

import (
	"path"
	"slices"
	"strings"
)

type Rules []rule

type rule struct {
	segments []string
	negate   bool
	dirOnly  bool
	anchored bool
}

func Parse(text string) Rules {
	var rules Rules
	for line := range strings.Lines(text) {
		if r, ok := parseLine(line); ok {
			rules = append(rules, r)
		}
	}
	return rules
}

func parseLine(line string) (rule, bool) {
	line = strings.TrimRight(line, "\r\n")
	line = trimUnescapedSpaces(line)
	if line == "" || line[0] == '#' {
		return rule{}, false
	}
	var r rule
	if line[0] == '!' {
		r.negate = true
		line = line[1:]
	}
	if trimmed, ok := strings.CutSuffix(line, "/"); ok {
		r.dirOnly = true
		line = trimmed
	}
	r.anchored = strings.Contains(line, "/")
	line = strings.TrimPrefix(line, "/")
	if line == "" {
		return rule{}, false
	}
	r.segments = strings.Split(line, "/")
	for i, segment := range r.segments {
		if segment == "**" && r.anchored {
			continue
		}
		segment = fnmatchToGo(segment)
		if _, err := path.Match(segment, ""); err != nil {
			return rule{}, false
		}
		r.segments[i] = segment
	}
	return r, true
}

func trimUnescapedSpaces(line string) string {
	end := len(line)
	for end > 0 && line[end-1] == ' ' {
		if end > 1 && line[end-2] == '\\' {
			break
		}
		end--
	}
	return line[:end]
}

func fnmatchToGo(segment string) string {
	if !strings.Contains(segment, "[!") {
		return segment
	}
	var out strings.Builder
	out.Grow(len(segment))
	for i := 0; i < len(segment); i++ {
		c := segment[i]
		out.WriteByte(c)
		switch {
		case c == '\\' && i+1 < len(segment):
			i++
			out.WriteByte(segment[i])
		case c == '[' && i+1 < len(segment) && segment[i+1] == '!':
			i++
			out.WriteByte('^')
		}
	}
	return out.String()
}

func (rules Rules) decide(rel string, isDir bool) (ignored, decided bool) {
	name := rel[strings.LastIndexByte(rel, '/')+1:]
	for _, r := range slices.Backward(rules) {
		if r.dirOnly && !isDir {
			continue
		}
		if r.anchored && matchSegments(r.segments, rel) || !r.anchored && matchSegment(r.segments[0], name) {
			return !r.negate, true
		}
	}
	return false, false
}

func matchSegments(segments []string, rel string) bool {
	for len(segments) > 0 {
		if segments[0] == "**" {
			rest := segments[1:]
			if len(rest) == 0 {
				return rel != ""
			}
			for {
				if matchSegments(rest, rel) {
					return true
				}
				_, after, found := strings.Cut(rel, "/")
				if !found {
					return false
				}
				rel = after
			}
		}
		if rel == "" {
			return false
		}
		head, after, _ := strings.Cut(rel, "/")
		if !matchSegment(segments[0], head) {
			return false
		}
		segments, rel = segments[1:], after
	}
	return rel == ""
}

func matchSegment(pattern, name string) bool {
	ok, _ := path.Match(pattern, name)
	return ok
}

type Stack struct {
	parent *Stack
	base   string
	rules  Rules
}

func (s *Stack) Push(base string, rules Rules) *Stack {
	if len(rules) == 0 {
		return s
	}
	return &Stack{parent: s, base: base, rules: rules}
}

func (s *Stack) Ignored(rel string, isDir bool) bool {
	for layer := s; layer != nil; layer = layer.parent {
		sub := rel
		if layer.base != "" {
			sub = rel[len(layer.base)+1:]
		}
		if ignored, decided := layer.rules.decide(sub, isDir); decided {
			return ignored
		}
	}
	return false
}
