package completion

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func completeTriggered(t *testing.T, src, trigger string) Result {
	t.Helper()

	offset := strings.LastIndex(src, cursor)
	text := src[:offset] + src[offset+len(cursor):]
	got, err := Complete(t.Context(), Input{Tree: parser.Parse(text, dialect.Bash), Offset: offset, ScriptDir: "/proj", Trigger: trigger, Env: env})
	if err != nil {
		t.Fatalf("Complete(%q) error = %v", src, err)
	}
	return got
}

func TestDirectiveKeys(t *testing.T) {
	t.Parallel()

	got := complete(t, dialect.Bash, "# shellcheck ‸\necho hi\n")
	if !slices.Equal(labels(got), directive.Keys()) {
		t.Fatalf("keys = %v, want %v", labels(got), directive.Keys())
	}
	for _, it := range got.Items {
		if it.InsertText != it.Label+"=" || it.Kind != KindKey || it.Documentation == "" {
			t.Errorf("key item = %+v", it)
		}
	}
	mid, ok := find(complete(t, dialect.Bash, "# shellcheck dis‸able=SC2086\necho $1\n"), "disable")
	if !ok || mid.InsertText != "disable" || mid.Replace != (parser.Span{Start: 13, End: 16}) || mid.Word != (parser.Span{Start: 13, End: 20}) {
		t.Errorf("completing inside an existing key = %+v, %t", mid, ok)
	}
	if got := labels(complete(t, dialect.Bash, "echo hi # shellcheck so‸")); !slices.Equal(got, []string{"source", "source-path"}) {
		t.Errorf("trailing directive keys = %v", got)
	}
}

func TestDirectiveCodes(t *testing.T) {
	t.Parallel()

	got := complete(t, dialect.Bash, "# shellcheck disable=SC208‸\necho $1\n")
	item, ok := find(got, "SC2086")
	if !ok {
		t.Fatalf("SC2086 missing from %v", labels(got))
	}
	if item.Replace != (parser.Span{Start: 21, End: 26}) || item.Detail != "info" || !strings.Contains(item.Documentation, "https://www.shellcheck.net/wiki/SC2086") {
		t.Errorf("SC2086 item = %+v", item)
	}
	for _, label := range labels(got) {
		if !strings.HasPrefix(label, "SC208") {
			t.Errorf("%s offered for the prefix SC208", label)
		}
	}
	if slices.Contains(labels(complete(t, dialect.Bash, "# shellcheck disable=SC2086,SC20‸\necho $1\n")), "SC2086") {
		t.Error("a code already in the list was offered again")
	}
	bare, ok := find(complete(t, dialect.Bash, "# shellcheck disable=208‸\necho $1\n"), "SC2086")
	if !ok || bare.FilterText != "2086" {
		t.Errorf("a bare number prefix = %+v, %t", bare, ok)
	}
	ranged, ok := find(complete(t, dialect.Bash, "# shellcheck disable=SC2000-SC20‸\necho $1\n"), "SC2099")
	if !ok || ranged.Replace.Start != len("# shellcheck disable=SC2000-") {
		t.Errorf("the end of a range = %+v, %t", ranged, ok)
	}
	empty := complete(t, dialect.Bash, "# shellcheck disable=‸\necho $1\n")
	if got := len(empty.Items); got != len(lint.Codes())+1 {
		t.Errorf("%d items for an empty prefix, want every code and all", got)
	}
	native, ok := find(empty, "SL1005")
	if !ok || native.LabelDetail != " Downloaded code runs unchecked." || native.Detail != "warning" {
		t.Errorf("SL1005 item = %+v, %t", native, ok)
	}
	if _, ok := find(empty, "all"); !ok {
		t.Error("all is not offered")
	}
}

func TestDirectiveValues(t *testing.T) {
	t.Parallel()

	enable := complete(t, dialect.Bash, "# shellcheck enable=‸\necho hi\n")
	braces, ok := find(enable, "require-variable-braces")
	if !ok || !strings.Contains(braces.Documentation, "`SC2250`") || braces.Kind != KindOption {
		t.Errorf("require-variable-braces item = %+v, %t", braces, ok)
	}
	if labels(enable)[0] != "all" {
		t.Errorf("enable items start with %q", labels(enable)[0])
	}
	if got := labels(complete(t, dialect.Bash, "# shellcheck shell=b‸\necho hi\n")); !slices.Equal(got, []string{"bash", "busybox"}) {
		t.Errorf("shells = %v", got)
	}
	if got := labels(complete(t, dialect.Bash, "# shellcheck extended-analysis=‸\necho hi\n")); !slices.Equal(got, []string{"true", "false"}) {
		t.Errorf("booleans = %v", got)
	}
	if got := labels(complete(t, dialect.Bash, "# shellcheck source=lib/‸\n. \"$x\"\n")); !slices.Equal(got, []string{"util.sh", "helpers/"}) {
		t.Errorf("source paths = %v", got)
	}
	if got := labels(complete(t, dialect.Bash, "# shellcheck source-path=‸\n. \"$x\"\n")); !slices.Contains(got, "SCRIPTDIR") || !slices.Contains(got, "lib/") || slices.Contains(got, "main.sh") {
		t.Errorf("source-path offers directories and SCRIPTDIR, got %v", got)
	}
}

func TestDirectiveTriggersAnswerOnlyInDirectives(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ src, trigger string }{
		{"x=‸", "="},
		{"echo a,‸", ","},
		{"# a comment, not a directive=‸", "="},
	} {
		if got := completeTriggered(t, tc.src, tc.trigger); len(got.Items) != 0 {
			t.Errorf("%q triggered by %q offered %v", tc.src, tc.trigger, labels(got))
		}
	}
	if got := completeTriggered(t, "# shellcheck disable=SC1,‸\necho hi\n", ","); len(got.Items) == 0 {
		t.Error("a comma in a disable list offered nothing")
	}
}
