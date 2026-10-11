// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
)

func TestParseNestingLimit(t *testing.T) {
	t.Parallel()
	nest := func(open, inner, close string, n int) string {
		return strings.Repeat(open, n) + inner + strings.Repeat(close, n)
	}
	tests := []struct {
		name  string
		lang  LangVariant
		shape func(n int) string
	}{
		{"CmdSubst", LangBash, func(n int) string { return "echo " + nest("$(", "x", ")", n) }},
		{"QuotedCmdSubst", LangBash, func(n int) string { return "echo " + nest(`"$(`, "x", `)"`, n) }},
		{"ParamExp", LangBash, func(n int) string { return "echo " + nest("${a:-", "x", "}", n) }},
		{"ParamExpIndex", LangBash, func(n int) string { return "echo " + nest("${a[", "1", "]}", n) }},
		{"ZshParamExp", LangZsh, func(n int) string { return "echo " + nest("${", "a", "}", n) }},
		{"Block", LangBash, func(n int) string { return nest("{ ", "true", "; }", n) }},
		{"Subshell", LangBash, func(n int) string { return nest("(", "true", ")", n) }},
		{"If", LangBash, func(n int) string { return nest("if true; then ", "true", "; fi", n) }},
		{"Elif", LangBash, func(n int) string { return "if a; then b" + strings.Repeat("; elif a; then b", n) + "; fi" }},
		{"FuncBody", LangBash, func(n int) string { return strings.Repeat("f() ", n) + "{ x; }" }},
		{"AndList", LangBash, func(n int) string { return strings.Repeat("a && ", n) + "b" }},
		{"Pipeline", LangBash, func(n int) string { return strings.Repeat("a | ", n) + "b" }},
		{"ArithmParens", LangBash, func(n int) string { return "echo $((" + nest("(", "1", ")", n) + "))" }},
		{"ArithmUnary", LangBash, func(n int) string { return "echo $((" + strings.Repeat("- ", n) + "1))" }},
		{"ArithmBinary", LangBash, func(n int) string { return "echo $((" + strings.Repeat("1+", n) + "1))" }},
		{"ArithmAssign", LangBash, func(n int) string { return "(( " + strings.Repeat("a=", n) + "1 ))" }},
		{"ArithmTernary", LangBash, func(n int) string {
			return "(( " + strings.Repeat("a?", n) + "1" + strings.Repeat(":2", n) + " ))"
		}},
		{"TestParens", LangBash, func(n int) string { return "[[ " + nest("( ", "a", " )", n) + " ]]" }},
		{"TestNot", LangBash, func(n int) string { return "[[ " + strings.Repeat("! ", n) + "a ]]" }},
		{"TestAnd", LangBash, func(n int) string { return "[[ " + strings.Repeat("a && ", n) + "a ]]" }},
		{"KshNestedArray", LangKsh93, func(n int) string { return "a=(" + nest("[0]=(", "x", ")", n) + ")" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := NewParser(Variant(tc.lang))
			_, err := p.Parse(strings.NewReader(tc.shape(maxNesting/10)), "")
			qt.Assert(t, qt.IsNil(err))
			f, err := p.Parse(strings.NewReader(tc.shape(100_000)), "")
			qt.Assert(t, qt.ErrorMatches(err, fmt.Sprintf(`.*syntax nests more than %d levels deep`, maxNesting)))
			Walk(f, func(Node) bool { return true })
		})
	}
}
