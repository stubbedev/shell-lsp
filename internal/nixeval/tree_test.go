package nixeval

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

const truncatedOptions = `{"options": [
  {"loc": ["a", "b"], "type": "t", "description": "d", "default": "x", "example": "y", "declarations": [{"file": "/f.nix", "line": 3, "column": 5}, {"file": "/f.nix", "line": 9, "column": 1}]},
  {"loc": ["a", "<name>", "c"], "type": "u", "description": null, "default": null, "example": null, "declarations": []}
], "truncated": [["z", "deep"], ["a", "deep"]]}`

func TestEncodedTreeReportsItsTruncatedBranches(t *testing.T) {
	t.Parallel()

	encoded, err := encodeOptions([]byte(truncatedOptions))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := decodeOptions(encoded)
	if err != nil {
		t.Fatal(err)
	}
	got := tree.Truncated()
	if len(got) != 2 || !slices.Equal(got[0], []string{"a", "deep"}) || !slices.Equal(got[1], []string{"z", "deep"}) {
		t.Errorf("Truncated() = %q, want them sorted", got)
	}
	got[0][0] = "changed"
	if tree.Truncated()[0][0] != "a" {
		t.Error("Truncated() shares state with the tree")
	}
}

func TestEncodingADifferentInputOrderGivesTheSameBytes(t *testing.T) {
	t.Parallel()

	one, err := encodeOptions([]byte(`{"options": [{"loc": ["a"], "type": "1"}, {"loc": ["b"], "type": "2"}], "truncated": []}`))
	if err != nil {
		t.Fatal(err)
	}
	other, err := encodeOptions([]byte(`{"options": [{"loc": ["b"], "type": "2"}, {"loc": ["a"], "type": "1"}], "truncated": []}`))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(one, other) {
		t.Error("the encoding depends on the input order")
	}
}

func TestEncodeOptionsRequiresTheReportShape(t *testing.T) {
	t.Parallel()

	for _, in := range []string{`[]`, `{}`, `{"truncated": []}`, `not json`} {
		if _, err := encodeOptions([]byte(in)); err == nil {
			t.Errorf("encodeOptions(%s) = nil error", in)
		}
	}
}

func walkTreeNodes(node OptionNode, visit func(OptionNode)) {
	visit(node)
	for child := range node.Children() {
		walkTreeNodes(child, visit)
	}
}

func TestDamagedTreesAreRefusedOrStaySafeToRead(t *testing.T) {
	t.Parallel()

	encoded, err := encodeOptions([]byte(truncatedOptions))
	if err != nil {
		t.Fatal(err)
	}
	read := func(payload []byte) {
		t.Helper()
		tree, err := decodeOptions(payload)
		if err != nil {
			return
		}
		tree.Truncated()
		walkTreeNodes(tree.Root(), func(node OptionNode) {
			node.Name()
			node.Option()
			node.child("a")
		})
	}
	for length := range encoded {
		read(encoded[:length])
	}
	for position := range encoded {
		for _, value := range []byte{0x00, 0x01, 0x7F, 0x80, 0xFF} {
			damaged := slices.Clone(encoded)
			damaged[position] = value
			read(damaged)
		}
	}
}

func TestLookupFindsEveryChildOfAWideNode(t *testing.T) {
	t.Parallel()

	options := make([]string, 1000)
	for i := range options {
		options[i] = `{"loc": ["n` + strconv.Itoa(i) + `"], "type": "` + strconv.Itoa(i) + `"}`
	}
	encoded, err := encodeOptions([]byte(`{"options": [` + strings.Join(options, ",") + `], "truncated": []}`))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := decodeOptions(encoded)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 1000 {
		name := "n" + strconv.Itoa(i)
		node, ok := tree.Lookup([]string{name})
		if !ok {
			t.Fatalf("Lookup(%s) is missing", name)
		}
		if option, ok := node.Option(); !ok || option.Type != strconv.Itoa(i) || !slices.Equal(option.Path, []string{name}) {
			t.Errorf("Lookup(%s).Option() = %+v, %v", name, option, ok)
		}
	}
	if _, ok := tree.Lookup([]string{"absent"}); ok {
		t.Error("Lookup(absent) found a node")
	}
}
