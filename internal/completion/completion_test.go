package completion

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/manpage"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

type fakeEnv struct {
	dirs        map[string][]DirEntry
	options     map[string][]manpage.Option
	subcommands map[string]manpage.Listing
}

func (f fakeEnv) Executables() []string { return []string{"git", "grep", "gzip", "ls"} }

func (f fakeEnv) ReadDir(dir string) ([]DirEntry, error) {
	entries, ok := f.dirs[filepath.Clean(dir)]
	if !ok {
		return nil, os.ErrNotExist
	}
	return entries, nil
}

func (f fakeEnv) Home() string { return "/home/u" }

func (f fakeEnv) WorkDir() (string, error) { return "/work", nil }

func (f fakeEnv) EnvironmentNames() []string { return []string{"EDITOR_THEME", "HOME"} }

func (f fakeEnv) Options(_ context.Context, command string, args []string) ([]manpage.Option, error) {
	if opts, ok := f.options[strings.Join(append([]string{command}, args...), " ")]; ok {
		return opts, nil
	}
	if opts, ok := f.options[command]; ok {
		return opts, nil
	}
	return nil, manpage.ErrNoPage
}

func (f fakeEnv) Subcommands(_ context.Context, command string) (manpage.Listing, error) {
	return f.subcommands[command], nil
}

var env = fakeEnv{
	dirs: map[string][]DirEntry{
		"/proj":           {{Name: "lib", Dir: true}, {Name: "main.sh"}, {Name: ".hidden"}, {Name: "my file.sh"}},
		"/proj/lib":       {{Name: "util.sh"}, {Name: "helpers", Dir: true}},
		"/home/u":         {{Name: "bin", Dir: true}, {Name: "notes.txt"}},
		"/srv/app":        {{Name: "config.yml"}},
		"/work":           {{Name: "main.sh"}, {Name: "other.sh"}},
		"/home/u/scripts": {{Name: "x.sh"}},
	},
	options: map[string][]manpage.Option{
		"grep":            {{Names: []string{"-E", "--extended-regexp"}, Doc: "Extended regexps."}, {Names: []string{"-i", "--ignore-case"}, Doc: "Ignore case."}},
		"git -q checkout": {{Names: []string{"-b"}, Doc: "Create a branch."}},
	},
	subcommands: map[string]manpage.Listing{
		"git": {Subcommands: []manpage.Subcommand{
			{Name: "checkout", Summary: "Switch branches.", Doc: "**git-checkout**"},
			{Name: "commit", Summary: "Record changes.", Doc: "**git-commit**"},
		}},
		"docker": {Unloaded: []string{"docker-run"}},
	},
}

const cursor = "‸"

func complete(t *testing.T, d dialect.Dialect, src string) Result {
	t.Helper()

	offset := strings.LastIndex(src, cursor)
	if offset < 0 {
		t.Fatalf("source %q lacks a %s cursor marker", src, cursor)
	}
	text := src[:offset] + src[offset+len(cursor):]
	got, err := Complete(t.Context(), Input{
		Tree: parser.Parse(text, d), Offset: offset, ScriptDir: "/proj", Snippets: true, Env: env,
	})
	if err != nil {
		t.Fatalf("Complete(%q) error = %v", src, err)
	}
	return got
}

func labels(r Result) []string {
	out := make([]string, 0, len(r.Items))
	for _, it := range r.Items {
		out = append(out, it.Label)
	}
	return out
}

func find(r Result, label string) (Item, bool) {
	i := slices.IndexFunc(r.Items, func(it Item) bool { return it.Label == label })
	if i < 0 {
		return Item{}, false
	}
	return r.Items[i], true
}

