package navigation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
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

func TestValidNames(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		kind  Kind
		name  string
		valid bool
	}{
		{KindVariable, "count", true},
		{KindVariable, "_x9", true},
		{KindVariable, "9x", false},
		{KindVariable, "a.b", false},
		{KindVariable, "", false},
		{KindFunction, "git::log-short.v2", true},
		{KindFunction, "_private", true},
		{KindFunction, "-flag", false},
		{KindFunction, ".hidden", false},
		{KindFunction, "1st", false},
		{KindFunction, "a b", false},
		{KindFunction, "a/b", false},
		{KindFunction, "", false},
	} {
		if got := tc.kind.ValidName(tc.name); got != tc.valid {
			t.Errorf("%s.ValidName(%q) = %v, want %v", tc.kind, tc.name, got, tc.valid)
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

func TestRenameErrorsNameTheSymbolKindTheSameWay(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"main.sh": "deploy() { :; }\ndeploy\ngrep x\n"})
	main := f.load(t, "main.sh")
	_, _, err := PrepareRename(t.Context(), main, f.offset(t, "main.sh", "grep", 0), f.env, f.files)
	if want := `function "grep" is not defined in the workspace`; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("PrepareRename(grep) error = %v, want %q", err, want)
	}
	_, err = Rename(t.Context(), main, f.offset(t, "main.sh", "deploy", 1), "no way", f.env, f.files)
	if want := `"no way" is not a valid function name`; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Rename(deploy) error = %v, want %q", err, want)
	}
}

const programChain = "#!/bin/bash\nvalue=1\njq --arg name \"$value\" '$name, $name' f\nawk -v other=\"$value\" 'BEGIN { print other }'\necho \"$value\"\n"

func TestProgramArgumentsFollowTheirFlag(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"a.sh": programChain})
	file := f.load(t, "a.sh")
	ctx := context.Background()
	for _, tt := range []struct {
		name, needle string
		definition   []string
		references   []string
	}{
		{
			"jq", "$name",
			[]string{"a.sh:3:name", "a.sh:2:value"},
			[]string{"a.sh:2:value:w", "a.sh:3:name:w", "a.sh:3:value:r", "a.sh:3:name:r", "a.sh:3:name:r", "a.sh:4:value:r", "a.sh:5:value:r"},
		},
		{
			"awk", "other }",
			[]string{"a.sh:4:other", "a.sh:2:value"},
			[]string{"a.sh:2:value:w", "a.sh:3:value:r", "a.sh:4:other:w", "a.sh:4:value:r", "a.sh:4:other:r", "a.sh:5:value:r"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			at := f.offset(t, "a.sh", tt.needle, 0)
			locations, err := Definition(ctx, file, at, f.env, f.files, ModeDefinition)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, l := range locations {
				rel, _ := filepath.Rel(f.root, l.File.Path)
				text := l.File.Tree.Text()
				got = append(got, fmt.Sprintf("%s:%d:%s", rel, strings.Count(text[:l.Span.Start], "\n")+1, text[l.Span.Start:l.Span.End]))
			}
			if !slices.Equal(got, tt.definition) {
				t.Errorf("definition = %q, want %q", got, tt.definition)
			}
			occs, err := References(ctx, file, at, f.env, f.files, true)
			if err != nil {
				t.Fatal(err)
			}
			if got := render(f, occs); !slices.Equal(got, tt.references) {
				t.Errorf("references = %q, want %q", got, tt.references)
			}
		})
	}
}

func TestProgramArgumentRenameStaysInTheProgram(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"a.sh": programChain})
	file := f.load(t, "a.sh")
	ctx := context.Background()
	at := f.offset(t, "a.sh", "$name", 0)
	occs, err := Rename(ctx, file, at, "wanted", f.env, f.files)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := render(f, occs), []string{"a.sh:3:name:w", "a.sh:3:name:r", "a.sh:3:name:r"}; !slices.Equal(got, want) {
		t.Errorf("rename = %q, want %q", got, want)
	}
	if _, err := Rename(ctx, file, at, "a b", f.env, f.files); !errors.Is(err, ErrInvalidName) {
		t.Errorf("renaming to a bad name = %v", err)
	}
	refused := newFixture(t, map[string]string{"a.sh": "#!/bin/bash\njq --arg name \"$1\" '$name, $ARGS.named' f\n"})
	if _, err := Rename(ctx, refused.load(t, "a.sh"), refused.offset(t, "a.sh", "$name", 0), "wanted", refused.env, refused.files); !errors.Is(err, ErrNotRenamable) || !strings.Contains(err.Error(), "$ARGS") {
		t.Errorf("renaming beside $ARGS = %v", err)
	}
}

func TestAProgramVariableWithoutAFlagIsNotLinked(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"a.sh": "#!/bin/bash\njq '. as $name | $name' f\n"})
	locations, err := Definition(context.Background(), f.load(t, "a.sh"), f.offset(t, "a.sh", "| $name", 0)+2, f.env, f.files, ModeDefinition)
	if err != nil {
		t.Fatal(err)
	}
	if len(locations) != 0 {
		t.Errorf("definition = %+v", locations)
	}
}
