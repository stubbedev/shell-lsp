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
	"github.com/stubbedev/shell-lsp/internal/platform"
)

type fakeEnv struct {
	dirs        map[string][]DirEntry
	options     map[string][]manpage.Option
	subcommands map[string]manpage.Listing
}

func (f fakeEnv) Executables() []string { return []string{"git", "grep", "gzip", "ls"} }

func (f fakeEnv) ReadDir(dir string) ([]DirEntry, error) {
	entries, ok := f.dirs[filepath.ToSlash(filepath.Clean(dir))]
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

func (f fakeEnv) Subcommands(_ context.Context, command string, args []string) (manpage.Listing, error) {
	return f.subcommands[strings.Join(append([]string{command}, args...), " ")], nil
}

var absoluteApp = map[platform.Flavour]string{platform.POSIX: "/srv/app", platform.Windows: "C:/srv/app"}[platform.Native]

var env = fakeEnv{
	dirs: map[string][]DirEntry{
		"/proj":           {{Name: "lib", Dir: true}, {Name: "main.sh"}, {Name: ".hidden"}, {Name: "my file.sh"}},
		"/proj/lib":       {{Name: "util.sh"}, {Name: "helpers", Dir: true}},
		"/home/u":         {{Name: "bin", Dir: true}, {Name: "notes.txt"}},
		absoluteApp:       {{Name: "config.yml"}},
		"/work":           {{Name: "main.sh"}, {Name: "other.sh"}},
		"/home/u/scripts": {{Name: "x.sh"}},
	},
	options: map[string][]manpage.Option{
		"grep":                  {{Names: []string{"-E", "--extended-regexp"}, Doc: "Extended regexps."}, {Names: []string{"-i", "--ignore-case"}, Doc: "Ignore case."}},
		"git -q checkout":       {{Names: []string{"-b"}, Doc: "Create a branch."}},
		"git -C dir remote add": {{Names: []string{"-t"}, Doc: "Track a branch."}},
		"sudo":                  {{Names: []string{"-u", "--user"}, Doc: "Run as user.", Valued: true}},
	},
	subcommands: map[string]manpage.Listing{
		"git": {Subcommands: []manpage.Subcommand{
			{Name: "checkout", Summary: "Switch branches.", Doc: "**git-checkout**"},
			{Name: "commit", Summary: "Record changes.", Doc: "**git-commit**"},
		}},
		"git -q": {Subcommands: []manpage.Subcommand{
			{Name: "checkout", Summary: "Switch branches.", Doc: "**git-checkout**"},
			{Name: "commit", Summary: "Record changes.", Doc: "**git-commit**"},
		}},
		"git -C dir remote": {Subcommands: []manpage.Subcommand{
			{Name: "add", Summary: "Add a remote.", Doc: "**git remote add**"},
			{Name: "rename", Summary: "Rename a remote.", Doc: "**git remote rename**"},
		}},
		"git remote": {
			Subcommands: []manpage.Subcommand{{Name: "add", Summary: "Add a remote.", Doc: "**git remote add**"}},
			Unloaded:    []string{"git-remote-ext"},
		},
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
	want := []string{"gs", "getopts loop", "getopts", "git", "grep", "gzip"}
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

func TestBuiltinFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		d       dialect.Dialect
		src     string
		want    []string
		missing []string
	}{
		{d: dialect.Bash, src: "read -‸", want: []string{"-r", "-a", "-p", "-t"}, missing: []string{"-A", "-k"}},
		{d: dialect.Zsh, src: "read -‸", want: []string{"-r", "-A", "-k"}, missing: []string{"-a", "-i"}},
		{d: dialect.Dash, src: "read -‸", want: []string{"-r"}, missing: []string{"-a", "-p", "-t"}},
		{d: dialect.Bash, src: "declare -‸", want: []string{"-A", "-a", "-g", "-n", "-I"}, missing: []string{"-T"}},
		{d: dialect.Zsh, src: "typeset -‸", want: []string{"-A", "-T", "-U", "-g"}, missing: []string{"-n", "-I"}},
		{d: dialect.Dash, src: "f() { local -‸; }", want: nil, missing: []string{"-a", "-r"}},
		{d: dialect.Bash, src: "shopt -‸", want: []string{"-s", "-u", "-q"}, missing: []string{"globstar"}},
		{d: dialect.Bash, src: "kill -‸", want: []string{"-s", "-l", "-n"}},
		{d: dialect.Posix, src: "kill -‸", want: []string{"-s", "-l"}, missing: []string{"-n"}},
		{d: dialect.Bash, src: "mapfile -‸", want: []string{"-t", "-d"}},
	}
	for _, tc := range tests {
		got := labels(complete(t, tc.d, tc.src))
		for _, want := range tc.want {
			if !slices.Contains(got, want) {
				t.Errorf("%s %q: %v lacks %s", tc.d, tc.src, got, want)
			}
		}
		for _, missing := range tc.missing {
			if slices.Contains(got, missing) {
				t.Errorf("%s %q: %v offers %s", tc.d, tc.src, got, missing)
			}
		}
	}
	it, ok := find(complete(t, dialect.Bash, "read -r‸"), "-r")
	if !ok || !strings.Contains(it.Documentation, "Raw input") || it.Kind != KindFlag {
		t.Errorf("read -r item = %+v", it)
	}
}

func TestShellVocabularyCompletion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		d       dialect.Dialect
		src     string
		want    []string
		missing []string
	}{
		{name: "unary in [[", d: dialect.Bash, src: "if [[ -‸", want: []string{"-f", "-d", "-z", "-v"}, missing: []string{"-eq", "=~"}},
		{name: "unary after &&", d: dialect.Bash, src: "[[ -f a && -‸", want: []string{"-d", "-n"}, missing: []string{"-eq"}},
		{name: "unary in [", d: dialect.Posix, src: "[ -‸", want: []string{"-f", "-n"}, missing: []string{"-v", "-N"}},
		{name: "binary in [[", d: dialect.Bash, src: "[[ $a ‸", want: []string{"==", "=~", "-eq", "-nt", "<"}, missing: []string{"-f"}},
		{name: "binary in [", d: dialect.Dash, src: "[ \"$a\" -‸", want: []string{"-eq", "-ne", "-nt"}, missing: []string{"-f", "=~"}},
		{name: "binary in test", d: dialect.Bash, src: "test \"$a\" ‸", want: []string{"=", "==", "!="}, missing: []string{"=~", "<"}},
		{name: "operand of -v", d: dialect.Bash, src: "count=1\n[[ -v co‸", want: []string{"count"}},
		{name: "operand of -o", d: dialect.Bash, src: "[[ -o pipe‸", want: []string{"pipefail"}},
		{name: "after a full test", d: dialect.Bash, src: "[[ -f x -‸", want: nil, missing: []string{"-f", "-eq"}},
		{name: "after a negation", d: dialect.Bash, src: "[[ ! -‸", want: []string{"-e"}},
		{name: "trap conditions", d: dialect.Bash, src: "trap cleanup ‸", want: []string{"EXIT", "ERR", "RETURN", "INT", "TERM"}, missing: []string{"ZERR"}},
		{name: "trap in sh", d: dialect.Posix, src: "trap cleanup ‸", want: []string{"EXIT", "INT"}, missing: []string{"ERR", "DEBUG"}},
		{name: "trap in zsh", d: dialect.Zsh, src: "trap cleanup Z‸", want: []string{"ZERR"}},
		{name: "trap with SIG", d: dialect.Bash, src: "trap cleanup SIGTE‸", want: []string{"TERM"}, missing: []string{"INT"}},
		{name: "kill -s", d: dialect.Bash, src: "kill -s ‸", want: []string{"TERM", "KILL"}, missing: []string{"EXIT"}},
		{name: "kill -SIG", d: dialect.Bash, src: "kill -TE‸", want: []string{"-TERM"}},
		{name: "kill flags", d: dialect.Bash, src: "kill -‸", want: []string{"-s", "-l", "-HUP"}},
		{name: "emulate", d: dialect.Zsh, src: "emulate -L ‸", want: []string{"zsh", "sh", "ksh"}},
		{name: "expansion operators", d: dialect.Bash, src: "echo ${name:‸", want: []string{":-", ":=", ":?", ":+", ":"}, missing: []string{"##", ":#"}},
		{name: "expansion operators in dash", d: dialect.Dash, src: "echo ${name%‸", want: []string{"%", "%%"}, missing: []string{"//"}},
		{name: "replacement only where supported", d: dialect.Dash, src: "echo ${name/‸", want: nil, missing: []string{"/", "//"}},
		{name: "transformations", d: dialect.Bash, src: "echo \"${name@‸", want: []string{"@Q", "@E", "@U"}},
		{name: "zsh set operators", d: dialect.Zsh, src: "print ${list:‸", want: []string{":#", ":|", ":*", ":-"}, missing: []string{"@Q", "^^"}},
		{name: "operator after subscript", d: dialect.Bash, src: "echo ${arr[@]:‸", want: []string{":-", ":"}},
		{name: "zsh parameter flags", d: dialect.Zsh, src: "print ${(j‸", want: []string{"s", "U", "@"}, missing: []string{"j"}},
		{name: "no zsh flags in bash", d: dialect.Bash, src: "echo ${(‸", want: nil, missing: []string{"j", "s"}},
		{name: "glob qualifiers", d: dialect.Zsh, src: "print -l *(‸", want: []string{"/", ".", "N", "o"}},
		{name: "no glob qualifiers in bash", d: dialect.Bash, src: "ls *(‸", missing: []string{"/", "N"}},
		{name: "no qualifiers in a subshell", d: dialect.Zsh, src: "(‸", missing: []string{"/", "N"}},
	}
	for _, tc := range tests {
		got := labels(complete(t, tc.d, tc.src))
		for _, want := range tc.want {
			if !slices.Contains(got, want) {
				t.Errorf("%s: %v lacks %q", tc.name, got, want)
			}
		}
		for _, missing := range tc.missing {
			if slices.Contains(got, missing) {
				t.Errorf("%s: %v offers %q", tc.name, got, missing)
			}
		}
	}
}

