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
