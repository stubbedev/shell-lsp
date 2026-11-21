package shelldocs

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/referencetest"
)

var optionReferences = map[dialect.Dialect]referencetest.Shell{
	dialect.Dash:    referencetest.Dash,
	dialect.Busybox: referencetest.Busybox,
}

var execFlagProbes = map[string]string{
	"-a": `exec -a true "$(command -v cat)" < /dev/null`,
	"-c": `exec -c "$(command -v true)"`,
	"-l": `exec -l "$(command -v true)"`,
}

func TestEveryExecFlagHasAProbe(t *testing.T) {
	t.Parallel()

	for _, flag := range builtinFlags["exec"] {
		if _, ok := execFlagProbes[flag.Name]; !ok {
			t.Errorf("exec %s has no probe that checks it against the real shells", flag.Name)
		}
	}
	if len(execFlagProbes) != len(builtinFlags["exec"]) {
		t.Errorf("%d exec flag probes for %d documented exec flags", len(execFlagProbes), len(builtinFlags["exec"]))
	}
}

func TestSetOptionsMatchRealShells(t *testing.T) {
	t.Parallel()

	for d, reference := range optionReferences {
		t.Run(reference.Name, func(t *testing.T) {
			t.Parallel()
			reference.Require(t)
			letters := map[byte]bool{}
			for _, o := range shellOptions {
				patched := d == dialect.Dash && o.debianDash
				for _, l := range o.letters {
					letters[l.letter] = letters[l.letter] || !patched
				}
				want := o.dialects.Has(d)
				if patched {
					if want {
						t.Errorf("set -o %s is marked as added only by Debian's dash, but is documented for dash", o.name)
					}
					continue
				}
				if d == dialect.Dash && o.dashSince != "" {
					if !want {
						t.Errorf("set -o %s names the dash release %s that added it, but is not documented for dash", o.name, o.dashSince)
					}
					continue
				}
				if got := reference.Succeeds(t, "set -o "+o.name); got != want {
					t.Errorf("set -o %s documented for %s = %t, but %s accepts it = %t", o.name, d, want, reference.Name, got)
				}
			}
			for letter, checked := range letters {
				if !checked {
					continue
				}
				_, want := SetLetter(letter, d)
				if got := reference.Succeeds(t, "set -"+string(letter)); got != want {
					t.Errorf("set -%c documented for %s = %t, but %s accepts it = %t", letter, d, want, reference.Name, got)
				}
			}
		})
	}
}

func TestExecFlagsMatchRealShells(t *testing.T) {
	t.Parallel()

	for d, reference := range optionReferences {
		t.Run(reference.Name, func(t *testing.T) {
			t.Parallel()
			reference.Require(t)
			for flag, script := range execFlagProbes {
				_, want := BuiltinFlag("exec", flag, d)
				if got := reference.Succeeds(t, script); got != want {
					t.Errorf("exec %s documented for %s = %t, but %s runs %q = %t", flag, d, want, reference.Name, script, got)
				}
			}
		})
	}
}
