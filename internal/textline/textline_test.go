package textline

import (
	"slices"
	"testing"
)

func TestBoundsFindTheLineAroundAnOffset(t *testing.T) {
	t.Parallel()

	const text = "one\ntwo\n\nlast"
	for _, tc := range []struct {
		offset, start, end, next int
	}{
		{offset: 0, start: 0, end: 3, next: 4},
		{offset: 3, start: 0, end: 3, next: 4},
		{offset: 4, start: 4, end: 7, next: 8},
		{offset: 8, start: 8, end: 8, next: 9},
		{offset: 9, start: 9, end: 13, next: 13},
		{offset: 13, start: 9, end: 13, next: 13},
	} {
		start, end := Bounds(text, tc.offset)
		if start != tc.start || end != tc.end {
			t.Errorf("Bounds(%d) = %d, %d, want %d, %d", tc.offset, start, end, tc.start, tc.end)
		}
		if start != Start(text, tc.offset) || end != End(text, tc.offset) {
			t.Errorf("Bounds(%d) disagrees with Start and End", tc.offset)
		}
		if next := Next(text, tc.offset); next != tc.next {
			t.Errorf("Next(%d) = %d, want %d", tc.offset, next, tc.next)
		}
	}
}

func TestOwnsAcceptsOnlyBlankLineRemainders(t *testing.T) {
	t.Parallel()

	const text = "  echo a; echo b \nx=1"
	for _, tc := range []struct {
		start, end int
		want       bool
	}{
		{start: 2, end: 16, want: true},
		{start: 2, end: 8, want: false},
		{start: 10, end: 16, want: false},
		{start: 18, end: 21, want: true},
	} {
		if got := Owns(text, tc.start, tc.end); got != tc.want {
			t.Errorf("Owns(%d, %d) = %v, want %v", tc.start, tc.end, got, tc.want)
		}
	}
}

func TestCommentsAboveWalksUpToTheFirstCodeLine(t *testing.T) {
	t.Parallel()

	const text = "echo\n# a\n  # b\nf() {"
	var got [][2]int
	for start, end := range CommentsAbove(text, 15) {
		got = append(got, [2]int{start, end})
	}
	if want := [][2]int{{9, 14}, {5, 8}}; !slices.Equal(got, want) {
		t.Errorf("CommentsAbove = %v, want %v", got, want)
	}
}

func TestCommentBlockStartStopsAtTheShebang(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		text      string
		lineStart int
		want      int
	}{
		{text: "#!/bin/sh\n# doc\nf() {", lineStart: 16, want: 10},
		{text: "# one\n# two\nf() {", lineStart: 12, want: 0},
		{text: "echo\n\n# doc\nf() {", lineStart: 12, want: 6},
		{text: "f() {", lineStart: 0, want: 0},
	} {
		if got := CommentBlockStart(tc.text, tc.lineStart); got != tc.want {
			t.Errorf("CommentBlockStart(%q, %d) = %d, want %d", tc.text, tc.lineStart, got, tc.want)
		}
	}
}
