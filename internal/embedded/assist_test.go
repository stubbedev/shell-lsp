package embedded

import (
	"slices"
	"strings"
	"testing"
)

func TestCompletionsInsideAProgram(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, at string
		delta         int
		has           string
		not           string
	}{
		{"an awk function", "awk 'BEGIN { x = subs(\"a\", 1) }' f\n", "subs(", 4, "substr", "length"},
		{"a variable set by a flag", "awk -v limit=3 'BEGIN { print lim }' f\n", "lim }", 3, "limit", "substr"},
		{"a jq function", "jq 'map_v(.a)' f\n", "map_v", 5, "map_values", "select"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			items := CompletionsAt(find(t, tt.src), strings.Index(tt.src, tt.at)+tt.delta)
			labels := make([]string, len(items))
			for i, it := range items {
				labels[i] = it.Label
			}
			if !slices.Contains(labels, tt.has) || slices.Contains(labels, tt.not) {
				t.Errorf("labels at %q = %q, want %s and not %s", tt.at, labels, tt.has, tt.not)
			}
		})
	}
	if items := CompletionsAt(find(t, "sed -e 's/a/b/' f\n"), 8); items != nil {
		t.Errorf("sed has no names to complete, yet %+v", items)
	}
}

func TestSignatureInsideAProgram(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, at string
		delta         int
		label         string
		active        int
		ok            bool
	}{
		{"the first awk argument", "awk 'BEGIN { x = substr(a, 1) }' f\n", "substr(a", 7, "substr(s, m [, n])", 0, true},
		{"the second awk argument", "awk 'BEGIN { x = substr(a, 1) }' f\n", ", 1", 2, "substr(s, m [, n])", 1, true},
		{"past a nested call", "awk 'BEGIN { x = substr(length(a), 1) }' f\n", ", 1", 2, "substr(s, m [, n])", 1, true},
		{"a jq argument after a semicolon", "jq 'setpath(.a; 1)' f\n", "; 1", 2, "setpath(path; value)", 1, true},
		{"the jq spelling with arguments", "jq 'paths(.a)' f\n", "(.a", 1, "paths(f)", 0, true},
		{"outside a call", "awk 'BEGIN { x = 1 }' f\n", "= 1", 2, "", 0, false},
		{"a group in parentheses", "awk 'BEGIN { x = (1) }' f\n", "(1", 1, "", 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sig, ok := SignatureAt(find(t, tt.src), strings.Index(tt.src, tt.at)+tt.delta)
			if ok != tt.ok || sig.Label != tt.label || sig.Active != tt.active {
				t.Errorf("signature = %+v, %v", sig, ok)
			}
			for _, p := range sig.Params {
				if p.Start < 0 || p.End > len(sig.Label) || p.End <= p.Start {
					t.Errorf("parameter %+v does not fit %q", p, sig.Label)
				}
			}
		})
	}
}
