package filelint

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func recipeFile(t testing.TB, name, language, text string) *workspace.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), filepath.FromSlash("recipes/"+name))
	return workspace.Parse(path, docstore.NewDocument(fileuri.FromPath(path), language, 1, text), nil, nil)
}

func justFile(t testing.TB, text string) *workspace.File {
	t.Helper()
	return recipeFile(t, "justfile", "just", text)
}

func makeFile(t testing.TB, text string) *workspace.File {
	t.Helper()
	return recipeFile(t, "Makefile", "makefile", text)
}

func findingsAt(file *workspace.File) []string {
	report := Check(file, nixEnv(), Settings{})
	out := make([]string, 0, len(report.Findings))
	for _, f := range report.Findings {
		out = append(out, fmt.Sprintf("%v:%s", f.Code, file.Doc.Text()[f.Span.Start:f.Span.End]))
	}
	slices.Sort(out)
	return out
}

func TestRecipesShareOnlyWhatTheirHostGivesThem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file func(testing.TB, string) *workspace.File
		src  string
		want []string
	}{
		{
			name: "just: another recipe's assignment neither uses nor provides a variable",
			file: justFile,
			src:  "a:\n    shared=1\nb:\n    echo \"$shared\"\n",
			want: []string{"SC2034:shared=1", "SC2154:$shared"},
		},
		{
			name: "make: another rule's assignment neither uses nor provides a variable",
			file: makeFile,
			src:  "a:\n\tshared=1\nb:\n\techo \"$$shared\"\n",
			want: []string{"SC2034:shared=1", "SC2154:$shared"},
		},
		{
			name: "just: lines of a recipe stay one unit and report the lost change",
			file: justFile,
			src:  "a:\n    x=1\n    echo \"$x\"\n",
			want: []string{"SC2030:x=1", "SC2031:$x"},
		},
		{
			name: "make: lines of a rule stay one unit and report the lost change",
			file: makeFile,
			src:  "a:\n\tx=1\n\techo \"$$x\"\n",
			want: []string{"SC2030:x=1", "SC2031:$x"},
		},
		{
			name: "just: a shebang recipe runs as one script",
			file: justFile,
			src:  "a:\n    #!/bin/bash\n    x=1\n    echo \"$x\"\nb:\n    #!/bin/bash\n    echo \"$x\"\n",
			want: []string{"SC2154:$x"},
		},
		{
			name: "just: a script recipe runs as one script",
			file: justFile,
			src:  "set unstable\n[script('bash')]\na:\n    x=1\n    echo \"$x\"\n[script('bash')]\nb:\n    echo \"$x\"\n",
			want: []string{"SC2154:$x"},
		},
		{
			name: "make: .ONESHELL joins the lines of a rule but not two rules",
			file: makeFile,
			src:  ".ONESHELL:\na:\n\tx=1\n\techo \"$$x\"\nb:\n\techo \"$$x\"\n",
			want: []string{"SC2154:$x"},
		},
		{
			name: "just: an exported variable reaches every recipe",
			file: justFile,
			src:  "export shared := 'x'\na:\n    echo \"$shared\"\nb:\n    echo \"$shared\"\n",
			want: []string{},
		},
		{
			name: "just: an exported parameter reaches only its recipe",
			file: justFile,
			src:  "a $shared:\n    echo \"$shared\"\nb:\n    echo \"$shared\"\n",
			want: []string{"SC2154:$shared"},
		},
		{
			name: "make: an exported variable reaches every rule",
			file: makeFile,
			src:  "export SHARED := x\na:\n\techo \"$$SHARED\"\nb:\n\techo \"$$SHARED\"\n",
			want: []string{},
		},
		{
			name: "just: set shell decides the dialect of every recipe",
			file: justFile,
			src:  "set shell := ['bash', '-cu']\na:\n    [[ -n \"$HOME\" ]] && echo a\nb:\n    [[ -n \"$HOME\" ]] && echo b\n",
			want: []string{},
		},
		{
			name: "just: a shebang picks the dialect of its recipe alone",
			file: justFile,
			src:  "a:\n    [[ -n \"$HOME\" ]]\nb:\n    #!/bin/bash\n    [[ -n \"$HOME\" ]]\n",
			want: []string{"SC3010:[[ -n \"$HOME\" ]]"},
		},
		{
			name: "make: a target's SHELL picks the dialect of its rule alone",
			file: makeFile,
			src:  "SHELL := /bin/sh\nb: SHELL := /bin/bash\na:\n\t[[ -n \"$$HOME\" ]]\nb:\n\t[[ -n \"$$HOME\" ]]\n",
			want: []string{"SC3010:[[ -n \"$HOME\" ]]"},
		},
		{
			name: "just: a directive covers its own recipe",
			file: justFile,
			src:  "a:\n    # shellcheck disable=SC2086\n    echo $1\nb:\n    echo $1\n",
			want: []string{"SC2086:$1"},
		},
		{
			name: "make: a directive covers its own rule",
			file: makeFile,
			src:  "a:\n\t# shellcheck disable=SC2086\n\techo $$1\nb:\n\techo $$1\n",
			want: []string{"SC2086:$1"},
		},
		{
			name: "just: a function in one recipe does not define it for another",
			file: justFile,
			src:  "a:\n    #!/bin/bash\n    x=1\n    f() { echo \"$x\"; }\n    f\nb:\n    #!/bin/bash\n    echo \"$x\"\n",
			want: []string{"SC2154:$x"},
		},
		{
			name: "just: a syntax error stays in its recipe",
			file: justFile,
			src:  "a:\n    if true\nb:\n    unused=1\n",
			want: []string{"SC2034:unused=1"},
		},
		{
			name: "make: a command the file cannot see may read and assign anything",
			file: makeFile,
			src:  "adjust = space=$$(($${space} - 1))\na:\n\tspace=4 ; \\\n\t$(adjust)\nb:\n\t$(adjust) ; \\\n\techo \"$$given\"\n",
			want: []string{},
		},
		{
			name: "make: a command with arguments runs a program and cannot read a variable",
			file: makeFile,
			src:  "a:\n\tspace=4 ; \\\n\t$(CC) -c a.c\n",
			want: []string{"SC2034:space=4"},
		},
		{
			name: "just: an interpolated command may read and assign anything",
			file: justFile,
			src:  "a cmd:\n    space=4 ; \\\n    {{cmd}}\n",
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := findingsAt(tt.file(t, tt.src))
			if !slices.Equal(got, tt.want) {
				t.Errorf("findings = %q, want %q", got, tt.want)
			}
		})
	}
}

