package makefile

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/projection"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestProjectText(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, makefile, want string
	}{
		{
			name:     "each recipe line runs in its own shell",
			makefile: "all: build\n\tcd src && make\n\techo done\n",
			want:     "(\ncd src && make\n)\n(\necho done\n)\n",
		},
		{
			name:     "dollar escapes and make references",
			makefile: "x:\n\techo $$HOME $(CC) ${CFLAGS} $@ $(call f,$(X))\n",
			want:     "(\necho $HOME $$ $$ $$ $$\n)\n",
		},
		{
			name:     "recipe prefixes are not shell",
			makefile: "x:\n\t@echo quiet\n\t-rm -f out\n\t+$(MAKE) -C sub\n\t @ - echo mixed\n",
			want:     "(\necho quiet\n)\n(\nrm -f out\n)\n(\n$$ -C sub\n)\n(\necho mixed\n)\n",
		},
		{
			name:     "continuations keep the backslash and lose the next tab",
			makefile: "x:\n\tfor f in a b; do \\\n\t  echo $$f; \\\n\tdone\n",
			want:     "(\nfor f in a b; do \\\n  echo $f; \\\ndone\n)\n",
		},
		{
			name:     "inline recipe after a semicolon",
			makefile: "x: y ; echo one\n\techo two\n",
			want:     "(\necho one\n)\n(\necho two\n)\n",
		},
		{
			name:     "comment lines pass through for directives",
			makefile: "x:\n\t# shellcheck disable=SC2086\n\techo $$1\n",
			want:     "# shellcheck disable=SC2086\n(\necho $1\n)\n",
		},
		{
			name:     "make syntax between recipes is not shell",
			makefile: "CC := gcc\n# comment\nx: y\n\techo x\n\nifeq ($(A),b)\n\techo b\nelse\n\techo c\nendif\nFOO = bar\n\techo not a recipe\n",
			want:     "(\necho x\n)\n(\necho b\n)\n(\necho c\n)\n",
		},
		{
			name:     "define bodies are skipped",
			makefile: "define body\n\techo inside\nendef\nx:\n\techo outside\n",
			want:     "(\necho outside\n)\n",
		},
		{
			name:     "target specific variables open no recipe",
			makefile: "x: CFLAGS += -O2\n\techo nothing\n",
			want:     "",
		},
		{
			name:     "recipe prefix can be changed",
			makefile: ".RECIPEPREFIX = >\nx:\n> echo one\n\techo two\n",
			want:     "(\necho one\n)\n",
		},
		{
			name:     "oneshell joins a recipe into one script",
			makefile: ".ONESHELL:\nx:\n\tcd sub\n\t@pwd\ny:\n\t# only a comment\n",
			want:     "(\ncd sub\npwd\n)\n# only a comment\n",
		},
		{
			name:     "vpath with a colon is no rule",
			makefile: "vpath %.c src:lib\n\techo not a recipe\n",
			want:     "",
		},
		{
			name:     "double colon and static pattern rules",
			makefile: "a:: b\n\techo a\nobjs: %.o: %.c\n\techo $<\n",
			want:     "(\necho a\n)\n(\necho $$\n)\n",
		},
		{
			name:     "shell flags become set options",
			makefile: ".SHELLFLAGS := -euo pipefail -c\nx:\n\techo\n",
			want:     "(\nset -eu -o pipefail\necho\n)\n",
		},
		{
			name:     "posix makefiles run with errexit",
			makefile: ".POSIX:\nx:\n\techo\n",
			want:     "(\nset -e\necho\n)\n",
		},
		{
			name:     "computed shell flags are unknown",
			makefile: ".SHELLFLAGS = $(FLAGS)\nx:\n\techo\n",
			want:     "(\necho\n)\n",
		},
		{
			name:     "assignments are no rules",
			makefile: "A := b\n\techo no\nB ::= c\nC ?= d\nD != echo e\n",
			want:     "",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _ := Project(tt.makefile)
			if m.Text() != tt.want {
				t.Errorf("Project(%q) =\n%q\nwant\n%q", tt.makefile, m.Text(), tt.want)
			}
		})
	}
}

func TestProjectDialect(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		makefile string
		want     dialect.Detection
		analysed bool
	}{
		{"x:\n\techo\n", dialect.Detection{Dialect: dialect.Posix, Source: dialect.SourceHostDefault, Evidence: "make"}, true},
		{"SHELL := /bin/bash\nx:\n\techo\n", dialect.Detection{Dialect: dialect.Bash, Source: dialect.SourceHostSetting, Evidence: "SHELL := /bin/bash"}, true},
		{"export SHELL = /usr/bin/env zsh\nx:\n\techo\n", dialect.Detection{Dialect: dialect.Zsh, Source: dialect.SourceHostSetting, Evidence: "SHELL = /usr/bin/env zsh"}, true},
		{"SHELL = bash\nSHELL = dash\nx:\n\techo\n", dialect.Detection{Dialect: dialect.Dash, Source: dialect.SourceHostSetting, Evidence: "SHELL = dash"}, true},
		{"SHELL ?= bash\nx:\n\techo\n", dialect.Detection{Dialect: dialect.Posix, Source: dialect.SourceHostDefault, Evidence: "make"}, true},
		{"SHELL := python3\nx:\n\techo\n", dialect.Detection{Dialect: dialect.Posix, Source: dialect.SourceHostSetting, Evidence: "SHELL := python3"}, false},
		{"SHELL := $(shell command -v bash)\nx:\n\techo\n", dialect.Detection{Dialect: dialect.Posix, Source: dialect.SourceHostSetting, Evidence: "SHELL := $(shell command -v bash)"}, false},
		{"SHELL != command -v bash\nx:\n\techo\n", dialect.Detection{Dialect: dialect.Posix, Source: dialect.SourceHostSetting, Evidence: "SHELL != command -v bash"}, false},
	} {
		m, got := Project(tt.makefile)
		if got != tt.want {
			t.Errorf("Project(%q) detection = %+v, want %+v", tt.makefile, got, tt.want)
		}
		if analysed := m.Text() != ""; analysed != tt.analysed {
			t.Errorf("Project(%q) analysed = %v, want %v", tt.makefile, analysed, tt.analysed)
		}
	}
}

