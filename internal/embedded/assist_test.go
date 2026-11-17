package embedded

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/parser"
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
		{"a sed command", "sed -e 'p;' f\n", "p;", 2, "s", "loop"},
		{"a sed command at the start of the script", "sed -e 'p' f\n", "p'", 0, "s", "loop"},
		{"a sed label from another expression", "sed -e ':loop' -e 'b l' f\n", "b l", 3, "loop", "s"},
		{"a basic sed regex", "sed 's/a' f\n", "s/a", 3, `\+`, "+"},
		{"an extended sed regex", "sed -E 's/a' f\n", "s/a", 3, "+", `\+`},
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
	if items := CompletionsAt(find(t, "sed -e 's/a/b/' f\n"), 13); items != nil {
		t.Errorf("the replacement of a sed substitution has nothing to complete, yet %+v", items)
	}
}

func TestSedCompletionItems(t *testing.T) {
	t.Parallel()

	src := "sed -e ':loop' -e 'p;b lo' f\n"
	cursor := strings.Index(src, "b lo") + len("b lo")
	items := CompletionsAt(find(t, src), cursor)
	if len(items) != 1 || items[0].Label != "loop" || items[0].Kind != LabelItem || items[0].Detail != ":loop" {
		t.Fatalf("items = %+v, want the label loop", items)
	}
	if want := (parser.Span{Start: cursor - len("lo"), End: cursor}); items[0].Span != want {
		t.Errorf("label span = %+v, want %+v over the typed prefix", items[0].Span, want)
	}
	src = "sed -n '/x/' f\n"
	cursor = strings.Index(src, "/'") + 1
	items = CompletionsAt(find(t, src), cursor)
	i := slices.IndexFunc(items, func(it Item) bool { return it.Label == "p" })
	if i < 0 || items[i].Kind != CommandItem || items[i].Doc == "" || items[i].Span != (parser.Span{Start: cursor, End: cursor}) {
		t.Errorf("commands after an address = %+v, want a documented p inserted at the cursor", items)
	}
	src = "sed -E 's/a\\' f\n"
	cursor = strings.Index(src, "' f")
	items = CompletionsAt(find(t, src), cursor)
	i = slices.IndexFunc(items, func(it Item) bool { return it.Label == `\w` })
	if i < 0 || items[i].Kind != OperatorItem || items[i].Span != (parser.Span{Start: cursor - 1, End: cursor}) {
		t.Errorf("operators after a backslash = %+v, want \\w replacing the backslash", items)
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
