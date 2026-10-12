package hover

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/manpage"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type fakeCommands struct {
	docs        map[string]string
	subcommands map[string]manpage.Subcommand
	options     map[string][]manpage.Option
	err         error
}

func (f fakeCommands) Describe(_ context.Context, name string) (string, error) {
	doc, ok := f.docs[name]
	if !ok {
		return "", errors.New("no such command " + name)
	}
	return doc, nil
}

func (f fakeCommands) Subcommand(_ context.Context, command string, args []string) (manpage.Subcommand, error) {
	sub, ok := f.subcommands[strings.Join(append([]string{command}, args...), " ")]
	if !ok {
		return manpage.Subcommand{}, manpage.ErrNoPage
	}
	return sub, nil
}

func (f fakeCommands) Options(_ context.Context, command string, args []string) ([]manpage.Option, error) {
	if f.err != nil {
		return nil, f.err
	}
	if options, ok := f.options[strings.Join(append([]string{command}, args...), " ")]; ok {
		return options, nil
	}
	if options, ok := f.options[command]; ok {
		return options, nil
	}
	return nil, manpage.ErrNoPage
}

const script = `#!/bin/bash
# Greets someone.
greet() {
	local -a names=("$1" "$2")
	printf '%s\n' "${names[@]}" "${#names}" "${name:-anon}" "$@"
}
count=0
export LOG_DIR=/tmp
for f in *.txt; do
	greet "$f" "$count"
	(( count += 1 ))
done
if [[ -n $HOME ]]; then ls "$LOG_DIR"; fi
echo "$BASH_SOURCE $UNSET_THING $1"
`

func hoverAt(t *testing.T, src string, d dialect.Dialect, needle string, nth int) Result {
	t.Helper()

	offset := -1
	for range nth + 1 {
		next := strings.Index(src[offset+1:], needle)
		if next < 0 {
			t.Fatalf("needle %q #%d not found", needle, nth)
		}
		offset += next + 1
	}
	got, ok, err := Hover(t.Context(), Input{
		Tree:     parser.Parse(src, d),
		Offset:   offset,
		Commands: fakeCommands{docs: map[string]string{"ls": "**ls - list directory contents**"}},
	})
	if err != nil {
		t.Fatalf("Hover(%q) error = %v", needle, err)
	}
	if !ok {
		t.Fatalf("Hover(%q) found nothing", needle)
	}
	return got
}

func TestHover(t *testing.T) {
	t.Parallel()

	tests := []struct {
		needle string
		nth    int
		want   []string
		span   string
	}{
		{needle: "greet()", want: []string{"greet $1 $2 [args...]", "Greets someone.", "line 3"}, span: "greet"},
		{needle: "greet \"$f\"", want: []string{"greet $1 $2 [args...]"}, span: "greet"},
		{needle: "local", want: []string{"local [option] name", "**bash:**"}, span: "local"},
		{needle: "names[@]", want: []string{"local array in `greet`", "Declared on line 4", "local -a names"}, span: "${names[@]}"},
		{needle: "#names", want: []string{"`${#name}`: Length"}, span: "${#names}"},
		{needle: "name:-", want: []string{"not assigned in this file", "`${name:-word}`"}, span: "${name:-anon}"},
		{needle: "$1\"", want: []string{"argument 1 of function `greet`"}, span: "$1"},
		{needle: "$@", want: []string{"All positional parameters"}, span: "$@"},
		{needle: "count=0", want: []string{"global variable", "Assigned in 2 places"}, span: "count"},
		{needle: "count +=", want: []string{"global variable"}, span: "count"},
		{needle: "LOG_DIR=", want: []string{"exported global variable"}, span: "LOG_DIR"},
		{needle: "f in", want: []string{"(loop variable)"}, span: "f"},
		{needle: "for", want: []string{"for name [in words ...]", "**bash:**"}, span: "for"},
		{needle: "done", want: []string{"Ends the body of a loop"}, span: "done"},
		{needle: "[[", want: []string{"Conditional expression"}, span: "[["},
		{needle: "HOME", want: []string{"Home directory"}, span: "$HOME"},
		{needle: "ls ", want: []string{"list directory contents"}, span: "ls"},
		{needle: "BASH_SOURCE", want: []string{"source file names"}, span: "$BASH_SOURCE"},
		{needle: "$1", nth: 1, want: []string{"positional parameter 1 of the script"}, span: "$1"},
	}
	for _, tc := range tests {
		got := hoverAt(t, script, dialect.Bash, tc.needle, tc.nth)
		for _, want := range tc.want {
			if !strings.Contains(got.Markdown, want) {
				t.Errorf("hover %q: markdown lacks %q:\n%s", tc.needle, want, got.Markdown)
			}
		}
		if span := script[got.Span.Start:got.Span.End]; span != tc.span {
			t.Errorf("hover %q: span = %q, want %q", tc.needle, span, tc.span)
		}
	}
}

