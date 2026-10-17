package host

import (
	"maps"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestOf(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		uri, languageID string
		want            Language
		ok              bool
	}{
		{"file:///w/Makefile", "makefile", Make, true},
		{"file:///w/rules", "make", Make, true},
		{"file:///w/Makefile", "shellscript", Make, true},
		{"file:///w/justfile", "shellscript", Just, true},
		{"file:///w/flake.nix", "shellscript", Nix, true},
		{"file:///w/run.sh", "shellscript", 0, false},
		{"file:///w/rules", "shellscript", 0, false},
		{"file:///w/Makefile", "", Make, true},
		{"file:///w/GNUmakefile", "", Make, true},
		{"file:///w/build/common.mk", "", Make, true},
		{`C:\w\rules.mak`, "", Make, true},
		{"file:///w/Makefile.sh", "", 0, false},
		{"file:///w/run.sh", "", 0, false},
		{"file:///w/justfile", "just", Just, true},
		{"file:///w/Justfile", "", Just, true},
		{"file:///w/.justfile", "", Just, true},
		{"file:///w/tasks.just", "", Just, true},
		{"file:///w/flake.nix", "", Nix, true},
		{"file:///w/x", "nix", Nix, true},
		{"file:///w/Dockerfile", "", Docker, true},
		{"file:///w/Containerfile", "", Docker, true},
		{"file:///w/Dockerfile.dev", "", Docker, true},
		{"file:///w/Containerfile.ci", "", Docker, true},
		{"file:///w/build.Dockerfile", "", Docker, true},
		{"file:///w/app.dockerfile", "", Docker, true},
		{"file:///w/app.Containerfile", "", Docker, true},
		{"file:///w/x", "dockerfile", Docker, true},
		{"file:///w/Dockerfile", "shellscript", Docker, true},
		{"file:///w/dockerfile.sh", "", 0, false},
	} {
		got, ok := Of(tt.uri, tt.languageID)
		if ok != tt.ok || ok && got != tt.want {
			t.Errorf("Of(%q, %q) = %v, %v, want %v, %v", tt.uri, tt.languageID, got, ok, tt.want, tt.ok)
		}
	}
}

func TestEveryLanguageIsDefined(t *testing.T) {
	t.Parallel()

	for _, l := range []Language{Make, Just, Nix} {
		if d := definitions[l]; d.name == "" || len(d.languageIDs) == 0 || d.analyze == nil {
			t.Errorf("language %d is not fully defined: %+v", l, d)
		}
	}
}

func TestParseKeepsEachRecipeLineToItself(t *testing.T) {
	t.Parallel()

	text := "a:\n\techo \"unclosed\nb:\n\tif true; then echo x\nc:\n\techo $$(date\nd:\n\techo == x\n"
	script := Make.Project(docstore.NewDocument("file:///w/Makefile", "makefile", 1, text))
	tree := script.Parse(nil)
	type located struct {
		line, character uint32
		message         string
	}
	var got []located
	for _, e := range tree.Errors {
		at := script.Doc.OffsetToPosition(e.Span.Start, docstore.EncodingUTF16)
		got = append(got, located{at.Line, at.Character, e.Message})
	}
	want := []located{
		{1, 6, "missing `\"` to match `\"`"},
		{3, 1, "missing `fi` to match `if`"},
		{5, 6, "missing `)` to match `$(`"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("errors\n got %+v\nwant %+v", got, want)
	}
	if at := script.Doc.OffsetToPosition(len(script.Doc.Text()), docstore.EncodingUTF16); at != (lsp.Position{Line: 7, Character: 10}) {
		t.Errorf("the end of the projection is at %+v", at)
	}
}

func TestLostChangesBlameTheShellOfTheLine(t *testing.T) {
	t.Parallel()

	text := "x:\n\tFOO=bar\n\techo \"$$FOO\"\n\t(BAR=1); echo \"$$BAR\"\n"
	script := Make.Project(docstore.NewDocument("file:///w/Makefile", "makefile", 1, text))
	report := lint.Run(script.Parse(nil), nil, lint.Options{DialectSource: lint.DialectForced})
	var got []string
	for _, f := range report.Findings {
		if f.Code == 2030 || f.Code == 2031 {
			got = append(got, f.Message)
		}
	}
	want := []string{
		"This change to FOO is made in a line or script that runs in a shell of its own, so nothing after it sees the change.",
		"FOO was changed on line 2, which runs in a shell of its own, so this still sees the value from before that change.",
		"This change to BAR happens in a subshell caused by a ( ) group, so it is gone once that subshell exits.",
		"BAR was changed in a subshell (a ( ) group on line 4), so this still sees the value from before that change.",
	}
	if !slices.Equal(got, want) {
		t.Errorf("messages\n got %q\nwant %q", got, want)
	}
}

func TestShellFlagsReachTheLinter(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		makefile string
		warned   bool
	}{
		{"x:\n\tcd sub\n", true},
		{".SHELLFLAGS := -ec\nx:\n\tcd sub\n", false},
	} {
		script := Make.Project(docstore.NewDocument("file:///w/Makefile", "makefile", 1, tt.makefile))
		report := lint.Run(script.Parse(nil), nil, lint.Options{DialectSource: lint.DialectForced})
		warned := slices.ContainsFunc(report.Findings, func(f lint.Finding) bool { return f.Code == 2164 })
		if warned != tt.warned {
			t.Errorf("%q: SC2164 reported = %v, want %v", tt.makefile, warned, tt.warned)
		}
	}
}

