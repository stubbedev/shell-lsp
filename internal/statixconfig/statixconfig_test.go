package statixconfig

import (
	"errors"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

func TestParseReadsWhatStatixReads(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, text string
		want       []lint.Code
	}{
		{"nothing", "", nil},
		{"one", `disabled = ["empty_let_in"]`, []lint.Code{lint.CodeNixEmptyBinding}},
		{"literal string", `disabled = ['repeated_keys']`, []lint.Code{lint.CodeNixRepeatedKeys}},
		{"comments and trailing comma", "# why\ndisabled = [\n  \"useless_parens\", # because\n  \"eta_reduction\",\n]\n", []lint.Code{lint.CodeNixUselessParens, lint.CodeNixEtaReduction}},
		{"an unknown lint is ignored", `disabled = ["nope", "bool_comparison"]`, []lint.Code{lint.CodeNixBoolComparison}},
		{"nix_version is accepted", "nix_version = \"2.4\"\ndisabled = [\"unquoted_uri\"]", []lint.Code{lint.CodeNixUnquotedURI}},
		{"nix_version of another type", "nix_version = 3", nil},
		{"unknown keys", "other = 1\nmore = true\nfloat = 1.5\ndate = 2024-01-01\ninline = { a = 1, b = [2] }\nmulti = \"\"\"x\ny\"\"\"", nil},
		{"a table is not the top level", "[a]\ndisabled = [\"empty_let_in\"]", nil},
		{"after a table", "disabled = [\"empty_inherit\"]\n[a.b]\nc = 1\n[[d]]\ne = 2\n", []lint.Code{lint.CodeNixEmptyInherit}},
		{"dotted keys", "a.b = 1\n\"c\".d = 2\n", nil},
		{"escapes", `disabled = ["empty_let_in"]`, []lint.Code{lint.CodeNixEmptyBinding}},
		{"CRLF", "disabled = [\"empty_let_in\"]\r\n", []lint.Code{lint.CodeNixEmptyBinding}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Parse("/w/statix.toml", tt.text)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(cfg.Disabled, tt.want) {
				t.Errorf("Disabled = %v, want %v", cfg.Disabled, tt.want)
			}
		})
	}
}

func TestParseRejectsWhatStatixRejects(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		`disabled = "x"`,
		`disabled = ["a", 1]`,
		`disabled = """x"""`,
		"not toml",
		"disabled = [\"a\"",
		"disabled = [\"a\" \"b\"]",
		"disabled = [\"a]\n",
		"a = 1 b = 2",
		"a = 1\na = 2",
		"[a\nb = 1",
		"a =",
		`a = "\q"`,
		`a = "\u12"`,
		"a = nope",
		"= 1",
	} {
		if _, err := Parse("/w/statix.toml", text); err == nil {
			t.Errorf("%q parsed", text)
		}
	}
}

func TestEveryStatixLintMapsToACheck(t *testing.T) {
	t.Parallel()

	want := map[string]lint.Code{
		"bool_comparison":        lint.CodeNixBoolComparison,
		"empty_let_in":           lint.CodeNixEmptyBinding,
		"manual_inherit":         lint.CodeNixSpelledInherit,
		"manual_inherit_from":    lint.CodeNixManualInheritFrom,
		"legacy_let_syntax":      lint.CodeNixLegacyLet,
		"collapsible_let_in":     lint.CodeNixCollapsibleLet,
		"eta_reduction":          lint.CodeNixEtaReduction,
		"useless_parens":         lint.CodeNixUselessParens,
		"empty_pattern":          lint.CodeNixEmptyPattern,
		"redundant_pattern_bind": lint.CodeNixRedundantPatternBind,
		"unquoted_uri":           lint.CodeNixUnquotedURI,
		"empty_inherit":          lint.CodeNixEmptyInherit,
		"deprecated_to_path":     lint.CodeNixDeprecatedToPath,
		"bool_simplification":    lint.CodeNixBoolSimplification,
		"useless_has_attr":       lint.CodeNixUselessHasAttr,
		"repeated_keys":          lint.CodeNixRepeatedKeys,
		"empty_list_concat":      lint.CodeNixEmptyListConcat,
	}
	got := map[string]lint.Code{}
	for _, l := range lint.StatixLints() {
		got[l.Name] = l.Code
		if _, known := lint.Describe(l.Code); !known {
			t.Errorf("%s maps to %s, which the catalog lacks", l.Name, l.Code)
		}
		cfg, err := Parse("/w/statix.toml", `disabled = ["`+l.Name+`"]`)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(cfg.Disabled, []lint.Code{l.Code}) {
			t.Errorf("disabling %s disables %v", l.Name, cfg.Disabled)
		}
	}
	if !maps.Equal(got, want) {
		t.Errorf("statix lints = %v, want %v", got, want)
	}
}

