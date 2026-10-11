package lang

import "testing"

func TestNestingMatchesClosers(t *testing.T) {
	t.Parallel()

	var n Nesting[byte]
	if n.Pop(')') {
		t.Error("Pop on an empty nesting succeeded")
	}
	n.Push(')')
	n.Push(']')
	if top, ok := n.Top(); !ok || top != ']' || n.Depth() != 2 {
		t.Errorf("Top = %q, %v at depth %d, want ] at depth 2", top, ok, n.Depth())
	}
	if n.Pop(')') {
		t.Error("Pop(')') succeeded while ] is open")
	}
	if !n.Pop(']') || !n.Pop(')') || n.Depth() != 0 {
		t.Errorf("Pop in order failed, depth %d", n.Depth())
	}
	if _, ok := n.Top(); ok {
		t.Error("Top of an empty nesting is known")
	}
}

func TestNestingBeyondItsCapacityIsUntracked(t *testing.T) {
	t.Parallel()

	var n Nesting[byte]
	for range maxNesting {
		n.Push(')')
	}
	if !n.Tracked() {
		t.Fatal("nesting at capacity is untracked")
	}
	n.Push(']')
	if n.Tracked() {
		t.Error("nesting beyond capacity is tracked")
	}
	if _, ok := n.Top(); ok {
		t.Error("Top beyond capacity is known")
	}
	if !n.Pop(')') || !n.Tracked() {
		t.Error("an unverifiable Pop beyond capacity failed or left the nesting untracked")
	}
	if top, ok := n.Top(); !ok || top != ')' {
		t.Errorf("Top back at capacity = %q, %v", top, ok)
	}
}
