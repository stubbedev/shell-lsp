package parser

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/getopt"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func TestWrappersStart(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src      string
		wrappers Wrappers
		want     string
	}{
		{"command -p ls -l\n", Wrappers{}, "ls"},
		{"exec -a name tool --x\n", Wrappers{}, "tool"},
		{"command time -p make\n", Wrappers{}, "make"},
		{"command time --format x make\n", Wrappers{}, "make"},
		{"builtin echo hi\n", Wrappers{}, "echo"},
		{"busybox awk 1\n", Wrappers{}, "awk"},
		{"sudo rm -rf x\n", Wrappers{}, "sudo"},
		{"sudo rm -rf x\n", Wrappers{Programs: true}, "rm"},
		{"command sudo -u root env -i A=1 B=2 nice -n 5 git status\n", Wrappers{Programs: true}, "git"},
		{"run jq .\n", Wrappers{}, "run"},
		{"run jq .\n", Wrappers{Bats: true}, "jq"},
		{"command $tool x\n", Wrappers{}, "$tool"},
		{"command -v ls\n", Wrappers{}, "command"},
		{"command -pV ls\n", Wrappers{}, "command"},
		{"sudo command -v ls\n", Wrappers{Programs: true}, "command"},
		{"exec\n", Wrappers{}, ""},
		{"timeout 5\n", Wrappers{Programs: true}, ""},
	} {
		call, ok := Parse(tt.src, dialect.Bash).File.Stmts[0].Cmd.(*syntax.CallExpr)
		if !ok {
			t.Fatalf("%q does not parse to a call", tt.src)
		}
		got := ""
		if i := tt.wrappers.Start(call.Args); i >= 0 {
			got = call.Args[i].Lit()
			if got == "" {
				got, _ = Text(call.Args[i], "$tool")
			}
		}
		if got != tt.want {
			t.Errorf("%+v.Start(%q) = %q, want %q", tt.wrappers, tt.src, got, tt.want)
		}
	}
}

func TestWrappersUnwrap(t *testing.T) {
	t.Parallel()

	all := Wrappers{Programs: true}
	for _, tt := range []struct {
		line    string
		want    Wrap
		wrapper bool
	}{
		{"sudo git status", Wrap{Command: 0}, true},
		{"sudo -u root -E git", Wrap{Command: 3}, true},
		{"sudo --user=root git", Wrap{Command: 1}, true},
		{"sudo --user root git", Wrap{Command: 2}, true},
		{"sudo -u", Wrap{Command: 1, Pending: true}, true},
		{"sudo -E", Wrap{Command: 1}, true},
		{"sudo -- -weird", Wrap{Command: 1}, true},
		{"sudo", Wrap{Command: 0}, true},
		{"doas -u user git", Wrap{Command: 2}, true},
		{"env -i FOO=1 BAR=x=y git", Wrap{Command: 3}, true},
		{"env -u NAME FOO=1 -i", Wrap{Command: 3}, true},
		{"env FOO=1", Wrap{Command: 1}, true},
		{"nice -n 10 git", Wrap{Command: 2}, true},
		{"nice -10 git", Wrap{Command: 1}, true},
		{"nohup git", Wrap{Command: 0}, true},
		{"time -p git", Wrap{Command: 1}, true},
		{"command -v git", Wrap{Command: 1, Query: true}, true},
		{"command -pV git", Wrap{Command: 1, Query: true}, true},
		{"command -p git -v", Wrap{Command: 1}, true},
		{"exec -a name git", Wrap{Command: 2}, true},
		{"timeout 5s git", Wrap{Command: 1}, true},
		{"timeout -s KILL --kill-after=3 5s git", Wrap{Command: 4}, true},
		{"timeout -k 3", Wrap{Command: 2, Pending: true}, true},
		{"timeout", Wrap{Command: 0, Pending: true}, true},
		{"watch -n 1 kubectl get pods", Wrap{Command: 2}, true},
		{"xargs -0 -n1 -I {} git", Wrap{Command: 4}, true},
		{"xargs -l1 -i -e git", Wrap{Command: 3}, true},
		{"sudo -a bsdauth -c staff git", Wrap{Command: 4}, true},
		{"sudo -h build1 git", Wrap{Command: 2}, true},
		{"env -L alice -P /bin git", Wrap{Command: 4}, true},
		{"watch -s shots -n 2 git", Wrap{Command: 4}, true},
		{"git status", Wrap{}, false},
		{"run git", Wrap{}, false},
	} {
		words := strings.Fields(tt.line)
		got, ok := all.Unwrap(words[0], words[1:])
		if got != tt.want || ok != tt.wrapper {
			t.Errorf("Unwrap(%q) = %+v, %t; want %+v, %t", tt.line, got, ok, tt.want, tt.wrapper)
		}
	}
	if _, ok := (Wrappers{}).Unwrap("sudo", []string{"git"}); ok {
		t.Error("sudo unwrapped although program wrappers were not asked for")
	}
	if got, ok := (Wrappers{Bats: true}).Unwrap("run", []string{"git"}); !ok || got.Command != 0 {
		t.Errorf("bats run = %+v, %t", got, ok)
	}
}

func TestWrappersLayersOwnArguments(t *testing.T) {
	t.Parallel()

	all := Wrappers{Programs: true}
	for _, tt := range []struct {
		line string
		want []string
	}{
		{line: "git status", want: []string{"git [status]"}},
		{line: "sudo -u root env A=1 git status", want: []string{"sudo [-u root]", "env [A=1]", "git [status]"}},
		{line: "sudo -u", want: []string{"sudo [-u]"}},
		{line: "nohup", want: []string{"nohup []"}},
	} {
		words := strings.Fields(tt.line)
		var got []string
		for layer := range all.Layers(words[0], words[1:]) {
			got = append(got, layer.Name+" ["+strings.Join(layer.Own(), " ")+"]")
		}
		if strings.Join(got, ", ") != strings.Join(tt.want, ", ") {
			t.Errorf("Layers(%q) own = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestEveryWrapperHasAGrammarThatStopsAtTheCommand(t *testing.T) {
	t.Parallel()

	for name := range commandWrappers {
		grammar, ok := getopt.Commands[name]
		if !ok || !grammar.StopAtOperand {
			t.Errorf("%s: grammar %+v, %t; a wrapper needs one in getopt.Commands that stops at the wrapped command", name, grammar, ok)
		}
	}
}

func TestWrappersLayers(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		line string
		want []string
	}{
		{"git status", []string{"git [status] at 0"}},
		{"sudo -u root env A=1 git status", []string{"sudo wraps 2 at 0", "env wraps 1 at 3", "git [status] at 5"}},
		{"sudo -u", []string{"sudo wraps 1 at 0"}},
		{"command -v git", []string{"command wraps 1 at 0", "git [] at 2"}},
		{"run nice -n 5 make", []string{"run wraps 0 at 0", "nice wraps 2 at 1", "make [] at 4"}},
	} {
		words := strings.Fields(tt.line)
		var got []string
		for layer := range (Wrappers{Bats: true, Programs: true}).Layers(words[0], words[1:]) {
			if layer.Wraps {
				got = append(got, layer.Name+" wraps "+strconv.Itoa(layer.Wrap.Command)+" at "+strconv.Itoa(layer.Offset))
				continue
			}
			got = append(got, layer.Name+" ["+strings.Join(layer.Args, " ")+"] at "+strconv.Itoa(layer.Offset))
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("Layers(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}
