package syntax

import (
	"fmt"
	"strings"
	"testing"
)

func TestEveryNameHasItsOwnSlot(t *testing.T) {
	f := Parse(`{ a, b ? c.d, ... }@all: let x = a; inherit (b) y z; "q${x}".r = [ x y ]; in rec { p.q = x or y; ${all} = z; }`)
	seen := map[int]Node{}
	Inspect(f.Root, func(n Node) bool {
		var slot int
		switch n := n.(type) {
		case *Ident:
			slot = n.Slot()
		case Attr:
			slot = n.Slot()
		default:
			return true
		}
		if other, taken := seen[slot]; taken {
			t.Errorf("%T and %T share slot %d", n, other, slot)
		}
		if slot < 0 || slot >= f.Slots {
			t.Errorf("slot %d is outside the %d the file counted", slot, f.Slots)
		}
		seen[slot] = n
		return true
	})
	if len(seen) == 0 {
		t.Fatal("no names were visited")
	}
}

func TestArgumentsKeepTheirOrderAcrossNesting(t *testing.T) {
	f := Parse("f (g a b) [ c (h d) ] e")
	apply, ok := f.Root.(*Apply)
	if !ok || len(apply.Args) != 3 {
		t.Fatalf("root = %#v", f.Root)
	}
	got := make([]string, len(apply.Args))
	for i, arg := range apply.Args {
		got[i] = f.Text[arg.Span().Start:arg.Span().End]
	}
	if want := "(g a b),[ c (h d) ],e"; strings.Join(got, ",") != want {
		t.Errorf("arguments = %q, want %q", strings.Join(got, ","), want)
	}
}

func TestEstimatedTokensCoverCodeWithoutStrings(t *testing.T) {
	var sb strings.Builder
	for i := range 200 {
		fmt.Fprintf(&sb, "a%d = lib.mkIf (b != null) (f ./p.nix { inherit lib; });\n", i)
	}
	src := "{\n" + sb.String() + "}\n"
	if f := Parse(src); len(f.Tokens) > estimatedTokens(src) {
		t.Errorf("%d tokens against an estimate of %d", len(f.Tokens), estimatedTokens(src))
	}
}
