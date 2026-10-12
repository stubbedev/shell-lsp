package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type category uint8

const (
	unclassified category = iota
	posixExtension
	otherDialect
	runtimeCheck
	parenAmbiguity
	stricterByDesign
	parserGap
)

var categoryNames = [...]string{
	unclassified:     "unclassified",
	posixExtension:   "posix-extension",
	otherDialect:     "other-dialect",
	runtimeCheck:     "runtime-check",
	parenAmbiguity:   "paren-ambiguity",
	stricterByDesign: "stricter-by-design",
	parserGap:        "parser-gap",
}

func (c category) String() string { return categoryNames[c] }

func named[T ~uint8](names []string, name string) (T, bool) {
	i := slices.Index(names, name)
	return T(i), i >= 0
}

type classified struct {
	difference
	category category
}

type shellsBaseline struct {
	parses  int
	entries []classified
}

const shellsCountPrefix = "# parses compared: "

func renderShellsBaseline(b shellsBaseline) string {
	var out strings.Builder
	out.WriteString("# Known differences between the parser and the real shells' -n, written by\n")
	out.WriteString("# `just corpus-baseline` and classified by hand as docs/corpus.md describes.\n")
	out.WriteString("# Each line: category shell verdict script[#case]\n")
	out.WriteString(shellsCountPrefix + strconv.Itoa(b.parses) + "\n")
	for _, e := range b.entries {
		fmt.Fprintf(&out, "%s %s\n", e.category, e.key())
	}
	return out.String()
}

func parseShellsBaseline(text string) (shellsBaseline, error) {
	var b shellsBaseline
	counted := false
	for n, line := range strings.Split(text, "\n") {
		if rest, ok := strings.CutPrefix(line, shellsCountPrefix); ok {
			parses, err := strconv.Atoi(rest)
			if err != nil {
				return shellsBaseline{}, fmt.Errorf("line %d: parse count %q: %w", n+1, rest, err)
			}
			b.parses, counted = parses, true
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		e, err := parseClassified(line)
		if err != nil {
			return shellsBaseline{}, fmt.Errorf("line %d: %w", n+1, err)
		}
		b.entries = append(b.entries, e)
	}
	if !counted {
		return shellsBaseline{}, fmt.Errorf("no %q line", strings.TrimSpace(shellsCountPrefix))
	}
	return b, nil
}

func parseClassified(line string) (classified, error) {
	fields := strings.SplitN(line, " ", 4)
	if len(fields) != 4 {
		return classified{}, fmt.Errorf("want category, shell, verdict and script, got %q", line)
	}
	cat, ok := named[category](categoryNames[:], fields[0])
	if !ok {
		return classified{}, fmt.Errorf("unknown category %q in %q", fields[0], line)
	}
	shell, ok := named[realShell](realShellNames(), fields[1])
	if !ok {
		return classified{}, fmt.Errorf("unknown shell %q in %q", fields[1], line)
	}
	v, ok := named[verdict](verdictNames[:], fields[2])
	if !ok {
		return classified{}, fmt.Errorf("unknown verdict %q in %q", fields[2], line)
	}
	return classified{id: fields[3], shell: shell, verdict: v, category: cat}, nil
}

func reclassify(want shellsBaseline, parses int, got []difference) shellsBaseline {
	known := map[string]category{}
	for _, e := range want.entries {
		known[e.key()] = e.category
	}
	out := shellsBaseline{parses: parses, entries: make([]classified, 0, len(got))}
	for _, d := range got {
		out.entries = append(out.entries, classified{difference: d, category: known[d.key()]})
	}
	return out
}

func diffShells(want shellsBaseline, parses int, got []difference) []string {
	var out []string
	if want.parses != parses {
		out = append(out, fmt.Sprintf("corpus: the baseline compared %d parses, now %d", want.parses, parses))
	}
	expected := map[string]bool{}
	for _, e := range want.entries {
		expected[e.key()] = true
		if e.category == unclassified {
			out = append(out, "unclassified: "+e.key())
		}
	}
	found := map[string]bool{}
	for _, d := range got {
		found[d.key()] = true
		if !expected[d.key()] {
			out = append(out, fmt.Sprintf("NEW: %s: %s", d.key(), d.detail))
		}
	}
	for _, e := range want.entries {
		if !found[e.key()] {
			out = append(out, "gone, refresh the baseline: "+e.key())
		}
	}
	return out
}
