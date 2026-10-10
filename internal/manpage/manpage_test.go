package manpage

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

const lsPage = "LS(1)                            User Commands                            LS(1)\n\n" +
	"N\bNA\bAM\bME\bE\n     ls - list directory contents\n\n" +
	"SYNOPSIS\n     \x1b[1mls\x1b[0m [OPTION]... [FILE]...\n\n" +
	"DESCRIPTION\n     List information about the FILEs (the current directory by default).  Sort\n" +
	"     entries alphabetically if none of -cftuvSUX nor --sort is spec\u2010\n     ified.\n\n" +
	"     Mandatory arguments to long options are mandatory for short options too.\n\n" +
	"     -a, --all\n            do not ignore entries starting with .\n\n" +
	"     -A, --almost-all\n            do not list implied . and ..\n\n" +
	"AUTHOR\n     Written by someone.\n"

func TestParse(t *testing.T) {
	t.Parallel()

	page, err := Parse([]byte(lsPage))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if page.Summary != "ls - list directory contents" {
		t.Errorf("Summary = %q", page.Summary)
	}
	if strings.Join(page.Synopsis, "\n") != "ls [OPTION]... [FILE]..." {
		t.Errorf("Synopsis = %q", page.Synopsis)
	}
	if len(page.Description) != maxDescription {
		t.Fatalf("Description = %q", page.Description)
	}
	if !strings.HasSuffix(page.Description[0], "nor --sort is specified.") || !strings.HasPrefix(page.Description[0], "List information about the FILEs (the current directory by default). Sort entries") {
		t.Errorf("first paragraph = %q", page.Description[0])
	}
	md := page.Markdown()
	for _, want := range []string{"**ls - list directory contents**", "```\nls [OPTION]... [FILE]...\n```", "Mandatory arguments"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown() missing %q:\n%s", want, md)
		}
	}
}

func TestParseRejectsNonPages(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]byte("No manual entry for foo\n")); !errors.Is(err, ErrNoPage) {
		t.Errorf("Parse(garbage) error = %v, want ErrNoPage", err)
	}
}

func TestLookupCachesAndValidates(t *testing.T) {
	t.Parallel()

	calls := 0
	l := NewLookup()
	l.run = func(_ context.Context, name string) ([]byte, error) {
		calls++
		if name == "missing" {
			return nil, ErrNoPage
		}
		return []byte(lsPage), nil
	}
	for range 2 {
		if _, err := l.Page(t.Context(), "ls"); err != nil {
			t.Fatalf("Page(ls) error = %v", err)
		}
		if _, err := l.Page(t.Context(), "missing"); !errors.Is(err, ErrNoPage) {
			t.Fatalf("Page(missing) error = %v", err)
		}
	}
	if calls != 2 {
		t.Errorf("man ran %d times, want 2 (results cached)", calls)
	}
	for _, name := range []string{"-rf", "../etc/passwd", "a b", ""} {
		if _, err := l.Page(t.Context(), name); !errors.Is(err, ErrNoPage) {
			t.Errorf("Page(%q) error = %v, want rejection", name, err)
		}
	}
	if calls != 2 {
		t.Errorf("invalid names reached man")
	}
}

func TestRealManPage(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("man"); err != nil {
		t.Skip("man not installed")
	}
	page, err := NewLookup().Page(t.Context(), "ls")
	if errors.Is(err, ErrNoPage) {
		t.Skip("no ls manual page installed")
	}
	if err != nil {
		t.Fatalf("Page(ls) error = %v", err)
	}
	if !strings.Contains(page.Summary, "ls") {
		t.Errorf("Summary = %q", page.Summary)
	}
}

const grepPage = "NAME\n     grep - print lines\n\nOPTIONS\n   Generic Program Information\n     --help\n            Output a usage message and exit.\n\n" +
	"     -E, --extended-regexp\n            Interpret PATTERNS as extended regular expressions (EREs, see be‐\n            low).\n\n" +
	"     -e PATTERNS, --regexp=PATTERNS\n            Use PATTERNS as the patterns.\n\n" +
	"     -n     Number lines.\n\n" +
	"     -E, --extended-regexp\n            duplicate entry from another section\n\n" +
	"     - this is prose, not an option\n"

func TestOptions(t *testing.T) {
	t.Parallel()

	page, err := Parse([]byte(grepPage))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got := page.Options()
	want := []Option{
		{Names: []string{"--help"}, Doc: "Output a usage message and exit."},
		{Names: []string{"-E", "--extended-regexp"}, Doc: "Interpret PATTERNS as extended regular expressions (EREs, see below)."},
		{Names: []string{"-e", "--regexp"}, Doc: "Use PATTERNS as the patterns."},
		{Names: []string{"-n"}, Doc: "Number lines."},
	}
	if len(got) != len(want) {
		t.Fatalf("Options() = %+v", got)
	}
	for i := range want {
		if strings.Join(got[i].Names, " ") != strings.Join(want[i].Names, " ") || got[i].Doc != want[i].Doc {
			t.Errorf("option %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