func TestHoverDialectNotes(t *testing.T) {
	t.Parallel()

	got := hoverAt(t, "local x=1\n", dialect.Posix, "local", 0)
	if !strings.Contains(got.Markdown, "_Not available in sh._") {
		t.Errorf("posix local hover = %q", got.Markdown)
	}
	got = hoverAt(t, "echo -n hi\n", dialect.Dash, "echo", 0)
	if !strings.Contains(got.Markdown, "**dash:**") {
		t.Errorf("dash echo hover = %q", got.Markdown)
	}
}

func TestHoverZshFlagsAndModifiers(t *testing.T) {
	t.Parallel()

	src := "print -r -- ${(j:,:)parts} ${(U)name} ${file:t:r}\n"
	got := hoverAt(t, src, dialect.Zsh, "j:,:", 0)
	if !strings.Contains(got.Markdown, "- `j`: Join") || strings.Contains(got.Markdown, "`,`") {
		t.Errorf("flags hover = %q", got.Markdown)
	}
	got = hoverAt(t, src, dialect.Zsh, "(U)", 0)
	if !strings.Contains(got.Markdown, "- `U`: Convert to uppercase") {
		t.Errorf("U flag hover = %q", got.Markdown)
	}
	got = hoverAt(t, src, dialect.Zsh, "file:t", 0)
	if !strings.Contains(got.Markdown, "- `:t`: Tail") || !strings.Contains(got.Markdown, "- `:r`: Root") {
		t.Errorf("modifier hover = %q", got.Markdown)
	}
}

func TestHoverExpansionOperators(t *testing.T) {
	t.Parallel()

	src := "echo \"${a/x/y}\" \"${b//x/y}\" \"${c/#x/y}\" \"${d/%x/y}\" \"${e@Q}\" \"${f:1:2}\" \"${g^^}\"\n"
	for _, tc := range []struct{ needle, want string }{
		{"a/", "`${name/pattern/string}`: Replaces the first match"},
		{"b//", "`${name//pattern/string}`: Replaces every match"},
		{"c/#", "`${name/#pattern/string}`"},
		{"d/%", "`${name/%pattern/string}`"},
		{"e@", "`${name@Q}`: Quotes the value"},
		{"f:", "`${name:offset:length}`"},
		{"g^", "`${name^^pattern}`"},
	} {
		got := hoverAt(t, src, dialect.Bash, tc.needle, 0)
		if !strings.Contains(got.Markdown, tc.want) {
			t.Errorf("hover %q = %q, want it to contain %q", tc.needle, got.Markdown, tc.want)
		}
	}
}