func TestCommandPosition(t *testing.T) {
	t.Parallel()

	src := "deploy() { :; }\nalias gs='git status'\ng‸"
	got := complete(t, dialect.Bash, src)
	want := []string{"gs", "getopts", "git", "grep", "gzip"}
	if !slices.Equal(labels(got), want) {
		t.Errorf("labels = %v, want %v", labels(got), want)
	}
	if it, _ := find(got, "git"); it.Resolve != ResolveCommand || it.Kind != KindCommand {
		t.Errorf("git item = %+v, want lazily resolved command", it)
	}

	all := complete(t, dialect.Bash, "deploy() { :; }\nif true; then ‸")
	for _, want := range []string{"deploy", "local", "if", "ls"} {
		if _, ok := find(all, want); !ok {
			t.Errorf("command position after then lacks %q", want)
		}
	}
	fn, _ := find(all, "deploy")
	if fn.Kind != KindFunction || fn.Resolve != ResolveFunction || fn.SortText >= "2" {
		t.Errorf("function item = %+v", fn)
	}
	tree := parser.Parse("# Deploys.\ndeploy() { :; }\n", dialect.Bash)
	if doc, ok := Documentation(tree, nil, ResolveFunction, "deploy", 0); !ok || !strings.Contains(doc, "Deploys.") {
		t.Errorf("function documentation = %q, %t", doc, ok)
	}
	if doc, ok := Documentation(tree, nil, ResolveVariable, "HOME", 3); !ok || !strings.Contains(doc, "Home directory") {
		t.Errorf("variable documentation = %q, %t", doc, ok)
	}
	kw, _ := find(all, "for")
	if !kw.Snippet || !strings.Contains(kw.InsertText, "${1:name}") {
		t.Errorf("for keyword = %+v, want snippet", kw)
	}
}

func TestCommandPositionDialect(t *testing.T) {
	t.Parallel()

	got := labels(complete(t, dialect.Posix, "‸"))
	if slices.Contains(got, "declare") || slices.Contains(got, "[[") {
		t.Errorf("POSIX completions include bash-only names: %v", got)
	}
	if !slices.Contains(labels(complete(t, dialect.Zsh, "‸")), "setopt") {
		t.Error("zsh completions lack setopt")
	}
}

func TestCommandPositionAfterSeparatorsAndAssignments(t *testing.T) {
	t.Parallel()

	for _, src := range []string{"echo hi; gr‸", "ls | gr‸", "true && gr‸", "FOO=1 BAR=2 gr‸", "x=$(gr‸", "! gr‸", "{ gr‸", "time gr‸"} {
		if _, ok := find(complete(t, dialect.Bash, src), "grep"); !ok {
			t.Errorf("%q: grep not offered in command position", src)
		}
	}
}

func TestVariables(t *testing.T) {
	t.Parallel()

	src := "glob=1\nf() {\n\tlocal loc=2\n\techo \"$‸\"\n}\n"
	got := complete(t, dialect.Bash, src)
	loc, ok := find(got, "loc")
	if !ok || loc.Detail != "local" || loc.SortText >= "1" {
		t.Errorf("loc = %+v", loc)
	}
	if g, ok := find(got, "glob"); !ok || g.Detail != "global" {
		t.Errorf("glob = %+v", g)
	}
	if _, ok := find(got, "BASH_SOURCE"); !ok {
		t.Error("bash shell variables missing")
	}
	if e, ok := find(got, "EDITOR_THEME"); !ok || e.Detail != "environment" {
		t.Errorf("environment variable = %+v", e)
	}
	if _, ok := find(got, "@"); !ok {
		t.Error("special parameters missing for empty prefix")
	}

	outside := complete(t, dialect.Bash, "f() { local hidden=1; }\necho ${hi‸}\n")
	if _, ok := find(outside, "hidden"); ok {
		t.Error("function local offered outside its function")
	}
	braced := complete(t, dialect.Bash, "home_dir=1\necho ${ho‸")
	it, ok := find(braced, "home_dir")
	if !ok || braced.Items[0].Replace.End-it.Replace.Start != 2 {
		t.Errorf("braced completion = %+v", braced.Items)
	}
}

func TestNoCompletionInCommentsAndSingleQuotes(t *testing.T) {
	t.Parallel()

	for _, src := range []string{"# gr‸", "echo 'gr‸", "echo hi # $‸"} {
		if got := complete(t, dialect.Bash, src); len(got.Items) != 0 {
			t.Errorf("%q: got %v, want nothing", src, labels(got))
		}
	}
}

