package textedit

import "testing"

func TestApplySortsEdits(t *testing.T) {
	t.Parallel()

	got := Apply("hello world", []Edit{{Start: 6, End: 11, NewText: "there"}, {Start: 0, End: 0, NewText: ">> "}})
	if got != ">> hello there" {
		t.Errorf("Apply() = %q", got)
	}
}

func TestApplyInsertsBeforeAReplacementAtTheSameOffset(t *testing.T) {
	t.Parallel()

	edits := NonOverlapping([][]Edit{
		{{Start: 5, End: 11, NewText: "$(date)"}},
		{{Start: 5, End: 5, NewText: `"`}, {Start: 11, End: 11, NewText: `"`}},
	})
	if got := Apply("echo `date`", edits); got != `echo "$(date)"` {
		t.Errorf("Apply() = %q", got)
	}
}

func TestNonOverlappingKeepsFirstGroups(t *testing.T) {
	t.Parallel()

	got := NonOverlapping([][]Edit{
		{{Start: 0, End: 0, NewText: `"`}, {Start: 2, End: 2, NewText: `"`}},
		{{Start: 1, End: 3, NewText: "x"}},
		{{Start: 0, End: 0, NewText: "dup"}},
		{{Start: 5, End: 6, NewText: "y"}},
	})
	if len(got) != 3 || got[2].NewText != "y" {
		t.Errorf("NonOverlapping() = %+v", got)
	}
}
