// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func parseWord(t *testing.T, src string) *syntax.Word {
	t.Helper()
	p := syntax.NewParser()
	word, err := p.Document(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	return word
}

func TestConfigNils(t *testing.T) {
	os.Setenv("EXPAND_GLOBAL", "value")
	tests := []struct {
		name string
		cfg  *Config
		src  string
		want string
	}{
		{
			"NilConfig",
			nil,
			"$EXPAND_GLOBAL",
			"",
		},
		{
			"ZeroConfig",
			&Config{},
			"$EXPAND_GLOBAL",
			"",
		},
		{
			"EnvConfig",
			&Config{Env: ListEnviron(os.Environ()...)},
			"$EXPAND_GLOBAL",
			"value",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			word := parseWord(t, tc.src)
			got, err := Literal(tc.cfg, word)
			if err != nil {
				t.Fatalf("did not want error, got %v", err)
			}
			if got != tc.want {
				t.Fatalf("wanted %q, got %q", tc.want, got)
			}
		})
	}
}

func TestFieldsIdempotency(t *testing.T) {
	tests := []struct {
		src  string
		want []string
	}{
		{
			"{1..4}",
			[]string{"1", "2", "3", "4"},
		},
		{
			"a{1..4}",
			[]string{"a1", "a2", "a3", "a4"},
		},
	}
	for _, tc := range tests {
		word := parseWord(t, tc.src)
		for range 2 {
			got, err := Fields(nil, word)
			if err != nil {
				t.Fatalf("did not want error, got %v", err)
			}
			qt.Assert(t, qt.DeepEquals(got, tc.want))
		}
	}
}

func TestFieldsEscapedGlob(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Env: ListEnviron("PWD=/"),
		ReadDir2: func(string) ([]fs.DirEntry, error) {
			return []fs.DirEntry{
				&mockFileInfo{name: "a.go"},
				&mockFileInfo{name: "b.go"},
			}, nil
		},
	}
	tests := []struct {
		src  string
		want []string
	}{
		{`*.go`, []string{"a.go", "b.go"}},
		{`"*".go`, []string{"*.go"}},
		{`'*'.go`, []string{"*.go"}},
		{`\*.go`, []string{"*.go"}},
		{`\[a].go`, []string{"[a].go"}},
		{`a\*`, []string{"a*"}},
	}
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			p := syntax.NewParser()
			var words []*syntax.Word
			for w, err := range p.WordsSeq(strings.NewReader(tc.src)) {
				qt.Assert(t, qt.IsNil(err))
				words = append(words, w)
			}
			got, err := Fields(cfg, words...)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.DeepEquals(got, tc.want), qt.Commentf("input: %q", tc.src))
		})
	}
}

func Test_glob(t *testing.T) {
	cfg := &Config{
		ReadDir2: func(string) ([]fs.DirEntry, error) {
			return []fs.DirEntry{
				// The filenames here are sorted, just like [io/fs.ReadDirFS].
				&mockFileInfo{name: "A"},
				&mockFileInfo{name: "AB"},
				&mockFileInfo{name: "a"},
				&mockFileInfo{name: "ab"},
			}, nil
		},
	}

	tests := []struct {
		noCaseGlob bool
		pat        string
		want       []string
	}{
		{false, "a*", []string{"a", "ab"}},
		{false, "A*", []string{"A", "AB"}},
		{false, "*b", []string{"ab"}},
		{false, "b*", nil},
		{true, "a*", []string{"A", "AB", "a", "ab"}},
		{true, "A*", []string{"A", "AB", "a", "ab"}},
		{true, "*b", []string{"AB", "ab"}},
		{true, "b*", nil},
	}
	for _, tc := range tests {
		t.Run(tc.pat, func(t *testing.T) {
			cfg.NoCaseGlob = tc.noCaseGlob
			got, err := cfg.glob("/", tc.pat)
			if err != nil {
				t.Fatalf("did not want error, got %v", err)
			}
			qt.Assert(t, qt.DeepEquals(got, tc.want))
		})
	}
}

