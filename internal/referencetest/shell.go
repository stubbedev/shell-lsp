package referencetest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type Version struct {
	Script string
	Want   string
}

type Shell struct {
	Name    string
	Argv    []string
	Version Version
}

var (
	bashVersion = Version{Script: `echo "${BASH_VERSINFO[0]}.${BASH_VERSINFO[1]}"`, Want: "5.3"}

	Bash    = Shell{Name: "bash", Argv: []string{"bash", "--norc", "--noprofile"}, Version: bashVersion}
	Zsh     = Shell{Name: "zsh", Argv: []string{"zsh", "-f"}, Version: Version{Script: `echo "${ZSH_VERSION%.*}"`, Want: "5.9"}}
	Dash    = Shell{Name: "dash", Argv: []string{"dash"}}
	Ksh     = Shell{Name: "ksh", Argv: []string{"ksh"}, Version: Version{Script: `set -- ${.sh.version}; v=$3; echo "${v%.*}"`, Want: "93u+m/1.0"}}
	Mksh    = Shell{Name: "mksh", Argv: []string{"mksh"}, Version: Version{Script: `set -- $KSH_VERSION; echo "$3"`, Want: "R59"}}
	Busybox = Shell{Name: "busybox sh", Argv: []string{"busybox", "sh"}, Version: Version{Script: `set -- $(busybox 2>&1); echo "${2%.*}"`, Want: "v1.37"}}
)

const (
	probeWrapper = `(eval "$PROBE") >/dev/null 2>&1; case $? in 0) echo yes ;; *) echo no ;; esac`
	probeTimeout = 10 * time.Second
	runTimeout   = 2 * time.Minute
	separator    = "\x1e"
)

func (s Shell) Require(tb testing.TB) {
	tb.Helper()
	if _, err := exec.LookPath(s.Argv[0]); err != nil {
		Unavailable(tb, "running "+s.Name+" needs "+s.Argv[0]+" on PATH: "+err.Error())
	}
	if s.Version.Script == "" {
		return
	}
	if got := s.probeOutput(tb, s.Version.Script, tb.TempDir()); got != s.Version.Want {
		Unavailable(tb, "the reference is "+s.Name+" "+s.Version.Want+", but "+got+" is installed")
	}
}

func (s Shell) Succeeds(tb testing.TB, script string) bool {
	tb.Helper()
	dir := tb.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe"), nil, 0o600); err != nil {
		tb.Fatalf("creating the glob target for %q: %v", script, err)
	}
	switch answer := s.probeOutput(tb, probeWrapper, dir, "PROBE="+script); answer {
	case "yes":
		return true
	case "no":
		return false
	default:
		tb.Fatalf("%s running %q printed %q, want yes or no", s.Name, script, answer)
		return false
	}
}

func (s Shell) RunEach(tb testing.TB, scripts []string) []string {
	tb.Helper()
	var batch strings.Builder
	for _, script := range scripts {
		batch.WriteString("( eval " + Quote(script) + " )\nprintf '\\036'\n")
	}
	outputs := strings.Split(s.Run(tb, batch.String()), separator)
	if len(outputs) != len(scripts)+1 {
		tb.Fatalf("%s printed %d outputs for %d scripts", s.Name, len(outputs)-1, len(scripts))
	}
	return outputs[:len(scripts)]
}

func (s Shell) Run(tb testing.TB, script string) string {
	tb.Helper()
	ctx, cancel := context.WithTimeout(tb.Context(), runTimeout)
	defer cancel()
	cmd := s.command(ctx, "-s")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		tb.Fatalf("%s %q: %v\n%s", s.Name, script, err, out)
	}
	return string(out)
}

func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func (s Shell) probeOutput(tb testing.TB, script, dir string, env ...string) string {
	tb.Helper()
	ctx, cancel := context.WithTimeout(tb.Context(), probeTimeout)
	defer cancel()
	cmd := s.command(ctx, "-c", script)
	cmd.Dir = dir
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.Output()
	if err != nil {
		tb.Fatalf("%s running %q with %q: %v", s.Name, script, env, err)
	}
	return strings.TrimSpace(string(out))
}

func (s Shell) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, s.Argv[0], append(s.Argv[1:len(s.Argv):len(s.Argv)], args...)...) //nolint:gosec // the shells are fixed
	cmd.Env = append(environment(), "LC_ALL=C.UTF-8")
	return cmd
}

func environment() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PWD=") && !strings.HasPrefix(kv, "OLDPWD=") {
			env = append(env, kv)
		}
	}
	return env
}
