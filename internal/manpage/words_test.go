package manpage

import "testing"

func TestValidName(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"ls":         true,
		"git-commit": true,
		"_x":         true,
		"g++":        true,
		"python3.12": true,
		"a_b":        true,
		"":           false,
		"-ls":        false,
		".hidden":    false,
		"a/b":        false,
		"a b":        false,
		"é":          false,
	} {
		if got := validName(name); got != want {
			t.Errorf("validName(%q) = %t, want %t", name, got, want)
		}
	}
}

func TestIsBareWord(t *testing.T) {
	t.Parallel()

	for word, want := range map[string]bool{
		"add":      true,
		"ls-files": true,
		"x2":       true,
		"a_b":      true,
		"a":        false,
		"":         false,
		"Add":      false,
		"2x":       false,
		"add-":     false,
		"add_":     false,
		"a.b":      false,
		"-add":     false,
	} {
		if got := isBareWord(word); got != want {
			t.Errorf("isBareWord(%q) = %t, want %t", word, got, want)
		}
	}
}

func TestSplitReference(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		word, name string
		ok         bool
	}{
		{"git-add(1)", "git-add", true},
		{"printf(3p)", "printf", true},
		{"x(8SSL)", "x", true},
		{"(1)", "(1)", false},
		{"ls(1", "ls(1", false},
		{"ls()", "ls()", false},
		{"ls(x)", "ls(x)", false},
		{"ls(1x2)", "ls(1x2)", false},
		{"ls(1))", "ls(1))", false},
		{"a)b(1)", "a)b(1)", false},
		{"ls(1) ", "ls(1) ", false},
		{"ls", "ls", false},
	} {
		if name, ok := splitReference(tt.word); name != tt.name || ok != tt.ok {
			t.Errorf("splitReference(%q) = %q, %t, want %q, %t", tt.word, name, ok, tt.name, tt.ok)
		}
	}
}

func TestOptionName(t *testing.T) {
	t.Parallel()

	for part, want := range map[string]string{
		"-v":              "-v",
		"--verbose":       "--verbose",
		"--color=WHEN":    "--color",
		"-o FILE":         "-o",
		"-?":              "-?",
		"-#":              "-#",
		"-@":              "-@",
		"--dry-run[=x]":   "--dry-run",
		"--no_cache":      "--no_cache",
		"-":               "",
		"--":              "",
		"---x":            "",
		"-_":              "",
		"--=":             "",
		"v":               "",
		"":                "",
		"--a?b":           "--a",
		"-1":              "-1",
		"--x--y extra -z": "--x--y",
	} {
		if got := optionName(part); got != want {
			t.Errorf("optionName(%q) = %q, want %q", part, got, want)
		}
	}
}