func TestProjectPositions(t *testing.T) {
	t.Parallel()

	src := "x:\n\t@echo $$HOME $(CC) done\n"
	m, _ := Project(src)
	text := m.Text()
	for _, tt := range []struct {
		proj, host string
	}{
		{"echo", "echo"},
		{"$HOME", "$$HOME"},
		{"HOME", "HOME"},
		{"$$", "$(CC)"},
		{"done", "done"},
	} {
		start := projection.ProjOffset(index(t, text, tt.proj))
		hostStart := projection.HostOffset(index(t, src, tt.host))
		if got := m.HostOffset(start); got != hostStart {
			t.Errorf("HostOffset of %q = %d, want %d", tt.proj, got, hostStart)
		}
		if got := m.HostOffset(start + projection.ProjOffset(len(tt.proj))); got != hostStart+projection.HostOffset(len(tt.host)) {
			t.Errorf("HostOffset of the end of %q = %d, want %d", tt.proj, got, hostStart+projection.HostOffset(len(tt.host)))
		}
		if got, ok := m.Offset(hostStart); got != start || ok == (tt.host == "$(CC)") {
			t.Errorf("Offset of %q = %d, %v, want %d and only make syntax outside", tt.host, got, ok, start)
		}
	}
	for _, outside := range []string{"x:", "@"} {
		if _, ok := m.Offset(projection.HostOffset(index(t, src, outside))); ok {
			t.Errorf("Offset of %q reports a shell position", outside)
		}
	}
}

func TestProjectEdits(t *testing.T) {
	t.Parallel()

	src := "x:\n\techo $$HOME $(CC) $$x\n\techo two\n"
	m, _ := Project(src)
	text := m.Text()
	for _, tt := range []struct {
		name string
		edit textedit.Edit
		want string
		ok   bool
	}{
		{"quote an escaped expansion", replace(t, text, "$HOME", `"$HOME"`), "x:\n\techo \"$$HOME\" $(CC) $$x\n\techo two\n", true},
		{"insert a dollar", replace(t, text, "two", "$two"), "x:\n\techo $$HOME $(CC) $$x\n\techo $$two\n", true},
		{"rewrite a placeholder", replace(t, text, "$$", "x"), "", false},
		{"quote around a placeholder", replace(t, text, "$$ $x", `"$$" "$x"`), "", false},
		{"span two recipe lines", replace(t, text, "$x\n)\n(\necho", "$x; echo"), "", false},
		{"insert a newline", replace(t, text, "two", "t\nwo"), "", false},
		{"insert before the first line", textedit.Edit{Start: 0, End: 0, NewText: "#!/bin/sh\n"}, "", false},
	} {
		got, ok := m.Edit(tt.edit)
		if ok != tt.ok {
			t.Errorf("%s: Edit(%+v) ok = %v, want %v", tt.name, tt.edit, ok, tt.ok)
			continue
		}
		if ok {
			if applied := textedit.Apply(src, []textedit.Edit{got}); applied != tt.want {
				t.Errorf("%s: applying %+v gives %q, want %q", tt.name, got, applied, tt.want)
			}
		}
	}
}

func index(t *testing.T, s, sub string) int {
	t.Helper()
	for i := range len(s) - len(sub) + 1 {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	t.Fatalf("%q does not occur in %q", sub, s)
	return -1
}

func replace(t *testing.T, s, old, text string) textedit.Edit {
	t.Helper()
	start := index(t, s, old)
	return textedit.Edit{Start: start, End: start + len(old), NewText: text}
}

func TestEnclosing(t *testing.T) {
	t.Parallel()

	text := "objs := $(patsubst %.c,%.o,$(srcs))\nall: $(objs)\n\tcc -o app $(objs)\n"
	f := Parse(text)
	var got []string
	for _, s := range f.Enclosing(strings.Index(text, "srcs")) {
		got = append(got, text[s.Start:s.End])
	}
	want := []string{"srcs", "$(srcs)", "$(patsubst %.c,%.o,$(srcs))", "objs := $(patsubst %.c,%.o,$(srcs))", text}
	if !slices.Equal(got, want) {
		t.Errorf("Enclosing = %q, want %q", got, want)
	}
}
