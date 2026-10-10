package navigation

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func (f fixture) offset(t *testing.T, name, needle string, nth int) int {
	t.Helper()

	text := f.load(t, name).Tree.Text()
	offset := -1
	for range nth + 1 {
		i := strings.Index(text[offset+1:], needle)
		if i < 0 {
			t.Fatalf("needle %q #%d not in %s", needle, nth, name)
		}
		offset += i + 1
	}
	return offset
}

func render(f fixture, occs []Occurrence) []string {
	out := make([]string, len(occs))
	for i, o := range occs {
		rel, _ := filepath.Rel(f.root, o.File.Path)
		text := o.File.Tree.Text()
		line := strings.Count(text[:o.Span.Start], "\n") + 1
		mode := "r"
		if o.Write {
			mode = "w"
		}
		out[i] = fmt.Sprintf("%s:%d:%s:%s", rel, line, text[o.Span.Start:o.Span.End], mode)
	}
	return out
}

const shadowing = `#!/bin/bash
source ./lib.sh
count=0
bump() {
	local count=10
	(( count++ ))
	echo "$count"
	inner() {
		echo "${count}"
	}
}
reset() {
	count=0
}
read -r count
echo "$count" "$SHARED"
bump
`

func newReferenceFixture(t *testing.T) fixture {
	t.Helper()

	return newFixture(t, map[string]string{
		"main.sh":   shadowing,
		"lib.sh":    "SHARED=1\nbump() { :; }\n",
		"user.sh":   "source ./main.sh\necho \"$SHARED $count\"\nbump\n",
		"island.sh": "count=99\nbump\n",
	})
}

func TestReferencesRespectShadowing(t *testing.T) {
	t.Parallel()

	f := newReferenceFixture(t)
	global, err := References(t.Context(), f.load(t, "main.sh"), f.offset(t, "main.sh", "count=0", 0), f.env, f.files, true)
	if err != nil {
		t.Fatal(err)
	}
	want := "main.sh:3:count:w main.sh:13:count:w main.sh:15:count:w main.sh:16:count:r user.sh:2:count:r"
	if got := strings.Join(render(f, global), " "); got != want {
		t.Errorf("global count references:\n got %s\nwant %s", got, want)
	}

	local, err := References(t.Context(), f.load(t, "main.sh"), f.offset(t, "main.sh", "count++", 0), f.env, f.files, true)
	if err != nil {
		t.Fatal(err)
	}
	want = "main.sh:5:count:w main.sh:6:count:w main.sh:7:count:r main.sh:9:count:r"
	if got := strings.Join(render(f, local), " "); got != want {
		t.Errorf("local count references:\n got %s\nwant %s", got, want)
	}

	withoutDecl, err := References(t.Context(), f.load(t, "main.sh"), f.offset(t, "main.sh", "count++", 0), f.env, f.files, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(render(f, withoutDecl), " "); strings.Contains(got, "main.sh:5:") {
		t.Errorf("includeDeclaration=false kept the local declaration: %s", got)
	}
}

func TestReferencesForFunctionsSpanSourcingFiles(t *testing.T) {
	t.Parallel()

	f := newReferenceFixture(t)
	got, err := References(t.Context(), f.load(t, "main.sh"), f.offset(t, "main.sh", "bump() {", 0), f.env, f.files, true)
	if err != nil {
		t.Fatal(err)
	}
	want := "lib.sh:2:bump:w main.sh:4:bump:w main.sh:17:bump:r user.sh:3:bump:r"
	if s := strings.Join(render(f, got), " "); s != want {
		t.Errorf("bump references:\n got %s\nwant %s", s, want)
	}
}

func TestRename(t *testing.T) {
	t.Parallel()

	f := newReferenceFixture(t)
	main := f.load(t, "main.sh")
	occs, err := Rename(t.Context(), main, f.offset(t, "main.sh", "SHARED", 0), "COMMON", f.env, f.files)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(render(f, occs), " "); got != "lib.sh:1:SHARED:w main.sh:16:SHARED:r user.sh:2:SHARED:r" {
		t.Errorf("rename SHARED occurrences = %s", got)
	}

	tests := []struct {
		needle  string
		newName string
		err     error
	}{
		{needle: "SHARED\"", newName: "1bad", err: ErrInvalidName},
		{needle: "count=10", newName: "x y", err: ErrInvalidName},
		{needle: "#!/bin", newName: "X", err: ErrNoSymbol},
		{needle: "source", newName: "load", err: ErrNotRenamable},
		{needle: "reset", newName: "bump", err: ErrNameConflict},
	}
	for _, tc := range tests {
		_, err := Rename(t.Context(), main, f.offset(t, "main.sh", tc.needle, 0), tc.newName, f.env, f.files)
		if !errors.Is(err, tc.err) {
			t.Errorf("rename %q -> %q error = %v, want %v", tc.needle, tc.newName, err, tc.err)
		}
	}
}

func TestPrepareRenameRejectsUndefinedVariables(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"main.sh": "echo \"$PATH\"\n"})
	_, _, err := PrepareRename(t.Context(), f.load(t, "main.sh"), f.offset(t, "main.sh", "PATH", 0), f.env, f.files)
	if !errors.Is(err, ErrNotRenamable) {
		t.Errorf("PrepareRename(PATH) error = %v", err)
	}
}