func TestLanguageRegistry(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		l       Language
		id      string
		formats bool
		command string
	}{
		{Make, "makefile", true, "make"},
		{Just, "just", true, "just"},
	} {
		if got := tt.l.LanguageID(); got != tt.id {
			t.Errorf("%v.LanguageID() = %q, want %q", tt.l, got, tt.id)
		}
		if l, ok := LanguageOf(tt.id); !ok || l != tt.l {
			t.Errorf("LanguageOf(%q) = %v, %v", tt.id, l, ok)
		}
		if got := tt.l.Formats(); got != tt.formats {
			t.Errorf("%v.Formats() = %v", tt.l, got)
		}
		if got := tt.l.Recursion().Command; got != tt.command {
			t.Errorf("%v.Recursion().Command = %q", tt.l, got)
		}
		if argv := tt.l.Invocation("/p/file", "target"); argv[0] != tt.command || argv[len(argv)-1] != "target" {
			t.Errorf("%v.Invocation = %v", tt.l, argv)
		}
		if tt.l.Builtins().Of(model.KindFunction) == nil {
			t.Errorf("%v has no builtin functions", tt.l)
		}
	}
	if formatted, err := Just.Format("a:\n  echo\n"); err != nil || formatted != "a:\n    echo\n" {
		t.Errorf("Just.Format = %q, %v", formatted, err)
	}
	if _, ok := LanguageOf("python"); ok {
		t.Error("LanguageOf(python) found a host language")
	}
}

func TestEveryKindHasRulesOfItsOwnLanguage(t *testing.T) {
	t.Parallel()

	for l, own := range map[Language]func(lint.Code) bool{Make: lint.Code.Make, Just: lint.Code.Just} {
		checks := l.Checks()
		for kind := range model.Kinds() {
			r := checks.Of(kind)
			for _, code := range []lint.Code{r.Unknown, r.Arity, r.Circular, r.Unused} {
				if code != 0 && !own(code) {
					t.Errorf("%v rules for %s use %s", l, kind, code)
				}
			}
			if r.Unknown == 0 && r.UnknownWhen != model.WhenComplete {
				t.Errorf("%v rules for %s set a certainty without an unknown check", l, kind)
			}
		}
		for _, code := range []lint.Code{checks.Deprecated, checks.Missing} {
			if code != 0 && !own(code) {
				t.Errorf("%v checks use %s", l, code)
			}
		}
	}
}

func TestNixHasNoNativeAnalysis(t *testing.T) {
	t.Parallel()

	script := Nix.Project(docstore.NewDocument("file:///w/default.nix", "nix", 0, "{ buildPhase = ''\n  echo hi\n''; }\n"))
	if script.Native != nil {
		t.Errorf("a Nix file has native analysis: %+v", script.Native)
	}
	native := Just.Project(docstore.NewDocument("file:///w/justfile", "just", 0, "a:\n    echo\n")).Native
	if native == nil || !native.Formats() || len(native.Index.Symbols) != 1 {
		t.Errorf("a justfile's native analysis = %+v", native)
	}
	if _, ok := native.Expect(0); !ok {
		t.Error("no completion context at the start of a justfile")
	}
}

func TestEveryRecipeParsesAsItsOwnShell(t *testing.T) {
	t.Parallel()

	shells := map[dialect.Dialect][]string{dialect.Posix: {"dash", "-n"}, dialect.Bash: {"bash", "-n"}, dialect.Zsh: {"zsh", "-f", "-n"}}
	for _, tt := range []struct {
		language Language
		uri, id  string
		text     string
	}{
		{Just, "file:///w/justfile", "just", "plain:\n    echo \"$1\"\nbash:\n    #!/usr/bin/env bash\n    arr=(1 2); echo \"${arr[@]}\"\nzsh:\n    #!/usr/bin/env zsh\n    for i (1 2) echo $i\n"},
		{Make, "file:///w/Makefile", "makefile", "plain:\n\techo \"$$1\"\nbash: SHELL := /bin/bash\nbash:\n\tarr=(1 2); echo \"$${arr[@]}\"\nzsh: SHELL := /bin/zsh\nzsh:\n\tfor i (1 2) echo $$i\n"},
		{Docker, "file:///w/Dockerfile", "dockerfile", "FROM alpine\nRUN echo \"$1\"\nONBUILD RUN echo \"$2\"\nSHELL [\"/bin/bash\", \"-c\"]\nRUN arr=(1 2); echo \"${arr[@]}\"\nFROM alpine AS z\nSHELL [\"/bin/zsh\", \"-c\"]\nFROM z\nRUN for i (1 2) echo $i\n"},
	} {
		script := tt.language.Project(docstore.NewDocument(tt.uri, tt.id, 1, tt.text))
		trees := map[dialect.Dialect]*parser.Tree{script.Detection.Dialect: script.Parse(nil)}
		spans := map[dialect.Dialect][]parser.Span{script.Detection.Dialect: script.Spans()}
		for _, v := range script.Variants {
			trees[v.Detection.Dialect] = script.ParseVariant(v, nil)
			spans[v.Detection.Dialect] = v.Spans()
		}
		if len(trees) != 3 {
			t.Fatalf("%s: shells %v, want sh, bash and zsh", tt.uri, slices.Collect(maps.Keys(trees)))
		}
		for d, tree := range trees {
			if len(tree.Errors) > 0 {
				t.Errorf("%s: the %s recipes do not parse as %s: %v", tt.uri, d, d, tree.Errors)
			}
			argv := shells[d]
			if _, err := exec.LookPath(argv[0]); err != nil {
				t.Skipf("comparing with %s needs it on PATH: %v", argv[0], err)
			}
			for _, span := range spans[d] {
				cmd := exec.CommandContext(t.Context(), argv[0], argv[1:]...)
				cmd.Stdin = strings.NewReader(script.Doc.Text()[span.Start:span.End])
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Errorf("%s: %s -n rejects the %s recipe it runs: %v\n%s", tt.uri, argv[0], d, err, out)
				}
			}
		}
	}
}