func TestExpansionOperatorItems(t *testing.T) {
	t.Parallel()

	got := complete(t, dialect.Bash, "echo ${name:-‸")
	it, ok := find(got, ":-")
	if !ok || it.Kind != KindOperator || it.Detail != "${name:-word}" || !strings.Contains(it.Documentation, "unset or empty") {
		t.Fatalf("item = %+v", it)
	}
	if want := (parser.Span{Start: len("echo ${name"), End: len("echo ${name:-")}); it.Replace != want {
		t.Errorf("replace = %+v, want %+v", it.Replace, want)
	}
}

func TestArrayKeys(t *testing.T) {
	t.Parallel()

	src := "declare -A conf=([host]=x [port]=1)\nconf[user]=me\necho \"${conf[mode]}\"\necho \"${conf[‸"
	got := labels(complete(t, dialect.Bash, src))
	for _, want := range []string{"host", "port", "user", "mode", "@", "*"} {
		if !slices.Contains(got, want) {
			t.Errorf("keys = %v, lacks %q", got, want)
		}
	}
	if got := labels(complete(t, dialect.Bash, "declare -A conf=([host]=x [port]=1)\necho \"${conf[ho‸")); !slices.Equal(got, []string{"host"}) {
		t.Errorf("filtered keys = %v", got)
	}
	zsh := labels(complete(t, dialect.Zsh, "typeset -A h=(alpha 1 beta 2)\nprint ${h[‸"))
	if !slices.Contains(zsh, "alpha") || !slices.Contains(zsh, "beta") || slices.Contains(zsh, "1") {
		t.Errorf("zsh pair keys = %v", zsh)
	}
}

