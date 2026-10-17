package justfile

import (
	"slices"
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
	if !slices.Equal(ops, []string{"+", "||", "&&"}) {
		t.Errorf("operators = %v", ops)
	}
}
