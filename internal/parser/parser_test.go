package parser

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

var allDialects = []dialect.Dialect{dialect.Bash, dialect.Zsh, dialect.Posix, dialect.Dash, dialect.Ksh}

func messages(tree *Tree) []string {
	out := make([]string, 0, len(tree.Errors))
	for _, e := range tree.Errors {
		out = append(out, e.Message)
	}
	return out
}

func TestValidScriptHasNoErrors(t *testing.T) {
	t.Parallel()

	tree := Parse("#!/bin/bash\nf() { echo \"$1\"; }\nf x\n", dialect.Bash)
	if len(tree.Errors) != 0 {
		t.Errorf("Errors = %v, want none", messages(tree))
	}
	if len(tree.File.Stmts) != 2 {
		t.Errorf("statements = %d, want 2", len(tree.File.Stmts))
	}
}

func TestRecoveredClosersReportMissingTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src     string
		message string
		at      string
	}{
		{src: "if true; then\n  echo x\n", message: "missing `fi` to match `if`", at: "if"},
		{src: "while true; do\n  echo x\n", message: "missing `done` to match `while`", at: "while"},
		{src: "f() {\n  ls\n", message: "missing `}` to match `{`", at: "{"},
		{src: "echo $(date\n", message: "missing `)` to match `$(`", at: "$(date"},
		{src: "echo \"open\n", message: "missing `\"` to match `\"`", at: "\"open"},
		{src: "case x in\n", message: "missing `esac` to match `case`", at: "case"},
		{src: "true &&\n", message: "expected a command after `&&`", at: "&&"},
		{src: "if true; then\n", message: "expected a command after `then`", at: "then"},
		{src: "while true; do\n", message: "expected a command after `do`", at: "do"},
	}
	for _, tc := range tests {
		tree := Parse(tc.src, dialect.Bash)
		found := false
		for _, e := range tree.Errors {
			if e.Message == tc.message && tc.src[e.Span.Start:e.Span.End] == tc.at {
				found = true
			}
		}
		if !found {
			t.Errorf("Parse(%q) errors = %+v, want %q at %q", tc.src, tree.Errors, tc.message, tc.at)
		}
	}
}

func TestUnrecoverableErrorsKeepParsingLaterLines(t *testing.T) {
	t.Parallel()

	src := "echo ok\necho ${bad\nf() { ls; }\necho )\necho end\n"
	tree := Parse(src, dialect.Bash)
	if len(tree.Errors) != 2 {
		t.Fatalf("Errors = %v, want 2", messages(tree))
	}
	if line := strings.Count(src[:tree.Errors[0].Span.Start], "\n"); line != 1 {
		t.Errorf("first error on line %d, want 1", line)
	}
	if line := strings.Count(src[:tree.Errors[1].Span.Start], "\n"); line != 3 {
		t.Errorf("second error on line %d, want 3", line)
	}
	if len(tree.Functions) != 1 || tree.Functions[0].Name != "f" {
		t.Errorf("Functions = %+v, want f after the broken line", tree.Functions)
	}
	names := map[string]bool{}
	for _, c := range tree.Commands {
		names[src[c.NameSpan.Start:c.NameSpan.End]] = true
	}
	if !names["ls"] {
		t.Errorf("commands after errors missing: %v", names)
	}
}

func TestDialectErrorsAreTyped(t *testing.T) {
	t.Parallel()

	tree := Parse("echo ${name^^}\n", dialect.Ksh)
	if len(tree.Errors) == 0 {
		t.Fatal("no errors for bash case conversion in ksh")
	}
	if tree.Errors[0].Kind != ErrDialect {
		t.Errorf("Kind = %d, want ErrDialect: %s", tree.Errors[0].Kind, tree.Errors[0].Message)
	}
	for _, d := range []dialect.Dialect{dialect.Bash, dialect.Posix, dialect.Dash} {
		if got := Parse("arr=(1 2)\n[[ -n $x ]]\n", d); len(got.Errors) != 0 {
			t.Errorf("%s rejected bash syntax that linting reports instead: %v", d, messages(got))
		}
	}
}

func TestZshConstructsParse(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"print -l *(.)\n",
		"x=${(j:,:)arr}\n",
		"diff =(sort a) =(sort b)\n",
		"echo ${var:t:r}\n",
		"repeat 3 echo hi\n",
		"function { echo anon }\n",
		"echo $arr[1]\n",
	} {
		if tree := Parse(src, dialect.Zsh); len(tree.Errors) != 0 {
			t.Errorf("Parse(%q, zsh) errors = %v", src, messages(tree))
		}
	}
}

