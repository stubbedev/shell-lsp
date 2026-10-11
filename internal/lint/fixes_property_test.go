package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const fixAllRounds = 6

func TestEveryFixKeepsFixturesParsing(t *testing.T) {
	t.Parallel()

	for _, c := range loadFixtures(t) {
		d, source := fixtureDialect(c.script)
		opts := Options{Optional: []string{directive.All}, DialectSource: source}
		tree := parser.Parse(c.script, d)
		baseline := len(tree.Errors)
		for _, f := range Check(tree, nil, opts) {
			for _, fix := range f.Fixes {
				if fix.Title == "" || (fix.Safety != Safe && fix.Safety != Review) || len(fix.Edits) == 0 {
					t.Errorf("%s: %s offers a malformed fix %+v", c.id(), f.Code, fix)
					continue
				}
				if !inBounds(fix.Edits, len(c.script)) {
					t.Errorf("%s: %s fix %q has edits outside the script %+v", c.id(), f.Code, fix.Title, fix.Edits)
					continue
				}
				if overlapping(fix.Edits) {
					t.Errorf("%s: %s fix %q has overlapping edits %+v", c.id(), f.Code, fix.Title, fix.Edits)
					continue
				}
				out := textedit.Apply(c.script, fix.Edits)
				if errs := parser.Parse(out, d).Errors; len(errs) > baseline {
					t.Errorf("%s: %s fix %q introduced %q:\n%s", c.id(), f.Code, fix.Title, errs[0].Message, out)
				}
			}
		}
	}
}

func TestFixAllConvergesOnFixtures(t *testing.T) {
	t.Parallel()

	for _, c := range loadFixtures(t) {
		d, source := fixtureDialect(c.script)
		opts := Options{Optional: []string{directive.All}, DialectSource: source}
		original := parser.Parse(c.script, d)
		_, formatErr := format.Format(original, format.Options{})
		text := c.script
		converged := false
		for range fixAllRounds {
			tree := parser.Parse(text, d)
			edits := SafeEdits(fixSets(Check(tree, nil, opts)))
			if len(edits) == 0 {
				converged = true
				break
			}
			if !inBounds(edits, len(text)) {
				t.Fatalf("%s: fix all produced edits outside the script %+v", c.id(), edits)
			}
			text = textedit.Apply(text, edits)
		}
		if !converged {
			t.Errorf("%s: fix all still changes the script after %d rounds:\n%s", c.id(), fixAllRounds, text)
			continue
		}
		fixed := parser.Parse(text, d)
		if len(fixed.Errors) > len(original.Errors) {
			t.Errorf("%s: fix all introduced %q:\n%s", c.id(), fixed.Errors[0].Message, text)
			continue
		}
		if _, err := format.Format(fixed, format.Options{}); err != nil && formatErr == nil {
			t.Errorf("%s: the fixed script no longer formats: %v\n%s", c.id(), err, text)
		}
	}
}

func fixSets(findings []Finding) [][]Fix {
	out := make([][]Fix, len(findings))
	for i, f := range findings {
		out[i] = f.Fixes
	}
	return out
}

func inBounds(edits []textedit.Edit, size int) bool {
	return !slices.ContainsFunc(edits, func(e textedit.Edit) bool { return e.Start < 0 || e.End < e.Start || e.End > size })
}

func overlapping(edits []textedit.Edit) bool {
	sorted := slices.SortedFunc(slices.Values(edits), func(a, b textedit.Edit) int { return a.Start - b.Start })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Start < sorted[i-1].End || (sorted[i].Start == sorted[i-1].Start && sorted[i].End == sorted[i].Start && sorted[i-1].End == sorted[i-1].Start) {
			return true
		}
	}
	return false
}
