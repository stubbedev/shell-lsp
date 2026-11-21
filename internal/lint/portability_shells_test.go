package lint

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/referencetest"
)

var portabilityReferences = map[dialect.Dialect]referencetest.Shell{
	dialect.Dash:    referencetest.Dash,
	dialect.Busybox: referencetest.Busybox,
}

var portabilityProbes = map[Code][]string{
	3001: {`[ "$(cat <(echo x))" = x ]`, `echo x > >(cat)`},
	3002: {`case ab in @(ab)) ;; *) false ;; esac`},
	3003: {`v=$'a\x41'; [ "$v" = aA ]`},
	3004: {`[ $"x" = x ]`},
	3005: {`for ((i = 0; i < 1; i++)); do :; done`},
	3006: {`(( 1 ))`},
	3007: {`[ $[1 + 1] = 2 ]`},
	3008: {`echo 1 | { select x in a; do break; done; [ "$x" = a ]; }`},
	3009: {`set -- {a,b}; [ $# = 2 ]`, `set -- {1..3}; [ $# = 3 ]`},
	3010: {`[[ a == a ]]`},
	3011: {`read -r v <<< x; [ "$v" = x ]`},
	3012: {`[ a \< b ] && ! [ b \< a ]`, `[ b \> a ] && ! [ a \> b ]`},
	3014: {`[ a == a ] && ! [ a == b ]`},
	3015: {`[[ ab =~ a. ]]`},
	3016: {`v=; [ -v v ] && ! [ -v probe_unset ]`},
	3017: {`[ -a / ] && ! [ -a /nonexistent ]`},
	3018: {`i=0; : $((i++)); [ "$i" = 1 ]`, `i=1; : $((--i)); [ "$i" = 0 ]`},
	3019: {`[ $((2 ** 3)) = 8 ]`},
	3020: {`{ echo out; echo err >&2; } &> f; [ "$(cat f)" = "$(printf 'out\nerr')" ]`},
	3021: {`{ echo out; echo err >&2; } >& f; [ "$(cat f)" = "$(printf 'out\nerr')" ]`},
	3022: {`exec {fd}> f; [ -n "$fd" ]`},
	3023: {`exec 10> f; echo x >&10; [ "$(cat f)" = x ]`},
	3024: {`v=a; v+=b; [ "$v" = ab ]`},
	3026: {`case b in [^a]) ;; *) false ;; esac && case a in [^a]) false ;; esac`},
	3028: {`[ "$RANDOM" != "$RANDOM" ] || [ "$RANDOM" != "$RANDOM" ]`, `[ "$EPOCHSECONDS" -gt 1000000000 ]`, `[ -n "$BASHPID" ] && [ "$BASHPID" != "$$" ]`},
	3029: {`[ "$( { echo x >&2; } |& cat)" = x ]`},
	3030: {`a=(x y); [ "${#a[@]}" = 2 ]`},
	3031: {`echo x > prob*; [ "$(cat probe)" = x ]`},
	3032: {`coproc { echo x; }; read -r v <&"${COPROC[0]}"; [ "$v" = x ]`},
	3033: {`a-b() { :; }; a-b`},
	3034: {`echo x > f; [ "$(< f)" = x ]`},
	3035: {"echo x > f; [ \"`< f`\" = x ]"},
	3036: {`[ "$(echo -s x)" = x ]`},
	3037: {`[ "$(echo -n x)" = x ]`},
	3038: {`exec -c "$(command -v true)"`, `exec -a true "$(command -v cat)" < /dev/null`},
	3039: {`let x=1+1; [ "$x" = 2 ]`},
	3040: {`set -o pipefail`, `set -o posix`, `set -o emacs`},
	3041: {`set -E`, `set -B`},
	3042: {`set --posix`},
	3043: {`v=0; f() { local v=1; }; f; [ "$v" = 0 ]`},
	3044: {`typeset v=1; [ "$v" = 1 ]`},
	3045: {`printf -v v x; [ "$v" = x ]`, `export -n HOME`},
	3046: {`echo v=1 > f; source ./f; [ "$v" = 1 ]`},
	3047: {`trap 'v=1' ERR; false; [ "$v" = 1 ]`, `trap 'v=1' DEBUG; :; [ "$v" = 1 ]`, `: > f; trap 'v=1' RETURN; . ./f; [ "$v" = 1 ]`},
	3048: {`trap 'v=1' SIGUSR1 && trap > f && read -r line < f && case $line in *v=1*USR1*) ;; *) false ;; esac`},
	3049: {`trap 'v=1' usr1 && trap > f && read -r line < f && case $line in *v=1*USR1*) ;; *) false ;; esac`},
	3050: {`[ "$(printf %q 'a b')" = 'a\ b' ]`},
	3051: {`source /dev/null`},
	3052: {`[ $((16#ff)) = 255 ]`},
	3053: {`v=x; x=1; [ "${!v}" = 1 ]`},
	3054: {`a=x; [ "${a[0]}" = x ]`},
	3055: {`a=x; [ "${!a[@]}" = 0 ]`},
	3056: {`probe_name=1; [ "${!probe_*}" = probe_name ]`},
	3057: {`v=abc; [ "${v:1}" = bc ]`},
	3058: {`set -- a b; [ "${#@}" = 2 ]`, `set -- xa xb; set -- "${@#x}"; [ "$1$2" = ab ]`},
	3059: {`v=a; [ "${v^}" = A ]`},
	3060: {`v=abc; [ "${v/b/x}" = axc ]`},
	3061: {`echo x | { read; [ "$REPLY" = x ]; }`},
	3062: {`set -f; [ -o noglob ] && set +f && ! [ -o noglob ]`},
	3063: {`v=1; [ -R v ]; [ $? = 1 ]`},
	3064: {`: > f; [ -N f ]; [ $? -lt 2 ]`},
	3065: {`: > f; [ -k f ]; [ $? = 1 ]`},
	3066: {`: > f; [ -G f ]`},
	3067: {`: > f; [ -O f ]`},
}