func TestScopesAndLocals(t *testing.T) {
	t.Parallel()

	src := "g=1\nf() {\n  local l=2\n  declare d=3\n  declare -g G=4\n  h=5\n  echo $l $g\n}\n"
	tree := Parse(src, dialect.Bash)
	if len(tree.Functions) != 1 {
		t.Fatalf("Functions = %+v", tree.Functions)
	}
	body := tree.Functions[0].Body
	want := map[string]struct {
		local  bool
		within ScopeID
	}{
		"g": {false, FileScope}, "l": {true, body}, "d": {true, body}, "G": {false, body}, "h": {false, body},
	}
	for _, def := range tree.Definitions {
		w, ok := want[def.Name]
		if !ok {
			t.Errorf("unexpected definition %s", def.Name)
			continue
		}
		if def.Local != w.local || def.Within != w.within {
			t.Errorf("definition %s local=%t within=%d, want local=%t within=%d", def.Name, def.Local, def.Within, w.local, w.within)
		}
	}
	refs := 0
	for _, ref := range tree.References {
		if ref.Within == body && (ref.Name == "l" || ref.Name == "g") {
			refs++
		}
	}
	if refs != 2 {
		t.Errorf("references in body = %d, want 2: %+v", refs, tree.References)
	}
}

func TestScopeAt(t *testing.T) {
	t.Parallel()

	src := "a=1\nouter() {\n  inner() { echo in; }\n  echo mid\n}\nb=2\nnext() { echo nx; }\nc=3\n"
	tree := Parse(src, dialect.Bash)
	body := map[string]ScopeID{}
	for _, fn := range tree.Functions {
		body[fn.Name] = fn.Body
	}
	for _, tc := range []struct {
		at   string
		want ScopeID
	}{
		{"a=1", FileScope},
		{"in;", body["inner"]},
		{"mid", body["outer"]},
		{"b=2", FileScope},
		{"nx", body["next"]},
		{"c=3", FileScope},
	} {
		if got := tree.ScopeAt(strings.Index(src, tc.at)); got != tc.want {
			t.Errorf("ScopeAt(%q) = %d, want %d", tc.at, got, tc.want)
		}
	}
	if got := tree.ScopeAt(-1); got != FileScope {
		t.Errorf("ScopeAt(-1) = %d, want FileScope", got)
	}
}

func TestArithmeticDefinitionsAndReferences(t *testing.T) {
	t.Parallel()

	tree := Parse("(( a = b + 1 ))\n(( c += 1 ))\n(( d++ ))\n", dialect.Bash)
	defs := map[string]bool{}
	for _, d := range tree.Definitions {
		defs[d.Name] = true
	}
	refs := map[string]bool{}
	for _, r := range tree.References {
		refs[r.Name] = true
	}
	for _, name := range []string{"a", "c", "d"} {
		if !defs[name] {
			t.Errorf("missing arithmetic definition of %s", name)
		}
	}
	if refs["a"] {
		t.Error("plain assignment target recorded as a read")
	}
	for _, name := range []string{"b", "c", "d"} {
		if !refs[name] {
			t.Errorf("missing arithmetic reference to %s", name)
		}
	}
}

func TestPosixDeclarationBuiltins(t *testing.T) {
	t.Parallel()

	tree := Parse("f() { local a=1 b; }\nexport X=1 Y\nreadonly R=2\n", dialect.Dash)
	got := map[string]Definition{}
	for _, d := range tree.Definitions {
		got[d.Name] = d
	}
	if !got["a"].Local || !got["b"].Local {
		t.Errorf("dash local not marked local: %+v", got)
	}
	if !got["X"].Attrs.Has(AttrExport) || !got["Y"].Attrs.Has(AttrExport) {
		t.Errorf("export attrs missing: %+v", got)
	}
	if !got["R"].Attrs.Has(AttrReadonly) {
		t.Errorf("readonly attr missing: %+v", got["R"])
	}
}

func TestFunctionDocComment(t *testing.T) {
	t.Parallel()

	src := "#!/bin/bash\n# shellcheck disable=SC2034\n# First line.\n#  indented\nf() { :; }\n\n# detached\n\ng() { :; }\n"
	tree := Parse(src, dialect.Bash)
	if len(tree.Functions) != 2 {
		t.Fatalf("Functions = %+v", tree.Functions)
	}
	if got := tree.Functions[0].Doc; got != "First line.\n indented" {
		t.Errorf("f doc = %q", got)
	}
	if got := tree.Functions[1].Doc; got != "" {
		t.Errorf("g doc = %q, want empty for a comment separated by a blank line", got)
	}
}

func TestHeredocs(t *testing.T) {
	t.Parallel()

	src := "cat <<EOF\n$x\nEOF\ncat <<-'Q'\n\tbody\n\tQ\ncat <<E2\nE2\n"
	tree := Parse(src, dialect.Bash)
	if len(tree.Heredocs) != 3 {
		t.Fatalf("Heredocs = %+v", tree.Heredocs)
	}
	first, second, empty := tree.Heredocs[0], tree.Heredocs[1], tree.Heredocs[2]
	if first.Delimiter != "EOF" || first.Quoted || src[first.Body.Start:first.Body.End] != "$x\n" {
		t.Errorf("first heredoc = %+v", first)
	}
	if second.Delimiter != "Q" || !second.Quoted || !second.StripTabs {
		t.Errorf("second heredoc = %+v", second)
	}
	if empty.Body.Start != empty.Body.End {
		t.Errorf("empty heredoc body = %+v", empty.Body)
	}
}