func TestPrintfDirectives(t *testing.T) {
	t.Parallel()

	got := complete(t, dialect.Bash, "printf '%‸")
	for _, want := range []string{"%s", "%d", "%q", "%(%F)T", "%%"} {
		if _, ok := find(got, want); !ok {
			t.Errorf("printf directives = %v, lack %s", labels(got), want)
		}
	}
	it, _ := find(got, "%s")
	if it.InsertText != "s" || it.Replace != (parser.Span{Start: len("printf '%"), End: len("printf '%")}) {
		t.Errorf("%%s item = %+v", it)
	}
	if got := labels(complete(t, dialect.Dash, "printf \"%-10‸")); !slices.Contains(got, "%s") || slices.Contains(got, "%q") || slices.Contains(got, "%f") {
		t.Errorf("dash directives after a width = %v", got)
	}
	if got := labels(complete(t, dialect.Bash, "printf -v out '%‸")); !slices.Contains(got, "%s") {
		t.Errorf("directives after -v = %v", got)
	}
	for _, src := range []string{"printf '%%‸", "printf '%s' '%‸", "printf 'x‸"} {
		if got := labels(complete(t, dialect.Bash, src)); slices.Contains(got, "%s") {
			t.Errorf("%q offers directives: %v", src, got)
		}
	}
}

