package corpus

import (
	"slices"
	"testing"
)

const oilsSpec = `## oils_failures_allowed: 2
## compare_shells: dash bash mksh

#### Here string
cat <<< 'hi'
## STDOUT:
hi
## END
## N-I dash stdout-json: ""
## N-I dash status: 2

#### Two lines
# a comment stays
echo a
echo b
## OK bash STDOUT:
x
## END
## stdout: a

#### Last
true
`

func TestParseOilsSpecSplitsCasesAndDropsExpectations(t *testing.T) {
	t.Parallel()

	cases := ParseOilsSpec("oils/spec/x.test.sh", oilsSpec)
	want := []SpecCase{
		{Path: "oils/spec/x.test.sh", Index: 1, Line: 4, Code: "cat <<< 'hi'\n\n"},
		{Path: "oils/spec/x.test.sh", Index: 2, Line: 12, Code: "# a comment stays\necho a\necho b\n\n"},
		{Path: "oils/spec/x.test.sh", Index: 3, Line: 21, Code: "true\n\n"},
	}
	if len(cases) != len(want) {
		t.Fatalf("got %d cases, want %d: %+v", len(cases), len(want), cases)
	}
	for i, c := range cases {
		w := want[i]
		if c.Path != w.Path || c.Index != w.Index || c.Line != w.Line || c.Code != w.Code {
			t.Errorf("case %d = %+v, want %+v", i, c, w)
		}
		if !slices.Equal(c.Shells, []string{"dash", "bash", "mksh"}) {
			t.Errorf("case %d runs with %v, want the compare_shells list", i, c.Shells)
		}
	}
	if got := cases[1].ID(); got != "oils/spec/x.test.sh#2" {
		t.Errorf("ID = %s", got)
	}
}