func TestHoverShellVocabulary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		d           dialect.Dialect
		src, needle string
		nth         int
		want, span  string
		wantMissing string
	}{
		{d: dialect.Bash, src: "read -r line\n", needle: "-r", want: "**read -r**", span: "-r"},
		{d: dialect.Bash, src: "read -rs pw\n", needle: "-rs", want: "- `-s`: In bash, zsh and busybox sh, do not echo", span: "-rs"},
		{d: dialect.Zsh, src: "typeset -U path\n", needle: "-U", want: "Keep only the first occurrence", span: "-U"},
		{d: dialect.Bash, src: "[ -f x ] && test a -nt b\n", needle: "-f", want: "```sh\n-f file\n```", span: "-f"},
		{d: dialect.Bash, src: "[ -f x ] && test a -nt b\n", needle: "-nt", want: "modified more recently", span: "-nt"},
		{d: dialect.Bash, src: "[[ -d $x && $y =~ ^a ]]\n", needle: "-d", want: "is a directory", span: "-d"},
		{d: dialect.Bash, src: "[[ -d $x && $y =~ ^a ]]\n", needle: "=~", want: "extended regular expression", span: "=~"},
		{d: dialect.Bash, src: "[[ -d $x && $y =~ ^a ]]\n", needle: "&&", want: "both expressions are true", span: "&&"},
		{d: dialect.Bash, src: "trap cleanup EXIT INT\n", needle: "EXIT", want: "when the shell exits", span: "EXIT"},
		{d: dialect.Bash, src: "trap cleanup EXIT INT\n", needle: "INT", want: "**SIGINT**", span: "INT"},
		{d: dialect.Bash, src: "kill -TERM 1; kill -s HUP 2\n", needle: "-TERM", want: "**SIGTERM**", span: "-TERM"},
		{d: dialect.Bash, src: "kill -TERM 1; kill -s HUP 2\n", needle: "HUP", want: "**SIGHUP**", span: "HUP"},
		{d: dialect.Bash, src: "set -o pipefail\n", needle: "pipefail", want: "fail when any of its commands fails", span: "pipefail"},
		{d: dialect.Bash, src: "shopt -s nullglob\n", needle: "nullglob", want: "expand to nothing", span: "nullglob"},
		{d: dialect.Bash, src: "shopt -so pipefail\n", needle: "pipefail", want: "fail when any of its commands fails", span: "pipefail"},
		{d: dialect.Zsh, src: "setopt EXTENDED_GLOB NO_ERR_EXIT\n", needle: "EXTENDED", want: "**extended_glob**", span: "EXTENDED_GLOB"},
		{d: dialect.Zsh, src: "setopt EXTENDED_GLOB NO_ERR_EXIT\n", needle: "NO_ERR", want: "negates `err_exit`", span: "NO_ERR_EXIT"},
		{d: dialect.Zsh, src: "emulate -L zsh\n", needle: "zsh", want: "native zsh option", span: "zsh"},
		{d: dialect.Bash, src: "cmd >> log 2>&1\n", needle: ">>", want: "appending", span: ">>"},
		{d: dialect.Bash, src: "cmd >> log 2>&1\n", needle: ">&", want: "copy of descriptor", span: "2>&"},
		{d: dialect.Posix, src: "cmd &> log\n", needle: "&>", want: "_Not available in sh._", span: "&>"},
		{d: dialect.Bash, src: "cat <<'EOF'\n$x\nEOF\n", needle: "<<", want: "taken literally", span: "<<"},
		{d: dialect.Bash, src: "cat <<EOF\n$x\nEOF\n", needle: "<<", want: "delimiter is unquoted", span: "<<"},
		{d: dialect.Ksh, src: "cat <<#EOF\n\t$x\n\tEOF\n", needle: "<<#", want: "strips leading blanks", span: "<<#"},
		{d: dialect.Ksh, src: "cat <<#EOF\n\t$x\n\tEOF\n", needle: "<<#", want: "delimiter is unquoted", span: "<<#"},
		{d: dialect.Ksh, src: "cat <<#'EOF'\n\t$x\n\tEOF\n", needle: "<<#", want: "taken literally", span: "<<#"},
		{d: dialect.Bash, src: "a && b || c | d\n", needle: "||", want: "Runs `cmd2` only if `cmd1` fails", span: "||"},
		{d: dialect.Bash, src: "a && b || c | d\n", needle: "|", nth: 2, want: "Pipes the output", span: "|"},
		{d: dialect.Bash, src: "echo $(( a ** 2 )); (( i++ ))\n", needle: "**", want: "Exponentiation", span: "**"},
		{d: dialect.Bash, src: "echo $(( a ** 2 )); (( i++ ))\n", needle: "++", want: "Increment", span: "++"},
		{d: dialect.Bash, src: "case $x in a) b ;& c) d ;; esac\n", needle: ";&", want: "falls through", span: ";&"},
		{d: dialect.Bash, src: "echo $-\n", needle: "$-", want: "- `e`: `errexit`", span: "$-", wantMissing: "- `X`"},
		{d: dialect.Dash, src: "echo $-\n", needle: "$-", want: "- `x`: `xtrace`", span: "$-", wantMissing: "braceexpand"},
	}
	for _, tc := range tests {
		got := hoverAt(t, tc.src, tc.d, tc.needle, tc.nth)
		if !strings.Contains(got.Markdown, tc.want) {
			t.Errorf("%s hover %q in %q = %q, want %q", tc.d, tc.needle, tc.src, got.Markdown, tc.want)
		}
		if tc.wantMissing != "" && strings.Contains(got.Markdown, tc.wantMissing) {
			t.Errorf("%s hover %q = %q, should not contain %q", tc.d, tc.needle, got.Markdown, tc.wantMissing)
		}
		if span := tc.src[got.Span.Start:got.Span.End]; span != tc.span {
			t.Errorf("%s hover %q span = %q, want %q", tc.d, tc.needle, span, tc.span)
		}
	}
}

