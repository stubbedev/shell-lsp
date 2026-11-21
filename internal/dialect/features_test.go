package dialect

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/referencetest"
)

type featureProbe struct {
	script         string
	holdsOnFailure bool
}

var featureProbes = map[string]featureProbe{
	"Arrays":                       {script: `a=(x y); [ "${#a[@]}" = 2 ]`},
	"DoubleBracket":                {script: `[[ a == a ]]`},
	"Local":                        {script: `v=0; f() { local v=1; [ "$v" = 1 ]; }; f && [ "$v" = 0 ]`},
	"SplitsUnquoted":               {script: `v="a b"; set -- $v; [ "$#" = 2 ]`},
	"GlobsUnquoted":                {script: `v="prob*"; set -- $v; [ "$1" = probe ]`},
	"LastPipeInShell":              {script: `v=; echo x | read v; [ "$v" = x ]`},
	"DeclarationAssigns":           {script: `x="a b"; export v=$x; [ "$v" = "a b" ]`},
	"ArithmeticStatus":             {script: `f() { return 2+1; }; f; [ "$?" = 3 ]`},
	"AssignsPositionals":           {script: `set -- a; 1=b; [ "$1" = b ]`},
	"AssociativePairs":             {script: `typeset -A m; m=(k v); [ "${m[k]}" = v ]`},
	"QuotedRegexLiteral":           {script: `[[ ab =~ "a." ]]`, holdsOnFailure: true},
	"SubscriptsRunCode":            {script: `x='a[$(: > ran)0]'; : $(( x )); [ -e ran ]`},
	"ClearsErrexitInSubstitutions": {script: `set -e; f() { false; echo reached; }; v=$(f); [ "$v" = reached ]`},
}

type shellVersion struct {
	script string
	want   string
}

type featureRunner struct {
	shell   Shell
	argv    []string
	version shellVersion
}

var (
	bashVersion = shellVersion{script: `echo "${BASH_VERSINFO[0]}.${BASH_VERSINFO[1]}"`, want: "5.3"}

	featureRunners = []featureRunner{
		{shell: Shell{Dialect: Bash}, argv: []string{"bash"}, version: bashVersion},
		{shell: FlavorBats.Shell(), argv: []string{"bash"}, version: bashVersion},
		{shell: Shell{Dialect: Zsh}, argv: []string{"zsh"}, version: shellVersion{script: `echo "${ZSH_VERSION%.*}"`, want: "5.9"}},
		{shell: Shell{Dialect: Dash}, argv: []string{"dash"}},
		{shell: Shell{Dialect: Ksh}, argv: []string{"ksh"}, version: shellVersion{script: `set -- ${.sh.version}; v=$3; echo "${v%.*}"`, want: "93u+m/1.0"}},
		{shell: FlavorMksh.Shell(), argv: []string{"mksh"}, version: shellVersion{script: `set -- $KSH_VERSION; echo "$3"`, want: "R59"}},
		{shell: Shell{Dialect: Busybox}, argv: []string{"busybox", "sh"}, version: shellVersion{script: `set -- $(busybox 2>&1); echo "${2%.*}"`, want: "v1.37"}},
	}
)

const probeWrapper = `(eval "$PROBE") >/dev/null 2>&1; case $? in 0) echo yes ;; *) echo no ;; esac`

func TestEveryFeatureHasOneProbe(t *testing.T) {
	t.Parallel()

	fields := reflect.VisibleFields(reflect.TypeFor[Features]())
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.Name)
		if _, ok := featureProbes[field.Name]; !ok {
			t.Errorf("Features.%s has no probe that checks it against the real shells", field.Name)
		}
	}
	for name := range featureProbes {
		if !slices.Contains(names, name) {
			t.Errorf("the probe %q names no field of Features", name)
		}
	}
}

func TestEveryShellHasARunner(t *testing.T) {
	t.Parallel()

	var covered Set
	flavorsCovered := map[Flavor]bool{}
	for _, r := range featureRunners {
		covered |= SetOf(r.shell.Dialect)
		flavorsCovered[r.shell.Flavor] = true
	}
	for d := range (All &^ SetOf(Posix)).Members() {
		if !covered.Has(d) {
			t.Errorf("%s has no real shell that checks its features", d)
		}
	}
	if covered.Has(Posix) {
		t.Error("posix sh is a standard rather than one shell, so no single binary can stand for its features")
	}
	for f := range flavors {
		if Flavor(f) != FlavorNone && !flavorsCovered[Flavor(f)] {
			t.Errorf("the %s flavor has no real shell that checks its features", Flavor(f).Shell().Lang())
		}
	}
}

func TestFeaturesMatchRealShells(t *testing.T) {
	t.Parallel()

	for _, r := range featureRunners {
		t.Run(strings.Join(r.argv, " ")+"/"+r.shell.Lang().String(), func(t *testing.T) {
			t.Parallel()
			if _, err := exec.LookPath(r.argv[0]); err != nil {
				referencetest.Unavailable(t, "checking the "+r.shell.Dialect.String()+" features needs "+r.argv[0]+" on PATH: "+err.Error())
			}
			if r.version.script != "" {
				if got := shellOutput(t, r.argv, r.version.script, t.TempDir()); got != r.version.want {
					referencetest.Unavailable(t, "the "+r.shell.Dialect.String()+" features describe "+r.argv[0]+" "+r.version.want+", but "+got+" is installed")
				}
			}
			features := reflect.ValueOf(r.shell.Features())
			for name, probe := range featureProbes {
				want := features.FieldByName(name).Bool() != probe.holdsOnFailure
				if got := runProbe(t, r.argv, probe.script); got != want {
					t.Errorf("Features.%s is %t, but %s says %t: %s", name, features.FieldByName(name).Bool(), r.argv[0], got != probe.holdsOnFailure, probe.script)
				}
			}
		})
	}
}

func runProbe(t *testing.T, argv []string, script string) bool {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe"), nil, 0o600); err != nil {
		t.Fatalf("creating the glob target for %q: %v", script, err)
	}
	switch answer := shellOutput(t, argv, probeWrapper, dir, "PROBE="+script); answer {
	case "yes":
		return true
	case "no":
		return false
	default:
		t.Fatalf("%s running %q printed %q, want yes or no", argv[0], script, answer)
		return false
	}
}

func shellOutput(t *testing.T, argv []string, script, dir string, env ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], append(argv[1:], "-c", script)...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), "LC_ALL=C"), env...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s running %q with %q: %v", argv[0], script, env, err)
	}
	return strings.TrimSpace(string(out))
}
