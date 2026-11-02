package nix

import (
	"slices"
	"strconv"
	"testing"
)

func TestANameTableAnswersLikeAMapAtEverySize(t *testing.T) {
	var table nameTable[int]
	names := make([]string, 3*linearNames)
	for i := range names {
		names[i] = "n" + strconv.Itoa(i)
		table.set(names[i], i)
		table.set(names[i], i+1)
		for j, name := range names[:i+1] {
			if got, known := table.get(name); !known || got != j+1 {
				t.Fatalf("with %d names, %s = %d, %v", i+1, name, got, known)
			}
		}
	}
	if table.has("absent") {
		t.Error("an absent name is known")
	}
	if !slices.IsSorted(table.sorted()) || len(table.sorted()) != len(names) {
		t.Errorf("sorted = %v", table.sorted())
	}
}