func TestHoverReadsArgumentsByTheirPosition(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ src, needle, unwanted string }{
		{src: "kill -ERR 1\n", needle: "-ERR", unwanted: "command fails"},
		{src: "kill -s EXIT 1\n", needle: "EXIT", unwanted: "shell exits"},
		{src: "[ \"$x\" = -f ]\n", needle: "-f", unwanted: "regular file"},
	} {
		at := strings.Index(tc.src, tc.needle)
		got, _, err := Hover(t.Context(), Input{Tree: parser.Parse(tc.src, dialect.Bash), Offset: at, Commands: fakeCommands{}})
		if err != nil {
			t.Fatalf("Hover(%q) error = %v", tc.needle, err)
		}
		if strings.Contains(got.Markdown, tc.unwanted) {
			t.Errorf("hover %q in %q = %q, should not mention %q", tc.needle, tc.src, got.Markdown, tc.unwanted)
		}
	}
}

func TestHoverShadowingPrefersLocals(t *testing.T) {
	t.Parallel()

	src := "x=global\nf() {\n\tlocal x=inner\n\techo \"$x\"\n}\necho \"$x\"\n"
	inner := hoverAt(t, src, dialect.Bash, "$x", 0)
	if !strings.Contains(inner.Markdown, "local variable in `f`") {
		t.Errorf("inner hover = %q", inner.Markdown)
	}
	outer := hoverAt(t, src, dialect.Bash, "$x", 1)
	if !strings.Contains(outer.Markdown, "global variable") || !strings.Contains(outer.Markdown, "line 1") {
		t.Errorf("outer hover = %q", outer.Markdown)
	}
}

func TestHoverCommandLookupError(t *testing.T) {
	t.Parallel()

	_, _, err := Hover(t.Context(), Input{
		Tree:     parser.Parse("unknown-tool --flag\n", dialect.Bash),
		Offset:   2,
		Commands: fakeCommands{},
	})
	if err == nil {
		t.Error("Hover() swallowed the command lookup error")
	}
}

