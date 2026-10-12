package shelldocs

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestEntriesAreComplete(t *testing.T) {
	t.Parallel()

	for _, list := range [][]Entry{builtins, keywords, variables, parameters} {
		for _, e := range list {
			if e.Name == "" || e.Doc == "" || e.Dialects == 0 {
				t.Errorf("incomplete entry %+v", e)
			}
			for d := range e.Notes {
				if !e.Dialects.Has(d) {
					t.Errorf("%s has a %s note but is not available there", e.Name, d)
				}
			}
		}
	}
}

func TestDialectAvailability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		lookup    func(string) (Entry, bool)
		available []dialect.Dialect
		missing   []dialect.Dialect
	}{
		{name: "declare", lookup: Builtin, available: []dialect.Dialect{dialect.Bash, dialect.Zsh}, missing: []dialect.Dialect{dialect.Posix, dialect.Dash, dialect.Ksh}},
		{name: "local", lookup: Builtin, available: []dialect.Dialect{dialect.Bash, dialect.Dash, dialect.Zsh}, missing: []dialect.Dialect{dialect.Posix}},
		{name: "print", lookup: Builtin, available: []dialect.Dialect{dialect.Zsh, dialect.Ksh}, missing: []dialect.Dialect{dialect.Bash}},
		{name: "[[", lookup: Keyword, available: []dialect.Dialect{dialect.Bash, dialect.Zsh, dialect.Ksh}, missing: []dialect.Dialect{dialect.Posix, dialect.Dash}},
		{name: "BASH_SOURCE", lookup: Variable, available: []dialect.Dialect{dialect.Bash}, missing: []dialect.Dialect{dialect.Zsh}},
	}
	for _, tc := range tests {
		e, ok := tc.lookup(tc.name)
		if !ok {
			t.Fatalf("%s not documented", tc.name)
		}
		for _, d := range tc.available {
			if !e.Dialects.Has(d) {
				t.Errorf("%s should be available in %s", tc.name, d)
			}
		}
		for _, d := range tc.missing {
			if e.Dialects.Has(d) {
				t.Errorf("%s should not be available in %s", tc.name, d)
			}
		}
	}
}

func TestMarkdownCarriesNotesAndAvailability(t *testing.T) {
	t.Parallel()

	echo, _ := Builtin("echo")
	if md := echo.Markdown(dialect.Zsh); !strings.Contains(md, "**zsh:**") || !strings.Contains(md, "```zsh\necho") {
		t.Errorf("zsh echo markdown = %q", md)
	}
	declare, _ := Builtin("declare")
	if md := declare.Markdown(dialect.Posix); !strings.Contains(md, "_Not available in sh._") {
		t.Errorf("posix declare markdown = %q", md)
	}
}

func TestVocabularies(t *testing.T) {
	t.Parallel()

	for v, entries := range vocabularies {
		if len(entries) == 0 {
			t.Errorf("vocabulary %d is empty", v)
		}
		for _, e := range entries {
			if e.Name == "" || e.Doc == "" || e.Dialects == 0 {
				t.Errorf("vocabulary %d has an incomplete entry %+v", v, e)
			}
		}
	}
	for _, tc := range []struct {
		v    Vocabulary
		name string
	}{
		{ExpansionOperators, ":-"},
		{ExpansionOperators, "##"},
		{ExpansionOperators, "//"},
		{ExpansionOperators, ":#"},
		{ExpansionForms, "length"},
		{ZshParameterFlags, "j"},
		{ZshModifiers, "t"},
		{TestUnaryOperators, "-f"},
		{TestBinaryOperators, "-nt"},
		{CondOperators, "=~"},
		{Signals, "TERM"},
		{TrapConditions, "EXIT"},
	} {
		if _, ok := Term(tc.v, tc.name, dialect.Bash); !ok {
			t.Errorf("%q missing from vocabulary %d", tc.name, tc.v)
		}
	}
}