var undecidedPortability = map[Code]string{
	3025: "a /dev/tcp or /dev/udp redirection only succeeds against a listening peer, which a snippet cannot provide on its own",
}

func portabilityCodes() []Code {
	var out []Code
	for _, code := range Codes() {
		if catalog[code].shells != 0 {
			out = append(out, code)
		}
	}
	return out
}

func findingFor(script string, d dialect.Dialect, code Code) (Finding, bool) {
	findings := Check(parser.Parse(script+"\n", d), nil, Options{})
	i := slices.IndexFunc(findings, func(f Finding) bool { return f.Code == code })
	if i < 0 {
		return Finding{}, false
	}
	return findings[i], true
}

func reportsCode(script string, d dialect.Dialect, code Code) bool {
	_, ok := findingFor(script, d, code)
	return ok
}

func TestEveryPortabilityCodeHasAProbe(t *testing.T) {
	t.Parallel()

	codes := portabilityCodes()
	for _, code := range codes {
		_, probed := portabilityProbes[code]
		reason, undecided := undecidedPortability[code]
		switch {
		case probed && undecided:
			t.Errorf("%s has probes and is listed as undecidable: %s", code, reason)
		case !probed && !undecided:
			t.Errorf("%s names the shells it is reported in, but no probe checks them against the real shells", code)
		}
	}
	for _, code := range slices.Concat(slices.Collect(maps.Keys(portabilityProbes)), slices.Collect(maps.Keys(undecidedPortability))) {
		if !slices.Contains(codes, code) {
			t.Errorf("%s is listed for probing but names no shells", code)
		}
	}
}

func TestEveryStrictShellHasAReference(t *testing.T) {
	t.Parallel()

	var reported dialect.Set
	for _, code := range portabilityCodes() {
		reported |= catalog[code].shells
	}
	for d := range (reported &^ dialect.SetOf(dialect.Posix)).Members() {
		if _, ok := portabilityReferences[d]; !ok {
			t.Errorf("portability codes are reported in %s, but no real shell checks them", d)
		}
	}
	if _, ok := portabilityReferences[dialect.Posix]; ok {
		t.Error("posix sh is a standard rather than one shell, so no single binary can stand for it")
	}
}

func TestPortabilityProbesUseTheirConstruct(t *testing.T) {
	t.Parallel()

	for code, scripts := range portabilityProbes {
		for _, script := range scripts {
			if !slices.ContainsFunc(slices.Collect(catalog[code].shells.Members()), func(d dialect.Dialect) bool { return reportsCode(script, d, code) }) {
				t.Errorf("%s is not reported in any of its shells for its probe %q", code, script)
			}
		}
	}
}

func TestPortabilityShellsMatchRealShells(t *testing.T) {
	t.Parallel()

	for d, reference := range portabilityReferences {
		t.Run(reference.Name, func(t *testing.T) {
			t.Parallel()
			reference.Require(t)
			for _, code := range slices.Sorted(maps.Keys(portabilityProbes)) {
				for _, script := range portabilityProbes[code] {
					f, reported := findingFor(script, d, code)
					if reported && d == dialect.Dash && strings.Contains(f.Message, " in "+dashBefore) {
						continue
					}
					if runs := reference.Succeeds(t, script); runs == reported {
						t.Errorf("%s: reported in %s = %t (its shells include it = %t), but %s runs the probe = %t: %s", code, shellLabel(d), reported, catalog[code].shells.Has(d), reference.Name, runs, script)
					}
				}
			}
		})
	}
}
