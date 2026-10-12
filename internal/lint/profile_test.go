package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const profileScript = "#!/bin/bash\nx=`date`\necho $x\ny=1\necho \"a\\tb\"\necho \"$HOME_DIR\"\n"

func profileCodes(t *testing.T, src string, opts Options) []Code {
	t.Helper()
	var out []Code
	for _, f := range Check(parser.Parse(src, dialect.Bash), nil, opts) {
		if !slices.Contains(out, f.Code) {
			out = append(out, f.Code)
		}
	}
	slices.Sort(out)
	return out
}

func TestProfiles(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		src     string
		opts    Options
		want    []Code
		missing []Code
	}{
		{"recommended", profileScript, Options{}, []Code{2006, 2028, 2034, 2086}, []Code{2250, 2154}},
		{"minimal", profileScript, Options{Profile: ProfileMinimal}, []Code{2034, 2086}, []Code{2006, 2028, 2250}},
		{"strict", profileScript, Options{Profile: ProfileStrict}, []Code{2006, 2028, 2034, 2086, 2250}, []Code{2154}},
		{"minimal keeps an optional check named in settings", profileScript, Options{Profile: ProfileMinimal, Optional: []string{"require-variable-braces"}}, []Code{2250, 2086}, []Code{2006}},
		{"minimal keeps an optional check a directive enables", "#!/bin/bash\n# shellcheck enable=require-variable-braces\nx=1\necho \"$x\"\n", Options{Profile: ProfileMinimal}, []Code{2250}, nil},
		{"exclude wins over strict", profileScript, Options{Profile: ProfileStrict, Exclude: CodesOf(2250)}, []Code{2086}, []Code{2250}},
		{"a disable directive wins over minimal", "#!/bin/bash\n# shellcheck disable=SC2086\nx=$1\necho $x\n", Options{Profile: ProfileMinimal}, nil, []Code{2086}},
		{"strict leaves style-guide preferences out", "#!/bin/bash\nmode=a\nmode=b\necho \"$mode\"\n", Options{Profile: ProfileStrict}, nil, []Code{codeReassignedGlobal}},
		{"strict reports stale directives", "#!/bin/bash\n# shellcheck disable=SC2086\necho hi\n", Options{Profile: ProfileStrict}, []Code{codeUnusedDirective}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := profileCodes(t, tt.src, tt.opts)
			for _, code := range tt.want {
				if !slices.Contains(got, code) {
					t.Errorf("findings %v lack %s", got, code)
				}
			}
			for _, code := range tt.missing {
				if slices.Contains(got, code) {
					t.Errorf("findings %v include %s", got, code)
				}
			}
		})
	}
}

func TestOnlyInformationalChecksAreMarkedAsBugs(t *testing.T) {
	t.Parallel()

	for code, m := range catalog {
		if m.bug && m.severity != SeverityInfo {
			t.Errorf("%s is marked as a bug at severity %s; errors and warnings are always in the minimal profile", code, m.severity)
		}
	}
}

func TestProfileNamesRoundTrip(t *testing.T) {
	t.Parallel()

	for _, name := range ProfileNames() {
		profile, ok := ProfileNamed(name)
		if !ok || profile.String() != name {
			t.Errorf("ProfileNamed(%q) = %v, %t", name, profile, ok)
		}
	}
	if _, ok := ProfileNamed("loud"); ok {
		t.Error("ProfileNamed accepted an unknown profile")
	}
	if p, _ := ProfileNamed("recommended"); p != ProfileRecommended {
		t.Errorf("recommended is %v, want the zero profile so it is the default", p)
	}
}
