package dialect

import (
	"reflect"
	"slices"
	"testing"

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

type featureRunner struct {
	shell     Shell
	reference referencetest.Shell
}

var featureRunners = []featureRunner{
	{shell: Shell{Dialect: Bash}, reference: referencetest.Bash},
	{shell: FlavorBats.Shell(), reference: referencetest.Bash},
	{shell: Shell{Dialect: Zsh}, reference: referencetest.Zsh},
	{shell: Shell{Dialect: Dash}, reference: referencetest.Dash},
	{shell: Shell{Dialect: Ksh}, reference: referencetest.Ksh},
	{shell: FlavorMksh.Shell(), reference: referencetest.Mksh},
	{shell: Shell{Dialect: Busybox}, reference: referencetest.Busybox},
}

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
		t.Run(r.reference.Name+"/"+r.shell.Lang().String(), func(t *testing.T) {
			t.Parallel()
			r.reference.Require(t)
			features := reflect.ValueOf(r.shell.Features())
			for name, probe := range featureProbes {
				want := features.FieldByName(name).Bool() != probe.holdsOnFailure
				if got := r.reference.Succeeds(t, probe.script); got != want {
					t.Errorf("Features.%s is %t, but %s says %t: %s", name, features.FieldByName(name).Bool(), r.reference.Name, got != probe.holdsOnFailure, probe.script)
				}
			}
		})
	}
}
