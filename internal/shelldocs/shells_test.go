package shelldocs

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/referencetest"
)

type optionRunner struct {
	shell     dialect.Shell
	reference referencetest.Shell
}

var optionRunners = []optionRunner{
	{shell: dialect.Shell{Dialect: dialect.Bash}, reference: referencetest.Bash},
	{shell: dialect.Shell{Dialect: dialect.Zsh}, reference: referencetest.Zsh},
	{shell: dialect.Shell{Dialect: dialect.Ksh}, reference: referencetest.Ksh},
	{shell: dialect.FlavorMksh.Shell(), reference: referencetest.Mksh},
	{shell: dialect.Shell{Dialect: dialect.Dash}, reference: referencetest.Dash},
	{shell: dialect.Shell{Dialect: dialect.Busybox}, reference: referencetest.Busybox},
}

var execFlagProbes = map[string]string{
	"-a": `exec -a true "$(command -v cat)" < /dev/null`,
	"-c": `exec -c "$(command -v true)"`,
	"-l": `exec -l awk 'BEGIN { exit }'`,
}

func (v kshVariant) has(s dialect.Shell) bool {
	switch v {
	case ksh93Only:
		return s.Dialect != dialect.Ksh || s.Flavor != dialect.FlavorMksh
	case mkshOnly:
		return s.Dialect != dialect.Ksh || s.Flavor == dialect.FlavorMksh
	default:
		return true
	}
}

func (o shellOption) documentedIn(s dialect.Shell) bool {
	return o.dialects.Has(s.Dialect) && o.ksh.has(s)
}

func (o shellOption) lettersIn(s dialect.Shell) []byte {
	var letters []byte
	for _, l := range o.letters {
		if o.documentedIn(s) && l.dialects.Has(s.Dialect) && l.ksh.has(s) {
			letters = append(letters, l.letter)
		}
	}
	slices.Sort(letters)
	return letters
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

func TestSetOptionDataIsConsistent(t *testing.T) {
	t.Parallel()

	shells := []dialect.Shell{dialect.FlavorMksh.Shell()}
	for d := range dialect.All.Members() {
		shells = append(shells, dialect.Shell{Dialect: d})
	}
	for _, o := range shellOptions {
		if o.debianDash && o.dialects.Has(dialect.Dash) {
			t.Errorf("set -o %s is marked as added only by Debian's dash, but is documented for dash", o.name)
		}
		if o.dashSince != "" && !o.dialects.Has(dialect.Dash) {
			t.Errorf("set -o %s names the dash release %s that added it, but is not documented for dash", o.name, o.dashSince)
		}
		if o.ksh != everyKsh && !o.dialects.Has(dialect.Ksh) {
			t.Errorf("set -o %s names the ksh that has it, but is not documented for ksh", o.name)
		}
		for _, l := range o.letters {
			if extra := l.dialects &^ o.dialects; extra != 0 {
				t.Errorf("set -%c is documented for %v, where set -o %s is not", l.letter, extra, o.name)
			}
			if l.ksh != everyKsh && (o.ksh != everyKsh || !l.dialects.Has(dialect.Ksh)) {
				t.Errorf("set -%c names the ksh that has it, but set -o %s already does or ksh lacks the letter", l.letter, o.name)
			}
		}
	}
	for _, s := range shells {
		owners := map[byte]string{}
		for _, o := range shellOptions {
			for _, letter := range o.lettersIn(s) {
				if owner, taken := owners[letter]; taken {
					t.Errorf("set -%c in %s is documented as both %s and %s", letter, s.Lang(), owner, o.name)
				}
				owners[letter] = o.name
			}
		}
	}
}

func TestSetOptionsMatchRealShells(t *testing.T) {
	t.Parallel()

	for _, r := range optionRunners {
		t.Run(r.reference.Name, func(t *testing.T) {
			t.Parallel()
			r.reference.Require(t)
			var accepted []shellOption
			for _, o := range shellOptions {
				if r.shell.Dialect == dialect.Dash && (o.debianDash || o.dashSince != "") {
					continue
				}
				want := o.documentedIn(r.shell)
				got := r.reference.Succeeds(t, "set +o "+o.name)
				if got != want {
					t.Errorf("set -o %s documented for %s = %t, but %s accepts it = %t", o.name, r.shell.Lang(), want, r.reference.Name, got)
				}
				if got && want {
					accepted = append(accepted, o)
				}
			}
			checkSetLetters(t, r, accepted)
		})
	}
}

func checkSetLetters(t *testing.T, r optionRunner, options []shellOption) {
	t.Helper()
	var scripts []string
	for _, o := range options {
		scripts = append(scripts, enablingScript("+o "+o.name, "-o "+o.name))
		for _, letter := range o.lettersIn(r.shell) {
			if !r.reference.Succeeds(t, "set +"+string(letter)) {
				t.Errorf("set -%c is documented for %s as %s, but %s rejects it", letter, r.shell.Lang(), o.name, r.reference.Name)
			}
			scripts = append(scripts, enablingScript("+"+string(letter), "-"+string(letter)))
		}
	}
	outputs := r.reference.RunEach(t, scripts)
	for _, o := range options {
		byName := addedLetters(outputs[0])
		outputs = outputs[1:]
		letters := o.lettersIn(r.shell)
		if byName.completed && len(byName.letters) > 0 && !slices.Equal(byName.letters, letters) {
			t.Errorf("set -o %s in %s %s, but the documented letters are %q", o.name, r.reference.Name, byName, letters)
		}
		for _, letter := range letters {
			byLetter := addedLetters(outputs[0])
			outputs = outputs[1:]
			if !slices.Equal(byLetter.letters, byName.letters) || byLetter.completed != byName.completed {
				t.Errorf("set -%c in %s %s, but set -o %s, its documented name, %s", letter, r.reference.Name, byLetter, o.name, byName)
			}
		}
	}
}

func enablingScript(off, on string) string {
	return `exec 2>/dev/null; set ` + off + `; printf '%s\n' "$-"; set ` + on + `; printf '%s\n' "$-"`
}

type flagChange struct {
	letters   []byte
	completed bool
}

func (c flagChange) String() string {
	switch {
	case !c.completed:
		return "stops the shell before it reads $-"
	case len(c.letters) == 0:
		return "adds no letter to $-"
	default:
		return "adds " + string(c.letters) + " to $-"
	}
}

func addedLetters(output string) flagChange {
	before, after, completed := strings.Cut(strings.TrimSuffix(output, "\n"), "\n")
	if !completed {
		return flagChange{}
	}
	var letters []byte
	for i := range len(after) {
		if !strings.Contains(before, after[i:i+1]) {
			letters = append(letters, after[i])
		}
	}
	slices.Sort(letters)
	return flagChange{letters: letters, completed: true}
}

func TestExecFlagsMatchRealShells(t *testing.T) {
	t.Parallel()

	for _, r := range optionRunners {
		t.Run(r.reference.Name, func(t *testing.T) {
			t.Parallel()
			r.reference.Require(t)
			for flag, script := range execFlagProbes {
				_, want := BuiltinFlag("exec", flag, r.shell.Dialect)
				if got := r.reference.Succeeds(t, script); got != want {
					t.Errorf("exec %s documented for %s = %t, but %s runs %q = %t", flag, r.shell.Lang(), want, r.reference.Name, script, got)
				}
			}
		})
	}
}