func TestHeredocs(t *testing.T) {
	t.Parallel()

	src := "name=1\ncat <<EOF\n$na‸\nEOF\n"
	if _, ok := find(complete(t, dialect.Bash, src), "name"); !ok {
		t.Error("variables not completed in an unquoted heredoc")
	}
	quoted := "name=1\ncat <<'EOF'\n$na‸\nEOF\n"
	if got := complete(t, dialect.Bash, quoted); len(got.Items) != 0 {
		t.Errorf("quoted heredoc completions = %v", labels(got))
	}
}

func TestFlags(t *testing.T) {
	t.Parallel()

	got := complete(t, dialect.Bash, "grep --ext‸")
	if !slices.Equal(labels(got), []string{"--extended-regexp"}) {
		t.Errorf("flags = %v", labels(got))
	}
	if it := got.Items[0]; it.Documentation != "Extended regexps." || it.Detail != "-E, --extended-regexp" {
		t.Errorf("flag item = %+v", it)
	}
	if got := complete(t, dialect.Bash, "nopage -‸"); len(got.Items) != 0 {
		t.Errorf("flags for command without a page = %v", labels(got))
	}
}

func TestSubcommands(t *testing.T) {
	t.Parallel()

	got := complete(t, dialect.Bash, "git ‸")
	if !slices.Equal(labels(got), []string{"checkout", "commit"}) || got.Incomplete {
		t.Fatalf("git subcommands = %v, incomplete %t", labels(got), got.Incomplete)
	}
	if it := got.Items[0]; it.Kind != KindSubcommand || it.Detail != "Switch branches." || it.Documentation != "**git-checkout**" || it.SortText != "0checkout" {
		t.Errorf("checkout item = %+v", it)
	}
	if got := labels(complete(t, dialect.Bash, "git -q ch‸")); !slices.Equal(got, []string{"checkout"}) {
		t.Errorf("subcommands after a flag = %v", got)
	}
	if got := labels(complete(t, dialect.Bash, "git -q checkout -‸")); !slices.Equal(got, []string{"-b"}) {
		t.Errorf("flags after a subcommand = %v, want the subcommand's options", got)
	}
	if got := labels(complete(t, dialect.Bash, "git checkout ‸")); slices.Contains(got, "commit") || !slices.Contains(got, "lib/") {
		t.Errorf("arguments after a subcommand = %v, want paths", got)
	}
	if got := labels(complete(t, dialect.Bash, "git zz‸")); len(got) != 0 {
		t.Errorf("unknown subcommand prefix = %v, want the (empty) path completion", got)
	}
	pending := complete(t, dialect.Bash, "docker ‸")
	if !pending.Incomplete || !slices.Contains(labels(pending), "lib/") {
		t.Errorf("subcommands still loading = %v, incomplete %t; want paths marked incomplete", labels(pending), pending.Incomplete)
	}
	if plain := complete(t, dialect.Bash, "ls ‸"); plain.Incomplete || !slices.Contains(labels(plain), "lib/") {
		t.Errorf("command without subcommands = %v, incomplete %t", labels(plain), plain.Incomplete)
	}
}

func TestShellOptions(t *testing.T) {
	t.Parallel()

	if got := labels(complete(t, dialect.Bash, "set -o pipe‸")); !slices.Equal(got, []string{"pipefail"}) {
		t.Errorf("set -o = %v", got)
	}
	if got := labels(complete(t, dialect.Bash, "shopt -s glob‸")); !slices.Equal(got, []string{"globstar"}) {
		t.Errorf("shopt = %v", got)
	}
	if got := labels(complete(t, dialect.Zsh, "setopt extended‸")); !slices.Equal(got, []string{"extended_glob"}) {
		t.Errorf("setopt = %v", got)
	}
}

