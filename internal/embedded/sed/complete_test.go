package sed

import (
	"slices"
	"strings"
	"testing"
)

const cursorMark = "|"

func completeAtMark(t *testing.T, marked string, syntax Syntax) (Completion, bool) {
	t.Helper()
	at := strings.Index(marked, cursorMark)
	if at < 0 {
		t.Fatalf("%q has no cursor mark", marked)
	}
	return Complete(marked[:at]+marked[at+len(cursorMark):], syntax, at)
}

func labelsOf(c Completion) []string {
	out := make([]string, len(c.Candidates))
	for i, candidate := range c.Candidates {
		out[i] = candidate.Label
	}
	return out
}

func TestCompletionContexts(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, marked string
		syntax       Syntax
		context      Context
		start        int
		has, not     []string
	}{
		{"an empty script", "|", Basic, Commands, 0, []string{"s", "p", "b", ":", "="}, []string{"L"}},
		{"after a semicolon", "p;|", Basic, Commands, 2, []string{"d"}, nil},
		{"after a semicolon and a blank", "p; |", Basic, Commands, 3, []string{"d"}, nil},
		{"on a new line", "p\n|", Basic, Commands, 2, []string{"d"}, nil},
		{"after a regex address", "/x/|", Basic, Commands, 3, []string{"d"}, nil},
		{"after a line range", "1,3|", Basic, Commands, 3, []string{"d"}, nil},
		{"after a negation", "/x/!|", Basic, Commands, 4, []string{"d"}, nil},
		{"inside a block", "/x/{|", Basic, Commands, 4, []string{"d"}, nil},
		{"after a substitution and a semicolon", "s/a/b/g;|", Basic, Commands, 8, []string{"y"}, nil},
		{"after a label and a semicolon", ":a;|", Basic, Commands, 3, []string{"b"}, nil},
		{"a basic regex", "s/|", Basic, Operators, 2, []string{"*", `\+`, `\?`, `\{`, `\(`, `\|`, ".", "^", "$", "[", `\w`}, []string{"+", "?", "{", "(", "|"}},
		{"an extended regex", "s/a|", Extended, Operators, 3, []string{"*", "+", "?", "{", "(", "|", `\b`}, []string{`\+`, `\{`, `\(`}},
		{"an address regex", "/fo|", Basic, Operators, 3, []string{`\{`}, nil},
		{"a custom address delimiter", `\,fo|`, Extended, Operators, 4, []string{"{"}, nil},
		{"after an escaped delimiter", `s/a\/|`, Basic, Operators, 5, []string{"*"}, nil},
		{"a pending escape in a basic regex", `s/a\|`, Basic, Operators, 3, []string{`\(`, `\{`, `\w`}, []string{"*", "("}},
		{"a pending escape in an extended regex", `s/a\|`, Extended, Operators, 3, []string{`\w`, `\<`}, []string{"(", `\(`}},
		{"a branch", ":top;:end;b |", Basic, Labels, 12, []string{"top", "end"}, nil},
		{"a branch right after the command", ":a;t|", Basic, Labels, 4, []string{"a"}, nil},
		{"a label defined later", "b e|;s/a/b/;:end;:else;:x;:end", Basic, Labels, 2, []string{"end", "else"}, []string{"x"}},
		{"labels from every expression line", ":one\nT |\n:two", Basic, Labels, 7, []string{"one", "two"}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, ok := completeAtMark(t, tt.marked, tt.syntax)
			if !ok || c.Context != tt.context || c.Start != tt.start {
				t.Fatalf("Complete(%q) = context %d at %d, %v; want context %d at %d", tt.marked, c.Context, c.Start, ok, tt.context, tt.start)
			}
			labels := labelsOf(c)
			for _, want := range tt.has {
				if !slices.Contains(labels, want) {
					t.Errorf("Complete(%q) = %q, want %q among them", tt.marked, labels, want)
				}
			}
			for _, unwanted := range tt.not {
				if slices.Contains(labels, unwanted) {
					t.Errorf("Complete(%q) = %q, want no %q", tt.marked, labels, unwanted)
				}
			}
			if slices.ContainsFunc(c.Candidates, func(c Candidate) bool { return c.Doc.Signature == "" }) {
				t.Errorf("Complete(%q) offers a candidate without a signature: %+v", tt.marked, c.Candidates)
			}
		})
	}
}

func TestNoCompletionOutsideACompletableContext(t *testing.T) {
	t.Parallel()

	for _, marked := range []string{
		"p|", "p |", "# a comment|", "# a comment;|", "a text|", "a text;|", "w out;|", "s/a/b/g|", "s/a/b/ |",
		"1~|", "1,|", "s/a/|", "s/a/b|", "s/[a-|", `s/[a\|`, "/[[:al|", "y/ab|", ":a|", "b foo |", "{p}|", "s|", "e echo;|",
	} {
		if c, ok := completeAtMark(t, marked, Basic); ok {
			t.Errorf("Complete(%q) = %+v, want nothing", marked, c)
		}
	}
}

func TestCompletionFiltersByWhatIsTyped(t *testing.T) {
	t.Parallel()

	c, ok := completeAtMark(t, ":loop;:lap;:end;b lo|", Basic)
	if got := labelsOf(c); !ok || !slices.Equal(got, []string{"loop"}) {
		t.Errorf("labels = %q, %v; want only loop", got, ok)
	}
	c, ok = completeAtMark(t, ":a;:a;b |", Basic)
	if got := labelsOf(c); !ok || !slices.Equal(got, []string{"a"}) {
		t.Errorf("labels of a label defined twice = %q, %v; want it once", got, ok)
	}
	c, ok = completeAtMark(t, "b |", Basic)
	if got := labelsOf(c); !ok || len(got) != 0 {
		t.Errorf("labels of a script without any = %q, %v; want none", got, ok)
	}
}

func FuzzComplete(f *testing.F) {
	for _, src := range validScripts {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		for at := range len(src) + 1 {
			for _, syntax := range []Syntax{Basic, Extended} {
				if c, ok := Complete(src, syntax, at); ok && (c.Start < 0 || c.Start > at) {
					t.Fatalf("Complete(%q, %d) starts at %d", src, at, c.Start)
				}
			}
		}
	})
}
