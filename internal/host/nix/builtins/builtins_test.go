package builtins

import (
	"encoding/json"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const (
	nixBinary         = "nix"
	nixInstantiate    = "nix-instantiate"
	unsafeNativeFlag  = "allow-unsafe-native-code-during-evaluation"
	undefinedVariable = "undefined variable"
)

func TestTableIsSortedUniqueAndDocumented(t *testing.T) {
	t.Parallel()

	all := All()
	if len(all) == 0 {
		t.Fatal("the table is empty")
	}
	for i, b := range all {
		if i > 0 && all[i-1].Name >= b.Name {
			t.Errorf("%q does not sort strictly after %q", b.Name, all[i-1].Name)
		}
		if strings.TrimSpace(b.Doc) == "" {
			t.Errorf("%q has no doc", b.Name)
		}
		got, ok := Lookup(b.Name)
		if !ok || got.Name != b.Name {
			t.Errorf("Lookup(%q) = %+v, %v", b.Name, got, ok)
		}
	}
}

func TestMembersAreUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, len(members))
	for _, m := range members {
		if seen[m.name] {
			t.Errorf("member %q is listed twice", m.name)
		}
		seen[m.name] = true
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		global bool
		params []string
	}{
		{name: "true", global: true},
		{name: "false", global: true},
		{name: "null", global: true},
		{name: "builtins", global: true},
		{name: "import", global: true, params: []string{"path"}},
		{name: "map", global: true, params: []string{"f", "list"}},
		{name: "toString", global: true, params: []string{"e"}},
		{name: "abort", global: true, params: []string{"s"}},
		{name: "throw", global: true, params: []string{"s"}},
		{name: "derivation", global: true, params: []string{"attrs"}},
		{name: "attrNames", params: []string{"set"}},
		{name: "__attrNames", global: true, params: []string{"set"}},
		{name: "foldl'", params: []string{"op", "nul", "list"}},
		{name: "__foldl'", global: true, params: []string{"op", "nul", "list"}},
		{name: "__curPos", global: true},
	} {
		got, ok := Lookup(tc.name)
		if !ok {
			t.Errorf("Lookup(%q) found nothing", tc.name)
			continue
		}
		if got.Global != tc.global {
			t.Errorf("Lookup(%q).Global = %v, want %v", tc.name, got.Global, tc.global)
		}
		if !slices.Equal(got.Params, tc.params) {
			t.Errorf("Lookup(%q).Params = %v, want %v", tc.name, got.Params, tc.params)
		}
	}
	for _, name := range []string{"", "lib", "__builtins", "__map", "__overrides", "let"} {
		if got, ok := Lookup(name); ok {
			t.Errorf("Lookup(%q) = %+v, want nothing", name, got)
		}
	}
}

func TestAllReturnsACopy(t *testing.T) {
	t.Parallel()

	first := All()
	want := first[0]
	first[0] = Builtin{Name: "overwritten"}
	if got := All()[0]; got.Name != want.Name {
		t.Errorf("All()[0] = %q after a caller's write, want %q", got.Name, want.Name)
	}
}

func TestGateNotesReachTheDoc(t *testing.T) {
	t.Parallel()

	for _, m := range members {
		if m.gate.note == "" {
			continue
		}
		got, ok := Lookup(m.name)
		if !ok || !strings.Contains(got.Doc, m.gate.note) {
			t.Errorf("Lookup(%q).Doc = %q, want it to mention %q", m.name, got.Doc, m.gate.note)
		}
	}
}

func TestKeywords(t *testing.T) {
	t.Parallel()

	want := []string{"assert", "else", "if", "in", "inherit", "let", "or", "rec", "then", "with"}
	got := Keywords()
	names := make([]string, 0, len(got))
	for _, k := range got {
		names = append(names, k.Name)
		if strings.TrimSpace(k.Doc) == "" {
			t.Errorf("keyword %q has no doc", k.Name)
		}
	}
	if !slices.Equal(names, want) {
		t.Errorf("Keywords() names = %v, want %v", names, want)
	}
}

