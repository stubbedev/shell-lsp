package actions

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const libURI = "file:///w/lib.sh"

func moveDocument(t *testing.T, src, lib string, libDialect dialect.Dialect) (string, parser.Span, Document) {
	t.Helper()

	text, sel := selection(t, src)
	tree := parser.Parse(text, dialect.Bash)
	libTree := parser.Parse(lib, libDialect)
	sources := func() []Source {
		var out []Source
		for _, c := range tree.Commands {
			if c.IsSourceBuiltin() {
				out = append(out, Source{URI: libURI, Name: "lib.sh", Tree: libTree, Command: c})
			}
		}
		return out
	}
	return text, sel, Document{Tree: tree, Sources: sources}
}

func TestMoveFunction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src, lib, want, wantLib, title string
		libDialect                     dialect.Dialect
	}{
		{
			src:     "#!/bin/bash\n. ./lib.sh\n\n# Greets.\n«greet»() {\n\techo hi\n}\n\ngreet\n",
			lib:     "helper() { :; }\n",
			want:    "#!/bin/bash\n. ./lib.sh\n\ngreet\n",
			wantLib: "helper() { :; }\n\n# Greets.\ngreet() {\n\techo hi\n}\n",
			title:   "Move greet to lib.sh",
		},
		{
			src:     "source lib.sh || exit 1\nfunction «g» {\n\techo hi\n}\ng\n",
			lib:     "x=1\n\n",
			want:    "source lib.sh || exit 1\ng\n",
			wantLib: "x=1\n\nfunction g {\n\techo hi\n}\n",
			title:   "Move g to lib.sh",
		},
		{
			src:     "source lib.sh\n«g»() { echo hi; }",
			lib:     "x=1",
			want:    "source lib.sh\n",
			wantLib: "x=1\n\ng() { echo hi; }\n",
			title:   "Move g to lib.sh",
		},
		{
			src:     "source lib.sh\n«g»() { echo hi; }\n",
			lib:     "",
			want:    "source lib.sh\n",
			wantLib: "g() { echo hi; }\n",
			title:   "Move g to lib.sh",
		},
		{
			src:        "#!/bin/bash\n. ./lib.sh\n# Section notes.\n\n# Greets.\n«greet»() {\n\tprintf '%s\\n' \"hi $1\"\n}\n",
			lib:        "helper() { :; }\n",
			want:       "#!/bin/bash\n. ./lib.sh\n# Section notes.\n\n",
			wantLib:    "helper() { :; }\n\n# Greets.\ngreet() {\n\tprintf '%s\\n' \"hi $1\"\n}\n",
			title:      "Move greet to lib.sh",
			libDialect: dialect.Posix,
		},
		{
			src:        "source lib.sh\n# Counts.\n«count»() {\n\tlocal n=$#\n\techo \"$n\"\n}\n",
			lib:        "",
			want:       "source lib.sh\n",
			wantLib:    "# Counts.\ncount() {\n\tlocal n=$#\n\techo \"$n\"\n}\n",
			title:      "Move count to lib.sh",
			libDialect: dialect.Dash,
		},
		{
			src:        "source lib.sh\n# shellcheck disable=SC3043\n«count»() {\n\tlocal n=$#\n}\n",
			lib:        "",
			want:       "source lib.sh\n",
			wantLib:    "# shellcheck disable=SC3043\ncount() {\n\tlocal n=$#\n}\n",
			title:      "Move count to lib.sh",
			libDialect: dialect.Posix,
		},
		{
			src:        "source lib.sh\n«g»() {\n\t[[ -n $1 ]] && echo \"${1,,}\"\n}\n",
			lib:        "",
			want:       "source lib.sh\n",
			wantLib:    "g() {\n\t[[ -n $1 ]] && echo \"${1,,}\"\n}\n",
			title:      "Move g to lib.sh",
			libDialect: dialect.Bash,
		},
	}
	for _, tc := range tests {
		text, sel, doc := moveDocument(t, tc.src, tc.lib, tc.libDialect)
		in := Input{Document: doc, Selection: sel}
		i := slices.IndexFunc(Actions(in), func(a Action) bool { return a.Kind == RefactorMove })
		if i < 0 {
			t.Errorf("move %q not offered", tc.src)
			continue
		}
		action := Actions(in)[i]
		if action.Refactor == nil || action.Refactor.Target != libURI || action.Title != tc.title {
			t.Errorf("move action = %+v", action)
		}
		change, ok := Compute(doc, *action.Refactor)
		if !ok || len(change.Others) != 1 || change.Others[0].URI != libURI {
			t.Fatalf("resolve %q = %+v (%t)", tc.src, change, ok)
		}
		if got := textedit.Apply(text, change.Edits); got != tc.want {
			t.Errorf("move %q left %q, want %q", tc.src, got, tc.want)
		}
		if got := textedit.Apply(tc.lib, change.Others[0].Edits); got != tc.wantLib {
			t.Errorf("move %q made lib %q, want %q", tc.src, got, tc.wantLib)
		}
	}
	for _, tc := range []struct {
		src, lib   string
		libDialect dialect.Dialect
	}{
		{"source lib.sh\n«greet»() {\n\tlocal x=1\n\techo \"$x\"\n}\n", "", dialect.Posix},
		{"source lib.sh\n«greet»() {\n\t[[ -n $1 ]] && echo hi\n}\n", "", dialect.Dash},
		{"source lib.sh\n«greet»() {\n\techo {a,b}\n}\n", "", dialect.Busybox},
		{"source lib.sh\n«greet»() {\n\tmapfile -t lines <f\n}\n", "", dialect.Posix},
		{"source lib.sh\n«greet»() {\n\tcoproc cat\n}\n", "", dialect.Posix},
		{"«greet»() { :; }\nsource lib.sh\n", "", dialect.Bash},
		{"if true; then source lib.sh; fi\n«greet»() { :; }\n", "", dialect.Bash},
		{"source lib.sh &\n«greet»() { :; }\n", "", dialect.Bash},
		{"source lib.sh\n«greet»() { :; }\n", "greet() { :; }\n", dialect.Bash},
		{"source lib.sh\nf() {\n\t«g»() { :; }\n}\n", "", dialect.Bash},
		{"source lib.sh\n«g»() { :; }\ng() { echo; }\n", "", dialect.Bash},
		{"source lib.sh\n«g»() { :; }; g\n", "", dialect.Bash},
		{"source lib.sh\ng() {\n\t«echo» hi\n}\n", "", dialect.Bash},
		{"«g»() { :; }\n", "", dialect.Bash},
		{"f() { source lib.sh; }\n«g»() { :; }\n", "", dialect.Bash},
	} {
		_, sel, doc := moveDocument(t, tc.src, tc.lib, tc.libDialect)
		for _, a := range Actions(Input{Document: doc, Selection: sel}) {
			if a.Kind == RefactorMove {
				t.Errorf("move %q offered: %+v", tc.src, a)
			}
		}
	}
	_, sel, doc := moveDocument(t, "source lib.sh\n«g»() { :; }\n", "", dialect.Bash)
	if _, ok := Compute(doc, Refactor{Kind: MoveFunction, Start: sel.Start, End: sel.End, Target: "file:///w/other.sh"}); ok {
		t.Error("move resolved for a file the document does not source")
	}
	doc.Sources = nil
	if _, ok := Compute(doc, Refactor{Kind: MoveFunction, Start: sel.Start, End: sel.End, Target: libURI}); ok {
		t.Error("move resolved without any sourced files")
	}
}
