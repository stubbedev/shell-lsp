package justfile

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestCommentsAreRecordedOnceInOrder(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"version := \"1.0    # unterminated\n",
		"x := (\n  'a' + # one\n  'b' # two\n)\n# three\na: # four\n    echo\n",
		"a: (b # c\n  'x')\n",
	} {
		f := Parse(text)
		starts := make([]int, len(f.Comments))
		for i, c := range f.Comments {
			starts[i] = c.Span.Start
		}
		if !slices.IsSorted(starts) || len(slices.Compact(slices.Clone(starts))) != len(starts) {
			t.Errorf("Parse(%q) comments start at %v, want strictly increasing offsets", text, starts)
		}
	}
}

func TestParseRecoversAfterAnError(t *testing.T) {
	t.Parallel()

	f := Parse("x := (\nbad line here\n  echo\nok := \"1\"\nbuild:\n    echo {{ok}}\nalias b := := x\nafter:\n")
	if len(f.Errors) < 2 {
		t.Fatalf("errors = %+v, want each broken item reported", f.Errors)
	}
	var names []string
	for _, it := range f.Items {
		switch it.Kind {
		case ItemAssignment:
			names = append(names, it.Assignment.Name.Text)
		case ItemRecipe:
			names = append(names, it.Recipe.Name.Text)
		case ItemSetting, ItemUnexport, ItemAlias, ItemImport, ItemModule, ItemFunction:
		}
	}
	if !slices.Contains(names, "ok") || !slices.Contains(names, "build") || !slices.Contains(names, "after") {
		t.Errorf("items after the errors = %v", names)
	}
}

func TestParseKeepsEveryExpressionSpan(t *testing.T) {
	t.Parallel()

	text := `x := "a" + 'b' && c || !d`
	f := Parse("set unstable\nset lists\n" + text + "\n")
	if len(f.Errors) > 0 {
		t.Fatal(f.Errors)
	}
	var ops []string
	Walk(f.Assignments()[0].Value, func(e *Expr) {
		if e.Kind == ExprBinary {
			ops = append(ops, f.Text[e.OpSpan.Start:e.OpSpan.End])
		}
	})
	if !slices.Equal(ops, []string{"||", "&&", "+"}) {
		t.Errorf("operators = %v", ops)
	}
}

func TestSyntaxErrorsLandWhereJustReportsThem(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ text, at string }{
		{"f\"\n0{{", "1:2"},
		{"f\"{{ 'a' }}\"\n", "1:2"},
		{"f\"\n0{{x}}\"\na:\n", "1:2"},
		{"a f\"{{\n1 }}\":\n", "2:1"},
		{"a: f\"{{\n1 }}\"\n", "2:1"},
		{"x := f\"\nab", "1:7"},
		{"x := x\"\nab", "1:7"},
		{"x := f\"\\q\"\n", "1:7"},
		{"x := f\"{{ 'a' }}\n", "1:15"},
		{"x := f\"{{ 'a'\n}}\ny := 1\n", "2:1"},
		{"x := f\"{{ 'a'\n}}\"\ny := \"b", "3:6"},
		{"x := f\"a{{\n\"", "2:1"},
		{"x := f\"{{ f\"{{ 'a\n' }}\n\" }}\"\ny := 1 2\n", "4:6"},
		{"import f\"x\"\n", "1:8"},
		{"a:\n  echo {{ if 'a' == 'b' { 'c' } else { 'd' }}\n", "2:45"},
		{"x := if 'a' == 'b' { 'c' }} else { 'd' }\n", "1:27"},
	} {
		var got []string
		for _, e := range Parse(tc.text).Errors {
			start := strings.LastIndexByte(tc.text[:e.Span.Start], '\n') + 1
			got = append(got, fmt.Sprintf("%d:%d", strings.Count(tc.text[:start], "\n")+1, e.Span.Start-start+1))
		}
		if !slices.Contains(got, tc.at) {
			t.Errorf("Parse(%q) reports errors at %v, want one at %s where just reports it", tc.text, got, tc.at)
		}
	}
}

func TestParseAcceptsWhatJustAccepts(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"a:\n  echo {{ if 'a' == 'b' { 'c' } else { 'd' }}}\n",
		"x := f\"{{ if 'a' == 'b' { 'c' } else { 'd' }}}\"\n",
		"x := f\"{{ f\"{{ 'a' }}\" }}\"\n",
		"x := f\"{{ 'a' }}\n}}\"\nb:\n",
		"x := f\"a\nb\"\n",
		"import x \"a\"\n",
		"mod a x \"b\"\n",
		"a: (b x\"a\" f\"{{'c'}}\")\nb x y:\n",
	} {
		if f := Parse(text); len(f.Errors) > 0 {
			t.Errorf("Parse(%q) = %+v, want no errors as just accepts it", text, f.Errors)
		}
	}
}

func TestPathsMayBeShellExpandedAfterWhitespace(t *testing.T) {
	t.Parallel()

	f := Parse("import x \"a\"\nmod b x \"c\"\n")
	var paths []*Expr
	for _, it := range f.Items {
		switch it.Kind {
		case ItemImport:
			paths = append(paths, it.Import.Path)
		case ItemModule:
			paths = append(paths, it.Module.Path)
		case ItemSetting, ItemUnexport, ItemAlias, ItemAssignment, ItemRecipe, ItemFunction:
		}
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %v, want the import and the module path", paths)
	}
	for _, p := range paths {
		if text := f.Text[p.Span.Start:p.Span.End]; p.Str.Prefix != 'x' || text[0] != 'x' {
			t.Errorf("path %q has prefix %q, want a shell-expanded string spanning its `x`", text, p.Str.Prefix)
		}
	}
}

func TestUnindentMatchesJust(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ in, want string }{
		{"foo", "foo"},
		{"foo\nbar\nbaz\n", "foo\nbar\nbaz\n"},
		{"", ""},
		{"  foo\n  bar", "foo\nbar"},
		{"  foo\n  bar\n\n", "foo\nbar\n"},
		{"  foo\r\n\r\n  bar\r\n", "foo\r\n\r\nbar\r\n"},
		{"\n          hello\n          bar\n        ", "hello\nbar\n"},
		{"hello\n  bar\n  foo", "hello\n  bar\n  foo"},
		{"\n\n          hello\n          bar\n\n        ", "\nhello\nbar\n\n"},
		{"0\n  \r\r\n0", "0\n\r\n0"},
		{"a\n \t\r\nb", "a\n\r\nb"},
		{"a\n\r \nb", "a\n\nb"},
		{"  a\n\r\r\n  b\n  ", "a\n\r\nb\n"},
		{" \r\n", ""},
		{"\r\n\r\n", ""},
		{"\n\n\n", "\n"},
	} {
		if got := unindent(tt.in); got != tt.want {
			t.Errorf("unindent(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
