package gitignore

import "testing"

func TestIgnoredFollowsGitSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		rules string
		rel   string
		isDir bool
		want  bool
	}{
		{"target", "target", true, true},
		{"target", "a/b/target", true, true},
		{"target", "a/target", false, true},
		{"target/", "a/target", false, false},
		{"target/", "a/target", true, true},
		{"/target", "target", true, true},
		{"/target", "a/target", true, false},
		{".devenv*", ".devenv", true, true},
		{".devenv*", ".devenv.flake.nix", false, true},
		{".devenv*", "x.devenv", false, false},
		{"*.sh", "a/b.sh", false, true},
		{"*.sh", "a.sh/b", false, false},
		{"a/*.sh", "a/b.sh", false, true},
		{"a/*.sh", "x/a/b.sh", false, false},
		{"a/*.sh", "a/c/b.sh", false, false},
		{"**/target/**", "target", true, false},
		{"**/target/**", "target/x.sh", false, true},
		{"**/target/**", "a/b/target/x.sh", false, true},
		{"**/build", "build", true, true},
		{"**/build", "x/y/build", true, true},
		{"a/**/b", "a/b", true, true},
		{"a/**/b", "a/x/y/b", true, true},
		{"a/**/b", "x/a/b", true, false},
		{"gen-?.sh", "gen-1.sh", false, true},
		{"gen-?.sh", "gen-12.sh", false, false},
		{"gen-[0-9].sh", "gen-7.sh", false, true},
		{"gen-[!0-9].sh", "gen-7.sh", false, false},
		{"gen-[!0-9].sh", "gen-x.sh", false, true},
		{"*.sh\n!keep.sh", "keep.sh", false, false},
		{"*.sh\n!keep.sh", "drop.sh", false, true},
		{"!keep.sh\n*.sh", "keep.sh", false, true},
		{`\!bang.sh`, "!bang.sh", false, true},
		{`\#hash.sh`, "#hash.sh", false, true},
		{"#hash.sh", "#hash.sh", false, false},
		{"space.sh   ", "space.sh", false, true},
		{`space.sh\ `, "space.sh ", false, true},
		{"crlf.sh\r\n", "crlf.sh", false, true},
		{"broken[.sh\nok.sh", "broken[.sh", false, false},
		{"broken[.sh\nok.sh", "ok.sh", false, true},
		{"\n\n/\n!\n", "x.sh", false, false},
	}
	for _, tt := range tests {
		if got := new(Stack).Push("", Parse(tt.rules)).Ignored(tt.rel, tt.isDir); got != tt.want {
			t.Errorf("rules %q: Ignored(%q, dir=%t) = %t, want %t", tt.rules, tt.rel, tt.isDir, got, tt.want)
		}
	}
}

func TestDeeperRulesOverrideShallowerOnes(t *testing.T) {
	t.Parallel()

	top := (*Stack)(nil).Push("", Parse("*.sh\n"))
	sub := top.Push("lib", Parse("!keep.sh\n/local.sh\n"))
	tests := []struct {
		stack *Stack
		rel   string
		want  bool
	}{
		{top, "lib/keep.sh", true},
		{sub, "lib/keep.sh", false},
		{sub, "lib/other.sh", true},
		{sub, "lib/local.sh", true},
		{sub, "lib/x/local.sh", true},
		{sub, "lib/x/keep.sh", false},
	}
	for _, tt := range tests {
		if got := tt.stack.Ignored(tt.rel, false); got != tt.want {
			t.Errorf("Ignored(%q) = %t, want %t", tt.rel, got, tt.want)
		}
	}
}

func TestEmptyStackIgnoresNothing(t *testing.T) {
	t.Parallel()

	var empty *Stack
	if empty.Push("", Parse("# only a comment\n")) != nil {
		t.Error("pushing no rules allocated a layer")
	}
	if empty.Ignored("a.sh", false) {
		t.Error("an empty stack ignored a.sh")
	}
}
