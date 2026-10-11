package actions

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestToggleFunctionKeyword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		d                dialect.Dialect
		src, want, title string
	}{
		{dialect.Bash, "function «g»reet {\n\techo hi\n}\n", "greet() {\n\techo hi\n}\n", "Declare greet() without the function keyword"},
		{dialect.Bash, "«function» greet() {\n\techo hi\n}\n", "greet() {\n\techo hi\n}\n", "Declare greet() without the function keyword"},
		{dialect.Bash, "function  greet«»  {\n\techo hi\n}\n", "greet()  {\n\techo hi\n}\n", "Declare greet() without the function keyword"},
		{dialect.Posix, "function «greet» {\n\techo hi\n}\n", "greet() {\n\techo hi\n}\n", "Declare greet() without the function keyword"},
		{dialect.Bash, "«greet»() {\n\techo hi\n}\n", "function greet {\n\techo hi\n}\n", "Declare greet with the function keyword"},
		{dialect.Bash, "«greet» ( ) {\n\techo hi\n}\n", "function greet {\n\techo hi\n}\n", "Declare greet with the function keyword"},
		{dialect.Zsh, "«greet»() {\n\techo hi\n}\n", "function greet {\n\techo hi\n}\n", "Declare greet with the function keyword"},
		{dialect.Zsh, "function «greet» {\n\techo hi\n}\n", "greet() {\n\techo hi\n}\n", "Declare greet() without the function keyword"},
		{dialect.Bash, "greet«(»){\n\techo hi\n}\n", "function greet {\n\techo hi\n}\n", "Declare greet with the function keyword"},
		{dialect.Bash, "if true; then\n\t«f»() { :; }\nfi\n", "if true; then\n\tfunction f { :; }\nfi\n", "Declare f with the function keyword"},
	}
	for _, tc := range tests {
		got, title, ok := refactorIn(t, tc.d, FunctionKeyword, tc.src)
		if !ok || got != tc.want || title != tc.title {
			t.Errorf("%s: toggle %q = %q (%q, %t), want %q (%q)", tc.d, tc.src, got, title, ok, tc.want, tc.title)
		}
	}
	for _, tc := range []struct {
		d   dialect.Dialect
		src string
	}{
		{dialect.Posix, "«greet»() {\n\techo hi\n}\n"},
		{dialect.Dash, "«greet»() {\n\techo hi\n}\n"},
		{dialect.Busybox, "«greet»() {\n\techo hi\n}\n"},
		{dialect.Ksh, "«greet»() {\n\techo hi\n}\n"},
		{dialect.Ksh, "function «greet» {\n\techo hi\n}\n"},
		{dialect.Bash, "greet() {\n\t«echo» hi\n}\n"},
		{dialect.Bash, "«greet»() ( echo hi )\n"},
		{dialect.Zsh, "function «a» b {\n\techo hi\n}\n"},
		{dialect.Bash, "«echo» hi\n"},
	} {
		if got, _, ok := refactorIn(t, tc.d, FunctionKeyword, tc.src); ok {
			t.Errorf("%s: toggle %q offered: %q", tc.d, tc.src, got)
		}
	}
}