const (
	replayedJustfile = "set shell := ['bash', '-cu']\nexport token := 'x'\n\nbuild target:\n    echo $target $token\n    x=1\n    echo \"$x\"\n\ncheck $name:\n    #!/usr/bin/env bash\n    y=$1\n    echo $name {{name}}\n\ndeploy:\n    # shellcheck disable=SC2086\n    echo $1 $2\n    cp a b && echo done\n"
	replayedMakefile = "CC := cc\nexport TOKEN := x\n\nbuild: main.c\n\t$(CC) -c main.c\n\techo $$TOKEN $$1\n\tx=1\n\techo \"$$x\"\n\ncheck: SHELL := /bin/bash\ncheck:\n\t@[[ -n \"$$HOME\" ]] && echo $$2\n\ndeploy:\n\t# shellcheck disable=SC2086\n\techo $$1\n\tcp a b\n"
)

func TestRememberedRecipeReportsEqualAFullRecompute(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		file  func(testing.TB, string) *workspace.File
		text  string
		edits []string
	}{
		{"justfile", justFile, replayedJustfile, []string{" ", "x", "$v ", "\n", "\n    echo $z\n", "'", "\"", "{{a}}", "export ", "set export\n", "set shell := ['zsh', '-c']\n", "#!/bin/zsh\n", "# shellcheck disable=SC2086\n", "\\\n"}},
		{"Makefile", makeFile, replayedMakefile, []string{" ", "x", "$$v ", "\n", "\n\techo $$z\n", "'", "\"", "$(CC)", "export ", ".ONESHELL:\n", "SHELL := /bin/zsh\n", ".SHELLFLAGS := -ec\n", "# shellcheck disable=SC2086\n", "\\\n"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			source := rand.New(rand.NewPCG(69, 69))
			memory := NewMemory()
			env := nixEnv()
			text := tt.text
			for step := range 400 {
				switch at := source.IntN(len(text) + 1); {
				case step%5 == 4 && at < len(text):
					text = text[:at] + text[min(at+1+source.IntN(4), len(text)):]
				default:
					text = text[:at] + tt.edits[source.IntN(len(tt.edits))] + text[at:]
				}
				if step%40 == 39 {
					text = tt.text
				}
				got := reportsOf(memory.Check(tt.file(t, text), env, Settings{}))
				want := reportsOf(Check(tt.file(t, text), env, Settings{}))
				if got != want {
					t.Fatalf("step %d differs from a full recompute\nremembered: %s\nfull:       %s\ntext:\n%s", step, got, want, text)
				}
			}
		})
	}
}

func TestAnEditReusesTheOtherRecipes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		file func(testing.TB, string) *workspace.File
		text string
	}{
		{"justfile", justFile, "a:\n    one=1\nb:\n    two=2\nc:\n    cp a b\n"},
		{"Makefile", makeFile, "a:\n\tone=1\nb:\n\ttwo=2\nc:\n\tcp a b\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			memory := NewMemory()
			env := nixEnv()
			memory.Check(tt.file(t, tt.text), env, Settings{})
			memory.mu.Lock()
			kept := maps.Clone(memory.reports)
			memory.mu.Unlock()
			held := len(kept)
			if held < 3 {
				t.Fatalf("remembered %d reports, want one for each recipe", held)
			}
			memory.Check(tt.file(t, strings.Replace(tt.text, "cp a b", "cp a c", 1)), env, Settings{})
			memory.mu.Lock()
			defer memory.mu.Unlock()
			reused := 0
			for key, report := range kept {
				if memory.reports[key] == report && report.lastRun == memory.run {
					reused++
				}
			}
			if reused != held-1 {
				t.Errorf("reused %d of %d reports, want all but the edited recipe", reused, held)
			}
		})
	}
}