func TestArgumentSpecialCases(t *testing.T) {
	t.Parallel()

	if _, ok := find(complete(t, dialect.Bash, "sudo gr‸"), "grep"); !ok {
		t.Error("sudo argument does not complete commands")
	}
	if got := labels(complete(t, dialect.Bash, "x=1\nunset ‸")); !slices.Contains(got, "x") {
		t.Errorf("unset = %v", got)
	}
	if got := labels(complete(t, dialect.Bash, "cd ‸")); !slices.Equal(got, []string{"lib/"}) {
		t.Errorf("cd = %v, want directories only", got)
	}
}

func TestPaths(t *testing.T) {
	t.Parallel()

	got := complete(t, dialect.Bash, "source ./lib/‸")
	if !slices.Equal(labels(got), []string{"util.sh", "helpers/"}) {
		t.Errorf("labels = %v", labels(got))
	}
	util, _ := find(got, "util.sh")
	if util.InsertText != "./lib/util.sh" || util.Replace.End-util.Replace.Start != len("./lib/") {
		t.Errorf("util item = %+v", util)
	}

	merged := labels(complete(t, dialect.Bash, "cat ‸"))
	for _, want := range []string{"lib/", "main.sh", "my file.sh", "other.sh"} {
		if !slices.Contains(merged, want) {
			t.Errorf("relative completion lacks %q: %v", want, merged)
		}
	}
	if slices.Contains(merged, ".hidden") {
		t.Error("hidden file offered without a dot prefix")
	}
	if !slices.Contains(labels(complete(t, dialect.Bash, "cat .h‸")), ".hidden") {
		t.Error("hidden file not offered for a dot prefix")
	}

	spaced, _ := find(complete(t, dialect.Bash, "cat my‸"), "my file.sh")
	if spaced.InsertText != `my\ file.sh` {
		t.Errorf("unquoted insert = %q", spaced.InsertText)
	}
	quoted, _ := find(complete(t, dialect.Bash, `cat "my‸`), "my file.sh")
	if quoted.InsertText != `"my file.sh` {
		t.Errorf("quoted insert = %q", quoted.InsertText)
	}
	if got := labels(complete(t, dialect.Bash, "cat ~/‸")); !slices.Equal(got, []string{"bin/", "notes.txt"}) {
		t.Errorf("home completion = %v", got)
	}
	if got := labels(complete(t, dialect.Bash, "cd /srv/app\ncat ‸")); !slices.Contains(got, "config.yml") {
		t.Errorf("cd-tracked completion = %v", got)
	}
	if got := labels(complete(t, dialect.Bash, "./li‸")); !slices.Equal(got, []string{"lib/"}) {
		t.Errorf("command position path = %v", got)
	}
}

func TestEscapeName(t *testing.T) {
	t.Parallel()

	tests := map[[2]string]string{
		{"a b$c", ""}:     `a\ b\$c`,
		{"it's", "'"}:     `it'\''s`,
		{`say "hi"`, `"`}: `say \"hi\"`,
	}
	for in, want := range tests {
		if got := escapeName(in[0], in[1]); got != want {
			t.Errorf("escapeName(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestNestedContexts(t *testing.T) {
	t.Parallel()

	for _, src := range []string{"kill \"$(gr‸", "case \"$(gr‸", "x=\"a $(echo b) $(gr‸", "echo $(ls | gr‸"} {
		if _, ok := find(complete(t, dialect.Bash, src), "grep"); !ok {
			t.Errorf("%q: grep not offered inside a command substitution", src)
		}
	}
	for _, src := range []string{"count=1\n(( cou‸", "count=1\nx=$((cou‸", "count=1\n(( x = (cou‸", "count=1\necho \"$(( cou‸"} {
		if _, ok := find(complete(t, dialect.Bash, src), "count"); !ok {
			t.Errorf("%q: count not offered in arithmetic", src)
		}
	}
	after := complete(t, dialect.Bash, "echo \"$(date)\" --he‸")
	if len(after.Items) != 0 {
		t.Errorf("after a closed substitution the outer argument context is lost: %v", labels(after))
	}
	if got := labels(complete(t, dialect.Bash, "grep \"$(echo x)\" -‸")); len(got) != 4 {
		t.Errorf("flags of the outer command after a substitution = %v", got)
	}
}