func TestIgnoreMatchesLikeStatix(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		pattern string
		ignored []string
		kept    []string
	}{
		{".direnv", []string{".direnv/d.nix"}, []string{"x.nix", "gen/g.nix"}},
		{"gen", []string{"gen/g.nix", "a/gen/h.nix"}, []string{"x.nix", "a/k.nix"}},
		{"a/gen", []string{"a/gen/h.nix"}, []string{"gen/g.nix"}},
		{"/gen", []string{"gen/g.nix"}, []string{"a/gen/h.nix"}},
		{"*.nix", []string{"x.nix", "a/k.nix"}, nil},
		{"k.nix", []string{"a/k.nix"}, []string{"x.nix"}},
		{"sub/*", []string{"sub/s.nix"}, []string{"x.nix"}},
		{"!sub/*", nil, []string{"sub/s.nix"}},
	} {
		cfg, err := Parse("/w/statix.toml", `ignore = ["`+tt.pattern+`"]`)
		if err != nil {
			t.Fatal(err)
		}
		for _, rel := range tt.ignored {
			if len(cfg.ExcludedFor(filepath.Join("/w", rel))) != len(lint.StatixLints()) {
				t.Errorf("%s does not ignore %s", tt.pattern, rel)
			}
		}
		for _, rel := range tt.kept {
			if len(cfg.ExcludedFor(filepath.Join("/w", rel))) != 0 {
				t.Errorf("%s ignores %s", tt.pattern, rel)
			}
		}
	}
}

func TestResolverFindsTheNearestAncestor(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"/w/statix.toml":     `disabled = ["empty_let_in"]`,
		"/w/sub/statix.toml": `disabled = ["unquoted_uri"]`,
		"/w/bad/statix.toml": "not toml",
	}
	reads := 0
	read := func(path string) ([]byte, error) {
		reads++
		text, ok := files[filepath.ToSlash(path)]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return []byte(text), nil
	}
	r := NewResolver(read, time.Now, time.Hour)
	for _, tt := range []struct {
		path string
		want []lint.Code
	}{
		{"/w/a.nix", []lint.Code{lint.CodeNixEmptyBinding}},
		{"/w/deep/er/a.nix", []lint.Code{lint.CodeNixEmptyBinding}},
		{"/w/sub/a.nix", []lint.Code{lint.CodeNixUnquotedURI}},
		{"/elsewhere/a.nix", nil},
	} {
		got, err := r.ExcludedFor(tt.path)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.path, got, tt.want)
		}
	}
	if _, err := r.ExcludedFor("/w/bad/a.nix"); err == nil {
		t.Error("a broken statix.toml gave no error")
	}
	before := reads
	if _, err := r.ExcludedFor("/w/a.nix"); err != nil {
		t.Fatal(err)
	}
	if reads != before {
		t.Errorf("a second lookup read the disk %d times", reads-before)
	}
	files["/w/statix.toml"] = `disabled = ["useless_parens"]`
	r.Invalidate(filepath.FromSlash("/w/statix.toml"))
	got, err := r.ExcludedFor("/w/a.nix")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []lint.Code{lint.CodeNixUselessParens}) {
		t.Errorf("after invalidating: %v", got)
	}
}

func TestReadFailureIsReported(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	r := NewResolver(func(string) ([]byte, error) { return nil, boom }, time.Now, time.Hour)
	if _, err := r.ExcludedFor("/w/a.nix"); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{"", `disabled = ["a"]`, "[a]\nb = 1\n", `a = """x"""`, "a.b = { c = [1, 2] }\n", `"a" = 'b'`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		cfg, err := Parse("/w/statix.toml", text)
		if err != nil && cfg.Disabled != nil {
			t.Errorf("%q failed but disabled %v", text, cfg.Disabled)
		}
	})
}
