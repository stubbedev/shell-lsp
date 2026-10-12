package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestDialectSourceDecidesShellFindings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		script string
		source DialectSource
		want   []string
	}{
		{"echo hi\n", DialectDetected, nil},
		{"echo hi\n", DialectGuessed, []string{"SC2148 1:1"}},
		{"echo hi\n", DialectForced, nil},
		{"# shellcheck shell=sh\necho hi\n", DialectGuessed, nil},
		{"#!/usr/bin/python\nprint(1)\n", DialectDetected, []string{"SC1071 1:1"}},
		{"#!/usr/bin/python\nprint(1)\n", DialectForced, nil},
		{"#!/bin/ash\necho hi\n", DialectDetected, []string{"SC2187 1:1"}},
		{"#!/bin/ash\necho hi\n", DialectForced, nil},
	} {
		tree := parser.Parse(tc.script, dialect.Posix)
		var got []string
		for _, f := range Check(tree, nil, Options{DialectSource: tc.source}) {
			got = append(got, f.Code.String()+" "+position(tc.script, f.Span.Start))
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%q from source %d: got %v, want %v", tc.script, tc.source, got, tc.want)
		}
	}
}

func TestShebangLines(t *testing.T) {
	t.Parallel()

	for script, want := range map[string][]string{
		"# Header\n# ! Note\necho hi\n":     {"SC1115 2:2", "SC1128 2:1"},
		"\n  #!/bin/sh\necho hi\n":          {"SC1114 2:1", "SC1128 2:1"},
		"\n! /bin/sh\necho hi\n":            {"SC1104 2:1", "SC1128 2:1"},
		"  #/bin/sh\necho hi\n":             nil,
		"#!/bin/sh\n\n#!/bin/sh\necho hi\n": nil,
		"# /etc/foo config\necho hi\n":      {"SC1113 1:2"},
		"#  !  /bin/sh\necho hi\n":          {"SC1115 1:2"},
		"!/bin/sh\necho hi\n":               {"SC1104 1:1"},
		"echo hi\n#!/bin/sh\n":              nil,
	} {
		tree := parser.Parse(script, dialect.Posix)
		var got []string
		for _, f := range Check(tree, nil, Options{Exclude: CodesOf(2239)}) {
			got = append(got, f.Code.String()+" "+position(script, f.Span.Start))
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("%q: got %v, want %v", script, got, want)
		}
	}
}