func gatedFlags() []string {
	features := []string{"nix-command"}
	for _, m := range members {
		if m.gate.feature != "" {
			features = append(features, m.gate.feature)
		}
	}
	slices.Sort(features)
	return []string{"--extra-experimental-features", strings.Join(slices.Compact(features), " "), "--option", unsafeNativeFlag, "true"}
}

func requireNix(t *testing.T) {
	t.Helper()
	for _, binary := range []string{nixBinary, nixInstantiate} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skipf("comparing with nix needs %s on PATH: %v", binary, err)
		}
	}
}

func runNix(t *testing.T, binary string, args ...string) ([]byte, string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), binary, args...) //nolint:gosec // G204: the arguments reach nix as argv and come from this test
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	return out, stderr.String(), err
}

func installedNames(t *testing.T) []string {
	t.Helper()
	out, stderr, err := runNix(t, nixBinary, append(gatedFlags(), "eval", "--impure", "--json", "--expr", "builtins.attrNames builtins")...)
	if err != nil {
		t.Skipf("%s cannot evaluate here: %v: %s", nixBinary, err, stderr)
	}
	var names []string
	if err := json.Unmarshal(out, &names); err != nil {
		t.Fatalf("decoding the names %s reported: %v", nixBinary, err)
	}
	return names
}

type dumpedBuiltin struct {
	Args []string `json:"args"`
}

func dumpedLanguage(t *testing.T) map[string]dumpedBuiltin {
	t.Helper()
	out, stderr, err := runNix(t, nixBinary, "__dump-language")
	if err != nil {
		t.Logf("%s has no usable __dump-language: %v: %s", nixBinary, err, stderr)
		return nil
	}
	var dumped map[string]dumpedBuiltin
	if err := json.Unmarshal(out, &dumped); err != nil {
		t.Fatalf("decoding %s __dump-language: %v", nixBinary, err)
	}
	return dumped
}

func TestTableCoversInstalledNix(t *testing.T) {
	t.Parallel()
	requireNix(t)

	names := installedNames(t)
	dumped := dumpedLanguage(t)
	for name := range dumped {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range slices.Compact(names) {
		got, ok := Lookup(name)
		if !ok {
			t.Errorf("installed nix has builtins.%s, the table derived from Nix %s lacks it", name, tableNixVersion)
			continue
		}
		if d, inDump := dumped[name]; inDump && len(got.Params) != len(d.Args) {
			t.Errorf("Lookup(%q).Params = %v, installed nix reports %v", name, got.Params, d.Args)
		}
	}
}

func TestGlobalMatchesInstalledNix(t *testing.T) {
	t.Parallel()
	requireNix(t)

	installed := make(map[string]bool)
	for _, name := range installedNames(t) {
		installed[name] = true
	}

	reachable := []string{parserGlobal.Name}
	var prefixOnly []string
	for _, m := range members {
		if !installed[m.name] {
			continue
		}
		if m.global {
			reachable = append(reachable, m.name)
			prefixOnly = append(prefixOnly, hiddenAliasPrefix+m.name)
			continue
		}
		reachable = append(reachable, hiddenAliasPrefix+m.name)
		prefixOnly = append(prefixOnly, m.name)
	}

	if _, stderr, err := runNix(t, nixInstantiate, append(gatedFlags(), "--parse", "-E", "[ "+strings.Join(reachable, " ")+" ]")...); err != nil {
		t.Errorf("a name the table calls global is not defined at the top level: %v: %s", err, stderr)
	}
	for _, name := range prefixOnly {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, stderr, err := runNix(t, nixInstantiate, append(gatedFlags(), "--parse", "-E", name)...)
			if err == nil {
				t.Errorf("%q is defined at the top level, the table says it is not", name)
				return
			}
			if !strings.Contains(stderr, undefinedVariable) {
				t.Errorf("parsing %q failed for another reason: %v: %s", name, err, stderr)
			}
		})
	}
}
