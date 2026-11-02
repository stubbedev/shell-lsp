package nixeval

import (
	"strings"
	"testing"
)

func TestNixStringEscapesEverythingThatCouldEndTheLiteral(t *testing.T) {
	t.Parallel()

	tests := []struct{ in, want string }{
		{"plain", `"plain"`},
		{`"; throw "x`, `"\"; throw \"x"`},
		{`${throw "x"}`, `"\${throw \"x\"}"`},
		{`\${throw "x"}`, `"\\\${throw \"x\"}"`},
		{`$${x}`, `"\$\${x}"`},
		{`back\`, `"back\\"`},
		{"line\nbreak", "\"line\nbreak\""},
	}
	for _, tt := range tests {
		if got := nixString(tt.in); got != tt.want {
			t.Errorf("nixString(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestCallExprPassesArgumentsOnlyAsEscapedLiterals(t *testing.T) {
	t.Parallel()

	hostile := `"; x = ${throw "INJECTED"}; y = "`
	got := callExpr("attrs", stringArg("ref", hostile), listArg("path", []string{hostile}))
	if strings.Contains(got, hostile) {
		t.Errorf("callExpr() embeds the raw value: %s", got[len(evalSource):])
	}
	want := `.attrs { ref = ` + nixString(hostile) + `; path = [ ` + nixString(hostile) + ` ]; }`
	if !strings.HasSuffix(got, want) {
		t.Errorf("callExpr() ends in %s, want %s", got[len(evalSource):], want)
	}
}

func TestLockedRefEncodesTheHash(t *testing.T) {
	t.Parallel()

	tests := []struct{ dir, want string }{
		{"", "path:/nix/store/a-source?narHash=sha256-a%2Bb%2Fc%3D"},
		{"sub dir", "path:/nix/store/a-source?dir=sub+dir&narHash=sha256-a%2Bb%2Fc%3D"},
	}
	for _, tt := range tests {
		if got := lockedRef("/nix/store/a-source", "sha256-a+b/c=", tt.dir); got != tt.want {
			t.Errorf("lockedRef(dir %q) = %s, want %s", tt.dir, got, tt.want)
		}
	}
}