func TestCasePatterns(t *testing.T) {
	t.Parallel()

	options := "while getopts ':ab:v' opt; do\n\tcase $opt in\n\t\ta) all=1 ;;\n\t\t‸\n\tesac\ndone\n"
	got := complete(t, dialect.Bash, options)
	if names := labels(got); !slices.Contains(names, "b") || !slices.Contains(names, "v") || !slices.Contains(names, `\?`) || !slices.Contains(names, ":") || slices.Contains(names, "a") {
		t.Errorf("option patterns = %v", names)
	}
	if it, _ := find(got, "b"); !strings.Contains(it.Detail, "$OPTARG") || it.Kind != KindValue {
		t.Errorf("b item = %+v", it)
	}
	compared := "if [[ $mode == fast ]]; then :; fi\n[ \"$mode\" = slow ] && :\ncase \"$mode\" in\n\tf‸\nesac\n"
	if got := labels(complete(t, dialect.Bash, compared)); !slices.Equal(got, []string{"fast"}) {
		t.Errorf("compared values = %v", got)
	}
	inline := "[ \"$1\" = start ]\ncase $1 in stop) halt ;; s‸"
	if got := labels(complete(t, dialect.Posix, inline)); !slices.Equal(got, []string{"start"}) {
		t.Errorf("inline values = %v", got)
	}
	for _, src := range []string{
		"case $x in\n\ta) ec‸",
		"case $x in\n\ta) :;;\nesac\nec‸",
		"for f in\n\ta‸",
	} {
		if got := complete(t, dialect.Bash, src); slices.ContainsFunc(got.Items, func(it Item) bool { return it.Kind == KindValue }) {
			t.Errorf("%q offers case values: %v", src, labels(got))
		}
	}
	if _, ok := find(complete(t, dialect.Bash, "case $x in\n\ta) ec‸"), "echo"); !ok {
		t.Error("a case body lost command completion")
	}
}