func TestLiteral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want string
		ok   bool
	}{
		{src: `plain`, want: "plain", ok: true},
		{src: `'single $x'`, want: "single $x", ok: true},
		{src: `"dq \$ \a"`, want: `dq $ \a`, ok: true},
		{src: `es\ caped`, want: "es caped", ok: true},
		{src: `mix'ed'"parts"`, want: "mixedparts", ok: true},
		{src: `"$HOME/x"`, ok: false},
		{src: `$'ansi'`, ok: false},
	}
	for _, tc := range tests {
		file, err := syntax.NewParser().Parse(strings.NewReader("echo "+tc.src), "")
		if err != nil {
			t.Fatalf("parsing %q: %v", tc.src, err)
		}
		call, isCall := file.Stmts[0].Cmd.(*syntax.CallExpr)
		if !isCall {
			t.Fatalf("parsing %q: not a simple command", tc.src)
		}
		got, ok := Literal(call.Args[1])
		if ok != tc.ok || got != tc.want {
			t.Errorf("Literal(%s) = %q, %t; want %q, %t", tc.src, got, ok, tc.want, tc.ok)
		}
	}
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"echo ${bad\n", "if true; then\n", "f() {", "cat <<EOF\n", "echo \"x", "(( a = ",
		"for f (*.txt); do; done", "case $x in (a) ;; esac", "${(j:,:)a}", "a=(1 2", "echo $(\n)\n)",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		for _, d := range allDialects {
			tree := Parse(src, d)
			check := func(what string, s Span) {
				if s.Start < 0 || s.End > len(src) || s.Start > s.End {
					t.Fatalf("%s: %s span %+v out of bounds for %d bytes", d, what, s, len(src))
				}
			}
			for _, e := range tree.Errors {
				check("error", e.Span)
			}
			for _, def := range tree.Definitions {
				check("definition", def.NameSpan)
			}
			for _, ref := range tree.References {
				check("reference", ref.Span)
			}
			for _, fn := range tree.Functions {
				check("function", fn.Span)
			}
			for _, c := range tree.Commands {
				check("command", c.Span)
			}
		}
	})
}

func TestBuiltinAssignments(t *testing.T) {
	t.Parallel()

	src := "read -r line\nread -rp 'Name: ' name rest\nread -a parts\nmapfile -t lines < f\nprintf -v out '%s' x\ngetopts 'ab:' opt\nread -r \"$dynamic\"\nwait -n -p done %1\n"
	tree := Parse(src, dialect.Bash)
	got := map[string]Attr{}
	for _, d := range tree.Definitions {
		if d.Kind == DefBuiltin {
			got[d.Name] = d.Attrs
		}
	}
	want := map[string]Attr{"line": 0, "name": 0, "rest": 0, "parts": AttrArray, "lines": AttrArray, "out": 0, "opt": 0, "done": 0}
	if len(got) != len(want) {
		t.Fatalf("builtin definitions = %v, want %v", got, want)
	}
	for name, attrs := range want {
		if a, ok := got[name]; !ok || a != attrs {
			t.Errorf("%s: attrs = %v, %t; want %v", name, a, ok, attrs)
		}
	}
}

func TestDefinitionsRecordTheirWriter(t *testing.T) {
	t.Parallel()

	src := "declare -i count=1\ndeclare +i count\nexec {log}>out.txt\nexec {log}>&-\nfor item in a b; do :; done\n(( total += 2 ))\n"
	tree := Parse(src, dialect.Bash)
	type summary struct {
		kind    DefinitionKind
		attrs   Attr
		cleared Attr
		writer  string
	}
	var got []summary
	for _, d := range tree.Definitions {
		got = append(got, summary{kind: d.Kind, attrs: d.Attrs, cleared: d.Cleared, writer: fmt.Sprintf("%T", d.Node)})
	}
	want := []summary{
		{DefDeclare, AttrInteger, 0, "*syntax.Assign"},
		{DefDeclare, 0, AttrInteger, "*syntax.Assign"},
		{DefDescriptor, 0, 0, "*syntax.Redirect"},
		{DefLoop, 0, 0, "*syntax.WordIter"},
		{DefArithmetic, 0, 0, "*syntax.BinaryArithm"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("definitions = %+v\nwant %+v", got, want)
	}
	if refs := tree.Visible("log", FileScope); refs.Len() != 1 {
		t.Errorf("closing a descriptor defined it again: %d definitions", refs.Len())
	}
}

func TestLine(t *testing.T) {
	t.Parallel()

	tree := Parse("a\nbb\n\nc", dialect.Bash)
	for offset, want := range map[int]int{0: 1, 1: 1, 2: 2, 4: 2, 5: 3, 6: 4, 7: 4} {
		if got := tree.Line(offset); got != want {
			t.Errorf("Line(%d) = %d, want %d", offset, got, want)
		}
	}
}