func TestHoverSubcommandsAndFlags(t *testing.T) {
	t.Parallel()

	commands := fakeCommands{
		subcommands: map[string]manpage.Subcommand{"tool -v stop": {Name: "stop", Doc: "**tool-stop**"}},
		options: map[string][]manpage.Option{
			"tool":         {{Names: []string{"-v", "--verbose"}, Doc: "be verbose"}, {Names: []string{"--width"}, Doc: "set the width"}},
			"tool -v stop": {{Names: []string{"-f", "--force"}, Doc: "kill them"}},
		},
	}
	src := "tool -v stop --force web\ntool --width=80 '-v'\nf() { :; }\nf stop\ntool $(tool -v)\n"
	at := func(needle string, nth int) (Result, bool) {
		t.Helper()
		offset := -1
		for range nth + 1 {
			offset += strings.Index(src[offset+1:], needle) + 1
		}
		got, ok, err := Hover(t.Context(), Input{Tree: parser.Parse(src, dialect.Bash), Offset: offset + 1, Commands: commands})
		if err != nil {
			t.Fatalf("Hover(%q #%d) error = %v", needle, nth, err)
		}
		return got, ok
	}
	if got, ok := at("stop", 0); !ok || got.Markdown != "**tool-stop**" || got.Span != (parser.Span{Start: 8, End: 12}) {
		t.Errorf("subcommand hover = %+v, %t", got, ok)
	}
	if got, ok := at("--force", 0); !ok || got.Markdown != "**-f, --force**\n\nkill them" {
		t.Errorf("subcommand flag hover = %+v, %t", got, ok)
	}
	if got, ok := at("-v", 0); !ok || !strings.Contains(got.Markdown, "be verbose") {
		t.Errorf("command flag hover = %+v, %t", got, ok)
	}
	if got, ok := at("--width", 0); !ok || !strings.Contains(got.Markdown, "set the width") {
		t.Errorf("flag with a value hover = %+v, %t", got, ok)
	}
	if got, ok := at("-v", 1); !ok || !strings.Contains(got.Markdown, "be verbose") {
		t.Errorf("quoted flag hover = %+v, %t", got, ok)
	}
	if got, ok := at("-v", 2); !ok || !strings.Contains(got.Markdown, "be verbose") {
		t.Errorf("flag of a command inside a substitution = %+v, %t", got, ok)
	}
	for _, needle := range []struct {
		text string
		nth  int
	}{{"web", 0}, {"stop", 1}} {
		if got, ok := at(needle.text, needle.nth); ok {
			t.Errorf("Hover(%q #%d) = %+v, want nothing", needle.text, needle.nth, got)
		}
	}

	failing := commands
	failing.err = errors.New("unreadable page")
	if _, _, err := Hover(t.Context(), Input{Tree: parser.Parse("tool -v\n", dialect.Bash), Offset: 6, Commands: failing}); err == nil {
		t.Error("Hover() swallowed the option lookup error")
	}
}

func TestHoverNestedSubcommands(t *testing.T) {
	t.Parallel()

	commands := fakeCommands{
		subcommands: map[string]manpage.Subcommand{
			"tool -C dir remote":     {Name: "remote", Doc: "**tool-remote**"},
			"tool -C dir remote add": {Name: "add", Doc: "**tool remote add**"},
		},
		options: map[string][]manpage.Option{
			"tool -C dir remote add": {{Names: []string{"-f"}, Doc: "fetch at once"}},
		},
	}
	src := "tool -C dir remote add -f origin\n"
	for needle, want := range map[string]string{"remote": "**tool-remote**", "add": "**tool remote add**", "-f": "**-f**\n\nfetch at once"} {
		got, ok, err := Hover(t.Context(), Input{Tree: parser.Parse(src, dialect.Bash), Offset: strings.Index(src, needle) + 1, Commands: commands})
		if err != nil || !ok || got.Markdown != want {
			t.Errorf("Hover(%q) = %+v, %t, %v; want %q", needle, got, ok, err, want)
		}
	}
	for _, needle := range []string{"dir", "origin"} {
		if got, ok, err := Hover(t.Context(), Input{Tree: parser.Parse(src, dialect.Bash), Offset: strings.Index(src, needle) + 1, Commands: commands}); ok || err != nil {
			t.Errorf("Hover(%q) = %+v, %v; want nothing", needle, got, err)
		}
	}
}

