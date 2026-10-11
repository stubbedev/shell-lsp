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
		if _, ok := Term(tc.v, tc.name); !ok {
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
