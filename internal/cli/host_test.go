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

func TestCheckNamesStdinToChooseItsLanguage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, tt := range []struct {
		name, text string
		want       []string
	}{
		{"pkg.nix", "let a = 1; in 2\n", []string{"pkg.nix:1:5 SL8005"}},
		{"sub/Makefile", "all:\n\t@echo $$1\n", []string{"sub/Makefile:2:8 SC2086"}},
		{"justfile", "a:\n    echo $1\n", []string{"justfile:2:10 SC2086"}},
		{"Dockerfile", "FROM x\nRUN echo $1\n", []string{"Dockerfile:2:10 SC2086"}},
		{"plain.sh", "#!/bin/sh\necho $1\n", []string{"plain.sh:2:6 SC2086"}},
	} {
		got := run(t, dir, tt.text, "check", "-f", "gcc", "-e", "DL3006", "--filename", tt.name, "-")
		want := make([]string, len(tt.want))
		for i, w := range tt.want {
			want[i] = filepath.FromSlash(w)
		}
		if !slices.Equal(findings(t, got.stdout), want) || got.status != checkStatusFindings {
			t.Errorf("check --filename %s = %d %v, want %v\n%s", tt.name, got.status, findings(t, got.stdout), want, got.stderr)
		}
	}
}

func TestCheckStdinFilenameOverridesTheFileOnDisk(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"pkg.nix": "let a = 1; in 2\n"})
	if got := run(t, dir, "1\n", "check", "--filename", "pkg.nix", "-"); got.status != statusOK {
		t.Errorf("stdin is clean but the check read the disk: %+v", got)
	}
}

func TestCheckFilenameNeedsStdin(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.sh": "#!/bin/sh\n"})
	if got := run(t, dir, "", "check", "--filename", "x.nix", "a.sh"); got.status != checkStatusSyntax || !strings.Contains(got.stderr, "--filename") {
		t.Errorf("--filename without stdin = %+v", got)
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

func TestFmtFormatsMakefiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"proj/Makefile": "CC=gcc\nall:app\n\t$(CC) -o app main.c  \n\n\n\n", "proj/tidy.mk": "X := 1\n", "proj/a.sh": "if true;then echo;fi\n", "proj/default.nix": "{ }\n"})
	if got := run(t, dir, "", "fmt", "-l", "proj"); got.stdout != filepath.FromSlash("proj/Makefile")+"\n"+filepath.FromSlash("proj/a.sh")+"\n" {
		t.Errorf("fmt -l over a project = %q, %q", got.stdout, got.stderr)
	}
	if got := run(t, dir, "", "fmt", "proj/Makefile"); got.status != statusOK || got.stdout != "CC = gcc\nall: app\n\t$(CC) -o app main.c  \n" {
		t.Errorf("fmt of a Makefile = %+v", got)
	}
	if got := run(t, dir, "", "fmt", "proj/default.nix"); got.status != statusOK {
		t.Errorf("fmt of a Nix file = %+v", got)
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

const nixFindings = "let\n  unused = 1;\nin\n  undefinedName\n"

func TestCheckReportsNixFilesInEveryFormat(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"proj/default.nix": nixFindings, "proj/sub/mod.nix": "{ }\n", "proj/.git/hook.nix": "undefinedElsewhere\n"})

	gcc := run(t, dir, "", "check", "-f", "gcc", "proj")
	want := []string{filepath.FromSlash("proj/default.nix:2:3 SL8005"), filepath.FromSlash("proj/default.nix:4:3 SL8002")}
	if !slices.Equal(findings(t, gcc.stdout), want) || gcc.status != checkStatusFindings {
		t.Errorf("gcc = %d %v, want %v\n%s", gcc.status, findings(t, gcc.stdout), want, gcc.stderr)
	}
	for _, tt := range []struct {
		format string
		has    []string
	}{
		{"tty", []string{filepath.FromSlash("In proj/default.nix line 2:"), "  unused = 1;", "SL8002 (error)"}},
		{"json", []string{`"level":"info"`, `"level":"error"`, `"line":4`}},
		{"json1", []string{`{"comments":[`, `"level":"error"`}},
		{"checkstyle", []string{"source='ShellCheck.SL8005'", "source='ShellCheck.SL8002'", "severity='error'"}},
		{"sarif", []string{`"ruleId":"SL8005"`, `"ruleId":"SL8002"`, `"version":"2.1.0"`}},
		{"diff", []string{"-  unused = 1;\n"}},
	} {
		got := run(t, dir, "", "check", "-f", tt.format, "proj")
		if got.status != checkStatusFindings {
			t.Errorf("%s exits %d: %s", tt.format, got.status, got.stderr)
		}
		for _, sub := range tt.has {
			if !strings.Contains(got.stdout, sub) {
				t.Errorf("%s output lacks %q:\n%s", tt.format, sub, got.stdout)
			}
		}
	}
	if got := run(t, dir, "", "check", "-f", "quiet", "proj"); got.status != checkStatusFindings || got.stdout != "" {
		t.Errorf("quiet = %+v", got)
	}
}

func TestCheckFiltersNixFindings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"default.nix": nixFindings, "clean.nix": "{ }\n", "broken.nix": "{ a = ; }\n"})
	for _, tt := range []struct {
		args []string
		want []string
	}{
		{[]string{"-S", "error"}, []string{"default.nix:4:3 SL8002"}},
		{[]string{"-e", "SL8002"}, []string{"default.nix:2:3 SL8005"}},
		{[]string{"-i", "SL8005"}, []string{"default.nix:2:3 SL8005"}},
		{[]string{"-e", "SL8002,SL8005"}, nil},
	} {
		args := append(append([]string{"check", "-f", "gcc"}, tt.args...), "default.nix")
		got := run(t, dir, "", args...)
		if have := findings(t, got.stdout); !slices.Equal(have, tt.want) {
			t.Errorf("%v = %v, want %v", tt.args, have, tt.want)
		}
		status := checkStatusFindings
		if tt.want == nil {
			status = statusOK
		}
		if got.status != status {
			t.Errorf("%v exits %d, want %d", tt.args, got.status, status)
		}
	}
	if got := run(t, dir, "", "check", "clean.nix"); got.status != statusOK || got.stdout != "" {
		t.Errorf("a clean Nix file = %+v", got)
	}
	if got := run(t, dir, "", "check", "-f", "gcc", "broken.nix"); got.status != checkStatusFindings || !strings.Contains(got.stdout, "[SL8001]") {
		t.Errorf("a Nix file that does not parse = %+v", got)
	}
	if got := run(t, dir, "", "check", "missing.nix"); got.status != checkStatusFiles {
		t.Errorf("a missing Nix file = %+v", got)
	}
}

func TestCheckWalksTheNixCorpus(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"single-flake", "flake-parts"} {
		root, err := filepath.Abs(filepath.Join("..", "..", "corpus", "nix", name))
		if err != nil {
			t.Fatal(err)
		}
		got := run(t, t.TempDir(), "", "check", "-f", "gcc", root)
		if got.status != statusOK && got.status != checkStatusFindings {
			t.Errorf("%s exits %d: %s", name, got.status, got.stderr)
		}
		for _, code := range []string{"[SL8001]", "[SL8002]"} {
			if strings.Contains(got.stdout, code) {
				t.Errorf("%s reports %s:\n%s", name, code, got.stdout)
			}
		}
	}
}