func TestHoverLooksThroughWrappers(t *testing.T) {
	t.Parallel()

	commands := fakeCommands{
		docs: map[string]string{"tool": "**tool - manage widgets**"},
		subcommands: map[string]manpage.Subcommand{
			"tool -C dir remote add": {Name: "add", Doc: "**tool remote add**"},
			"tool start":             {Name: "start", Doc: "**tool-start**"},
		},
		options: map[string][]manpage.Option{
			"sudo":                   {{Names: []string{"-u", "--user"}, Doc: "run as user", Valued: true}},
			"tool -C dir remote add": {{Names: []string{"-f"}, Doc: "fetch at once"}},
			"tool":                   {{Names: []string{"-w"}, Doc: "wait"}},
		},
	}
	src := "sudo -u root env -i A=1 tool -C dir remote add -f origin\ntimeout 5 nice -n 3 tool start -w\nhelper() { :; }\ncommand helper start\nrun tool start\n"
	at := func(needle string, nth int, bats bool) (Result, bool) {
		t.Helper()
		offset := -1
		for range nth + 1 {
			offset += strings.Index(src[offset+1:], needle) + 1
		}
		lang := syntax.LangBash
		if bats {
			lang = syntax.LangBats
		}
		tree := parser.ParseEdit(nil, src, dialect.Bash, lang)
		got, ok, err := Hover(t.Context(), Input{Tree: tree, Offset: offset + 1, Commands: commands})
		if err != nil {
			t.Fatalf("Hover(%q #%d) error = %v", needle, nth, err)
		}
		return got, ok
	}
	for _, tc := range []struct {
		needle string
		nth    int
		bats   bool
		want   string
	}{
		{"tool", 0, false, "**tool - manage widgets**"},
		{"add", 0, false, "**tool remote add**"},
		{"-f", 0, false, "**-f**\n\nfetch at once"},
		{"-u", 0, false, "**-u, --user**\n\nrun as user"},
		{"start", 0, false, "**tool-start**"},
		{"-w", 0, false, "**-w**\n\nwait"},
		{"start", 2, true, "**tool-start**"},
	} {
		got, ok := at(tc.needle, tc.nth, tc.bats)
		if !ok || got.Markdown != tc.want {
			t.Errorf("Hover(%q #%d) = %+v, %t; want %q", tc.needle, tc.nth, got, ok, tc.want)
		}
	}
	for _, tc := range []struct {
		needle string
		nth    int
	}{{"origin", 0}, {"root", 0}, {"A=1", 0}, {"start", 1}, {"start", 2}} {
		if got, ok := at(tc.needle, tc.nth, false); ok {
			t.Errorf("Hover(%q #%d) = %+v, want nothing", tc.needle, tc.nth, got)
		}
	}
}

func TestHoverNothing(t *testing.T) {
	t.Parallel()

	_, ok, err := Hover(t.Context(), Input{Tree: parser.Parse("echo hi   \n", dialect.Bash), Offset: 8})
	if err != nil || ok {
		t.Errorf("Hover(whitespace) = %t, %v; want nothing", ok, err)
	}
}

func TestFunctionSignature(t *testing.T) {
	t.Parallel()

	src := "# Copies a file.\n# $1 - source path\n# dest: where to copy\ncopy() {\n\tlocal src=\"$1\" dest=${2:-.}\n\tshift 2\n\tcp \"$src\" \"$dest\" \"$@\"\n}\n"
	tree := parser.Parse(src, dialect.Bash)
	sig := FunctionSignature(tree, tree.Functions[0])
	if sig.Label() != "copy src dest [args...]" || sig.Doc != "Copies a file." {
		t.Errorf("signature = %q, doc %q", sig.Label(), sig.Doc)
	}
	if len(sig.Params) != 2 || sig.Params[0].Doc != "source path" || sig.Params[1].Doc != "where to copy" {
		t.Errorf("params = %+v", sig.Params)
	}
}
