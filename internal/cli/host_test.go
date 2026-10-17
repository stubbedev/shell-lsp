package cli

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const makefile = "SHELL := /bin/bash\nall:\n\t@echo `date` $$1 $(CC)\n\tcount -eq 3\n\techo \"unclosed\n\tcd sub && echo $$PWD\n"

func TestCheckLintsMakefileRecipes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"proj/Makefile": makefile, "proj/rules/extra.mk": "x:\n\techo $$1\n", "proj/Makefile.am": "SUBDIRS = .\n"})
	got := run(t, dir, "", "check", "-f", "gcc", "proj")
	want := []string{
		filepath.FromSlash("proj/Makefile:3:8 SC2006"),
		filepath.FromSlash("proj/Makefile:3:8 SC2046"),
		filepath.FromSlash("proj/Makefile:3:15 SC2086"),
		filepath.FromSlash("proj/Makefile:5:7 SC1072"),
		filepath.FromSlash("proj/Makefile:6:17 SC2086"),
		filepath.FromSlash("proj/rules/extra.mk:2:7 SC2086"),
	}
	if !slices.Equal(findings(t, got.stdout), want) || got.status != checkStatusFindings {
		t.Errorf("checking a project with Makefiles = %d %v, want %v\n%s", got.status, findings(t, got.stdout), want, got.stderr)
	}

	tty := run(t, dir, "", "check", "proj/Makefile")
	if !strings.Contains(tty.stdout, "In proj/Makefile line 3:\n\t@echo `date` $$1 $(CC)\n") {
		t.Errorf("tty output does not quote the Makefile line:\n%s", tty.stdout)
	}
}

func TestCheckDiffWritesFixesInMakeSyntax(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"Makefile": makefile})
	got := run(t, dir, "", "check", "-f", "diff", "Makefile")
	if !strings.Contains(got.stdout, "-\t@echo `date` $$1 $(CC)\n+\t@echo $$(date) $$1 $(CC)\n") {
		t.Errorf("diff = %q", got.stdout)
	}
}

func TestFmtLeavesMakefilesAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"proj/Makefile": makefile, "proj/a.sh": "if true;then echo;fi\n"})
	if got := run(t, dir, "", "fmt", "-l", "proj"); got.stdout != filepath.FromSlash("proj/a.sh")+"\n" {
		t.Errorf("fmt -l over a project = %q, %q", got.stdout, got.stderr)
	}
	if got := run(t, dir, "", "fmt", "proj/Makefile"); got.status == statusOK || !strings.Contains(got.stderr, "formats only shell scripts") {
		t.Errorf("fmt of a Makefile = %+v", got)
	}
}

func TestCheckLintsJustfileRecipes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"proj/justfile": "import 'tasks.just'\nrun:\n    echo $1 {{x}} {{nope}}\n", "proj/tasks.just": "x := 'a'\ny:\n    #!/usr/bin/env bash\n    echo $1\n"})
	got := run(t, dir, "", "check", "-f", "gcc", "proj")
	want := []string{
		filepath.FromSlash("proj/justfile:3:10 SC2086"),
		filepath.FromSlash("proj/justfile:3:21 SL5003"),
		filepath.FromSlash("proj/tasks.just:4:10 SC2086"),
	}
	if !slices.Equal(findings(t, got.stdout), want) {
		t.Errorf("checking a project with justfiles = %v, want %v\n%s", findings(t, got.stdout), want, got.stderr)
	}
	if got := run(t, dir, "", "fmt", "proj/justfile"); got.status != statusOK || got.stdout != "import 'tasks.just'\n\nrun:\n    echo $1 {{ x }} {{ nope }}\n" && got.stdout != "import 'tasks.just'\nrun:\n    echo $1 {{ x }} {{ nope }}\n" {
		t.Errorf("fmt of a justfile = %+v", got)
	}
	if got := run(t, dir, "", "check", "-e", "SL5003", "-f", "gcc", "proj/justfile"); slices.Contains(findings(t, got.stdout), filepath.FromSlash("proj/justfile:3:21 SL5003")) {
		t.Errorf("-e SL5003 still reports it: %v", findings(t, got.stdout))
	}
}

func TestCheckLintsNixShellStrings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"proj/default.nix": "{ buildPhase = ''\n  echo $1\n''; src = ~/x; }\n"})
	got := run(t, dir, "", "check", "-f", "gcc", "proj")
	if want := []string{filepath.FromSlash("proj/default.nix:2:8 SC2086")}; !slices.Equal(findings(t, got.stdout), want) {
		t.Errorf("checking a project with Nix files = %v, want %v\n%s", findings(t, got.stdout), want, got.stderr)
	}
}

func TestFmtRefusesToDropAJustfileComment(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"justfile": "build: # explains build\n    echo\n"})
	if got := run(t, dir, "", "fmt", "justfile"); got.status == statusOK || !strings.Contains(got.stderr, "would drop the comment") {
		t.Errorf("fmt of a justfile whose comment just --fmt drops = %+v", got)
	}
	if got := run(t, dir, "", "fmt", "-l", "."); got.status == statusOK {
		t.Errorf("fmt -l over a directory with that justfile = %+v", got)
	}
}