func TestTermsFollowTheDialect(t *testing.T) {
	t.Parallel()

	has := func(v Vocabulary, d dialect.Dialect, name string) bool {
		for e := range Terms(v, d) {
			if e.Name == name {
				return true
			}
		}
		return false
	}
	if has(ExpansionOperators, dialect.Dash, "//") || !has(ExpansionOperators, dialect.Bash, "//") {
		t.Error("pattern replacement should be offered in bash and not in dash")
	}
	if has(TrapConditions, dialect.Posix, "ERR") || !has(TrapConditions, dialect.Bash, "ERR") {
		t.Error("ERR should be a bash trap condition and not a POSIX one")
	}
	if has(CondOperators, dialect.Dash, "=~") {
		t.Error("=~ offered in dash")
	}
}

func TestBuiltinFlags(t *testing.T) {
	t.Parallel()

	for name, flags := range builtinFlags {
		b, ok := Builtin(name)
		if !ok {
			t.Errorf("flags documented for %q, which is not a builtin", name)
			continue
		}
		seen := map[string]dialect.Set{}
		for _, f := range flags {
			if !strings.HasPrefix(f.Name, "-") || f.Doc == "" || seen[f.Name]&f.Dialects != 0 {
				t.Errorf("%s: bad flag, or one described twice for a shell: %+v", name, f)
			}
			seen[f.Name] |= f.Dialects
			if f.Dialects&b.Dialects == 0 {
				t.Errorf("%s %s is documented for shells without %s", name, f.Name, name)
			}
		}
	}
	if _, ok := BuiltinFlag("read", "-a", dialect.Zsh); ok {
		t.Error("read -a offered in zsh, which spells it -A")
	}
	if _, ok := BuiltinFlag("read", "-r", dialect.Dash); !ok {
		t.Error("read -r missing in dash")
	}
	if _, ok := BuiltinFlags("declare", dialect.Posix); ok {
		t.Error("declare flags offered in POSIX sh, which has no declare")
	}
}

func TestSetLettersFollowTheShell(t *testing.T) {
	t.Parallel()

	type key struct {
		letter byte
		d      dialect.Dialect
	}
	for k, want := range map[key]string{
		{'e', dialect.Zsh}: "errexit", {'u', dialect.Dash}: "nounset", {'f', dialect.Bash}: "noglob", {'F', dialect.Zsh}: "noglob",
		{'E', dialect.Bash}: "errtrace", {'E', dialect.Busybox}: "errtrace", {'E', dialect.Dash}: "emacs", {'I', dialect.Dash}: "ignoreeof",
		{'k', dialect.Ksh}: "keyword", {'B', dialect.Ksh}: "braceexpand", {'h', dialect.Bash}: "hashall", {'h', dialect.Ksh}: "trackall",
	} {
		if got, ok := SetLetter(k.letter, k.d); !ok || got != want {
			t.Errorf("SetLetter(%c, %s) = %q, %t; want %q", k.letter, k.d, got, ok, want)
		}
	}
	for _, k := range []key{{'f', dialect.Zsh}, {'h', dialect.Zsh}, {'k', dialect.Zsh}, {'b', dialect.Zsh}, {'F', dialect.Bash}, {'E', dialect.Posix}, {'h', dialect.Posix}} {
		if got, ok := SetLetter(k.letter, k.d); ok {
			t.Errorf("SetLetter(%c, %s) = %q, want no option of that letter", k.letter, k.d, got)
		}
	}
}

func TestPosixSetTakesHWithoutALongName(t *testing.T) {
	t.Parallel()

	for d, want := range map[dialect.Dialect]bool{dialect.Posix: true, dialect.Bash: true, dialect.Ksh: true, dialect.Dash: false, dialect.Busybox: false} {
		if _, got := BuiltinFlag("set", "-h", d); got != want {
			t.Errorf("set -h documented for %s = %t, want %t", d, got, want)
		}
	}
}