type mockFileInfo struct {
	name        string
	typ         fs.FileMode
	fs.DirEntry // Stub out everything but Name() & Type()
}

var _ fs.DirEntry = (*mockFileInfo)(nil)

func (fi *mockFileInfo) Name() string      { return fi.name }
func (fi *mockFileInfo) Type() fs.FileMode { return fi.typ }

// mapEnviron is a writable environment for tests.
type mapEnviron map[string]Variable

func (m mapEnviron) Get(name string) Variable { return m[name] }

func (m mapEnviron) Each(fn func(name string, vr Variable) bool) {
	for name, vr := range m {
		if !fn(name, vr) {
			return
		}
	}
}

func (m mapEnviron) Set(name string, vr Variable) error {
	m[name] = vr
	return nil
}

func TestZshWholeReplaceAndAssignAlways(t *testing.T) {
	p := syntax.NewParser(syntax.Variant(syntax.LangZsh))
	word, err := p.Document(strings.NewReader("${a:/f*/x} ${b:/f/x} ${c::=new} $c"))
	qt.Assert(t, qt.IsNil(err))
	env := mapEnviron{}
	for name, value := range map[string]string{"a": "foo", "b": "foo", "c": "old"} {
		env[name] = Variable{Set: true, Kind: String, Str: value}
	}
	got, err := Literal(&Config{Env: env}, word)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, "x foo new new"))
}

func TestFormatBackslashInDirective(t *testing.T) {
	t.Parallel()
	// bash: printf: `\': invalid format character
	_, _, err := Format(nil, `%1\A00x`, []string{"0"})
	qt.Assert(t, qt.ErrorMatches(err, `invalid format char: \\`))
}

func literalWithEnv(t *testing.T, in string) (string, error) {
	t.Helper()
	word, err := syntax.NewParser().Document(strings.NewReader(in))
	qt.Assert(t, qt.IsNil(err))
	env := mapEnviron{
		"s": {Set: true, Kind: String, Str: "abc"},
		"a": {Set: true, Kind: Indexed, List: []string{"x", "y"}},
	}
	return Literal(&Config{Env: env}, word)
}

func TestIntegerConversionBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"$(( 9223372036854775807 + 1 ))", "-9223372036854775808"},
		{"$(( 2 ** 63 ))", "-9223372036854775808"},
		{"$(( x = 9223372036854775807, x += 1 ))", "-9223372036854775808"},
		{"$(( 1 << 63 ))", "-9223372036854775808"},
		{"$(( 1 << 64 ))", "0"},
		{"$(( 1 << -1 ))", "0"},
		{"$(( -8 >> 1 ))", "-4"},
		{"$(( -8 >> 64 ))", "-1"},
		{"$(( 8 >> -1 ))", "0"},
		{"$(( x = 1, x <<= 64 ))", "0"},
		{"$(( x = -1, x >>= -1 ))", "-1"},
		{"${s:9223372036854775807}", ""},
		{"${s: -9223372036854775808}", ""},
		{"${s:1:-9223372036854775808}", "bc"},
		{"${a[@]:9223372036854775807}", ""},
		{"${a[@]: -9223372036854775808}", ""},
		{"${a[@]:1:9223372036854775807}", "y"},
		{"${a[9223372036854775807]}", ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := literalWithEnv(t, tc.in)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(got, tc.want))
		})
	}
	escapes := []struct {
		in, want string
	}{
		{`\xff`, "\xff"},
		{`\U0010ffff`, "\U0010ffff"},
		{`\U00110000`, "\ufffd"},
		{`\Uffffffff`, "\ufffd"},
	}
	for _, tc := range escapes {
		t.Run(tc.in, func(t *testing.T) {
			got, _, err := Format(nil, tc.in, nil)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(got, tc.want))
		})
	}
	t.Run("negative index below the array", func(t *testing.T) {
		_, err := literalWithEnv(t, "${a[-9223372036854775808]}")
		qt.Assert(t, qt.ErrorMatches(err, "negative array index"))
	})
}