func TestIdiomSnippets(t *testing.T) {
	t.Parallel()

	got := complete(t, dialect.Bash, "‸")
	strict, ok := find(got, "strict mode")
	if !ok || !strict.Snippet || strict.Kind != KindSnippet || strict.InsertText != "set -euo pipefail\n$0" {
		t.Fatalf("strict mode = %+v", strict)
	}
	if !strings.Contains(strict.Documentation, "```sh\nset -euo pipefail\n```") {
		t.Errorf("strict mode preview = %q", strict.Documentation)
	}
	if it, _ := find(complete(t, dialect.Dash, "str‸"), "strict mode"); it.InsertText != "set -eu\n$0" {
		t.Errorf("dash strict mode = %q", it.InsertText)
	}
	if it, _ := find(complete(t, dialect.Zsh, "scr‸"), "script directory"); !strings.Contains(it.InsertText, "${0:A:h}") {
		t.Errorf("zsh script directory = %q", it.InsertText)
	}
	tmp, _ := find(complete(t, dialect.Bash, "mk‸"), "mktemp with cleanup")
	if !strings.Contains(tmp.Documentation, "tmp=$(mktemp -d) || exit\ntrap 'rm -rf -- \"$tmp\"' EXIT") {
		t.Errorf("mktemp preview = %q", tmp.Documentation)
	}
	if _, ok := find(complete(t, dialect.Bash, "echo stri‸"), "strict mode"); ok {
		t.Error("idiom offered in argument position")
	}
	plain, err := Complete(t.Context(), Input{Tree: parser.Parse("str", dialect.Bash), Offset: 3, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := find(plain, "strict mode"); ok {
		t.Error("idiom offered to a client without snippet support")
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

func TestNestedSubcommands(t *testing.T) {
	t.Parallel()

	if got := labels(complete(t, dialect.Bash, "git -C dir remote ‸")); !slices.Equal(got, []string{"add", "rename"}) {
		t.Errorf("subcommands of git remote after a global option = %v", got)
	}
	if got := labels(complete(t, dialect.Bash, "git -C dir remote add -‸")); !slices.Equal(got, []string{"-t"}) {
		t.Errorf("flags of git remote add = %v", got)
	}
	loading := complete(t, dialect.Bash, "git remote a‸")
	if !slices.Equal(labels(loading), []string{"add"}) || !loading.Incomplete {
		t.Errorf("subcommands with pages still loading = %v, incomplete %t", labels(loading), loading.Incomplete)
	}
}

func TestWrappedCommands(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"sudo git ‸", "sudo -u root -E git ‸", "doas -u admin git ‸", "env -i FOO=1 git ‸", "nice -n 5 git ‸", "nohup git ‸",
		"timeout -s KILL 5 git ‸", "watch -n 1 git ‸", "find . | xargs -0 -n1 git ‸", "command git ‸", "exec -a name git ‸",
		"sudo env FOO=1 nice git ‸",
	} {
		if got := labels(complete(t, dialect.Bash, src)); !slices.Equal(got, []string{"checkout", "commit"}) {
			t.Errorf("%q = %v, want the subcommands of git", src, got)
		}
	}
	if got := labels(complete(t, dialect.Bash, "sudo git -q checkout -‸")); !slices.Equal(got, []string{"-b"}) {
		t.Errorf("flags of a wrapped subcommand = %v", got)
	}
	if got := labels(complete(t, dialect.Bash, "nice -n 5 grep --ext‸")); !slices.Equal(got, []string{"--extended-regexp"}) {
		t.Errorf("flags of a wrapped command = %v", got)
	}
	if got := labels(complete(t, dialect.Bash, "sudo --u‸")); !slices.Equal(got, []string{"--user"}) {
		t.Errorf("flags of the wrapper itself = %v", got)
	}
	for _, src := range []string{"sudo -u ‸", "timeout ‸", "env -C ‸"} {
		if got := labels(complete(t, dialect.Bash, src)); !slices.Contains(got, "lib/") || slices.Contains(got, "git") {
			t.Errorf("%q = %v, want the value of the wrapper's option completed as a path", src, got)
		}
	}
	for _, src := range []string{"sudo -u root gr‸", "env FOO=1 gr‸", "timeout 5 gr‸", "command -p gr‸", "xargs -I {} gr‸"} {
		if _, ok := find(complete(t, dialect.Bash, src), "grep"); !ok {
			t.Errorf("%q: grep not offered as the wrapped command", src)
		}
	}
	if got := labels(complete(t, dialect.Bash, "sudo ./li‸")); !slices.Equal(got, []string{"lib/"}) {
		t.Errorf("path as the wrapped command = %v", got)
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
	if got := labels(complete(t, dialect.Bash, "cd "+absoluteApp+"\ncat ‸")); !slices.Contains(got, "config.yml") {
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
