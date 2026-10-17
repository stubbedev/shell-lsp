package hosttest

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/oracle"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

type Case struct {
	Name string
	Text string
}

const (
	sectionOpen  = "-- "
	sectionClose = " --"
)

func Sections(t testing.TB, path string) []Case {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // G304: the path is a fixture file the test names
	if err != nil {
		t.Fatal(err)
	}
	var out []Case
	for line := range strings.SplitAfterSeq(string(raw), "\n") {
		trimmed := strings.TrimSuffix(line, "\n")
		if name, ok := strings.CutPrefix(trimmed, sectionOpen); ok && strings.HasSuffix(name, sectionClose) {
			out = append(out, Case{Name: strings.TrimSuffix(name, sectionClose)})
			continue
		}
		if len(out) == 0 {
			if strings.TrimSpace(line) != "" {
				t.Fatalf("%s: text before the first %q section", path, sectionOpen)
			}
			continue
		}
		out[len(out)-1].Text += line
	}
	seen := map[string]bool{}
	for _, c := range out {
		if seen[c.Name] {
			t.Fatalf("%s: section %q appears twice", path, c.Name)
		}
		seen[c.Name] = true
	}
	return out
}

func position(text string, offset int) (int, int) {
	line := strings.Count(text[:offset], "\n") + 1
	return line, offset - (strings.LastIndexByte(text[:offset], '\n') + 1) + 1
}

func Describe(text string, findings []lint.Finding) []string {
	sorted := slices.Clone(findings)
	slices.SortStableFunc(sorted, func(a, b lint.Finding) int {
		return cmp.Or(cmp.Compare(a.Span.Start, b.Span.Start), cmp.Compare(a.Code, b.Code))
	})
	out := make([]string, len(sorted))
	for i, f := range sorted {
		line, column := position(text, f.Span.Start)
		out[i] = fmt.Sprintf("%d:%d %s %s %s", line, column, f.Code, f.Severity, f.Message)
	}
	return out
}

type Oracle func(t *testing.T, text string) oracle.Verdict

type Analyze func(text string) []lint.Finding

type golden struct {
	verdict  oracle.Verdict
	findings []string
}

func Check(t *testing.T, path string, analyze Analyze, tool Oracle, update bool) {
	t.Helper()
	cases := Sections(t, path)
	goldenPath := strings.TrimSuffix(path, ".txt") + ".golden"
	recorded := map[string]golden{}
	if update {
		var sb strings.Builder
		for _, c := range cases {
			v := tool(t, c.Text)
			fmt.Fprintf(&sb, "%s%s%s\n%s\n", sectionOpen, c.Name, sectionClose, v)
			for _, line := range Describe(c.Text, analyze(c.Text)) {
				sb.WriteString(line + "\n")
			}
		}
		if err := os.WriteFile(goldenPath, []byte(sb.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, g := range Sections(t, goldenPath) {
		lines := strings.Split(strings.TrimSuffix(g.Text, "\n"), "\n")
		v, ok := oracle.Parse(lines[0])
		if !ok {
			t.Fatalf("%s: section %q starts with %q, not a verdict", goldenPath, g.Name, lines[0])
		}
		var findings []string
		if len(lines) > 1 {
			findings = lines[1:]
		}
		recorded[g.Name] = golden{verdict: v, findings: findings}
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			want, ok := recorded[c.Name]
			if !ok {
				t.Fatalf("%s has no section %q; record it with -update", goldenPath, c.Name)
			}
			findings := analyze(c.Text)
			got := Describe(c.Text, findings)
			if !slices.Equal(got, want.findings) {
				t.Errorf("findings\n got %q\nwant %q", got, want.findings)
			}
			Agree(t, c.Text, want.verdict, findings)
		})
	}
}

func Agree(t *testing.T, text string, v oracle.Verdict, findings []lint.Finding) {
	t.Helper()
	var errorLines []int
	for _, f := range findings {
		if f.Severity == lint.SeverityError {
			line, _ := position(text, f.Span.Start)
			errorLines = append(errorLines, line)
		}
	}
	last, _ := position(text, len(text))
	if problem, ok := oracle.Disagreement(v, last, errorLines); ok {
		t.Errorf("%s: %q: %q", problem, text, Describe(text, findings))
	}
}

const (
	JustVersion = "just 1.58.0"
	MakeVersion = "GNU Make 4.4.1"
)

func RequireTool(t *testing.T, name, want string) {
	t.Helper()
	got, err := toolVersion(t, name)
	if err != nil {
		t.Fatalf("-update needs %s on PATH as the reference oracle: %v", name, err)
	}
	if got != want {
		t.Fatalf("the golden files are recorded with %s, but %s is installed; run -update from the nix dev shell", want, got)
	}
}

func LiveTool(t *testing.T, name, want string) {
	t.Helper()
	got, err := toolVersion(t, name)
	if err != nil {
		t.Skipf("comparing with the live reference needs %s on PATH: %v", name, err)
	}
	if got != want {
		t.Skipf("comparing with the live reference needs %s, but %s is installed", want, got)
	}
}

func toolVersion(t *testing.T, name string) (string, error) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), name, "--version").Output()
	if err != nil {
		return "", err
	}
	version, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return version, nil
}

func Findings(ix *model.Index, checks model.Checks, builtins model.Builtins) []lint.Finding {
	m := &model.Module{Files: []*model.Index{ix}, Complete: len(ix.Includes) == 0, Builtins: builtins}
	return append(slices.Clone(ix.Findings), m.Findings(0, checks, m.Usage())...)
}

func Bounded(t *testing.T, text string, ix *model.Index) {
	t.Helper()
	within := func(what string, s parser.Span) {
		if s.Start < 0 || s.Start > s.End || s.End > len(text) {
			t.Fatalf("%s span %v lies outside the %d bytes of %q", what, s, len(text), text)
		}
	}
	for _, s := range ix.Symbols {
		within("symbol "+s.Name, s.Span)
		within("symbol selection "+s.Name, s.Selection)
		if s.Selection.Start < s.Span.Start || s.Selection.End > s.Span.End {
			t.Fatalf("symbol %s selects %v outside its span %v in %q", s.Name, s.Selection, s.Span, text)
		}
		if s.Parent != model.NoParent && (s.Parent < 0 || s.Parent >= len(ix.Symbols)) {
			t.Fatalf("symbol %s has parent %d of %d symbols in %q", s.Name, s.Parent, len(ix.Symbols), text)
		}
	}
	for _, r := range ix.Refs {
		within("reference "+r.Name, r.Span)
	}
	for _, inc := range ix.Includes {
		within("include "+inc.Path, inc.Span)
	}
	for i, sh := range ix.Shells {
		within("shell", sh.Span)
		if i > 0 && ix.Shells[i-1].Span.End > sh.Span.Start {
			t.Fatalf("shell regions %v and %v overlap or are out of order in %q", ix.Shells[i-1].Span, sh.Span, text)
		}
	}
	for _, f := range ix.Folds {
		within("fold", f.Span)
	}
	for _, tok := range ix.Tokens {
		within("token", tok.Span)
	}
	for _, r := range ix.Runnables {
		within("runnable "+r.Name, r.Span)
	}
	for _, f := range ix.Findings {
		within("finding "+f.Code.String(), f.Span)
	}
}

func JustVerdict(t *testing.T, text string) oracle.Verdict {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "justfile")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return oracle.Just(t.Context(), path, dir, os.Environ())
}

const unrunnablePath = "/nonexistent/shell-lsp-fuzz"

var stoppedBeforeRecipes = []string{"*** No rule to make target", unrunnablePath + ": No such file or directory"}

var databaseTarget = regexp.MustCompile(`^([^.#%\s][^:=%]*?):(?:[^=]|$)`)

func makeTargets(t *testing.T, dir string, vars []string) []string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "make", append([]string{"-pqrR", "--no-print-directory", "-f", "Makefile"}, vars...)...) //nolint:gosec // G204: the variables are this package's constants
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	out, _ := cmd.Output()
	var targets []string
	files, notTarget := false, false
	for line := range strings.SplitSeq(string(out), "\n") {
		switch {
		case line == "# Files":
			files = true
		case strings.HasPrefix(line, "# ") && strings.HasSuffix(line, "database") || line == "# VPATH Search Paths":
			files = false
		case line == "# Not a target:":
			notTarget = true
			continue
		case files && !notTarget:
			if m := databaseTarget.FindStringSubmatch(line); m != nil && m[1] != "Makefile" && !strings.Contains(m[1], "=") && !strings.HasPrefix(m[1], "-") {
				targets = append(targets, m[1])
			}
		}
		notTarget = false
	}
	slices.Sort(targets)
	return slices.Compact(targets)
}

var (
	makeFatal          = regexp.MustCompile(`(?m)^Makefile:(\d+): \*\*\* (.*?)\.  Stop\.$`)
	makeMissingInclude = regexp.MustCompile(`(?m)^Makefile:(\d+): (\S+: No such file or directory)$`)
)

func MakeVerdict(t *testing.T, text string) oracle.Verdict {
	t.Helper()
	return makeVerdict(t, text)
}

const unrunnableShell = "SHELL=" + unrunnablePath

var shellReach = []string{"shell", "SHELL", "!=", "eval", "call", "override", "file", "load", "guile", "include", "MAKE"}

func MakeIsolated(text string) bool {
	return !slices.ContainsFunc(shellReach, func(word string) bool { return strings.Contains(text, word) })
}

func IsolatedMakeVerdict(t *testing.T, text string) oracle.Verdict {
	t.Helper()
	if !MakeIsolated(text) {
		t.Fatalf("make could run a command from %q", text)
	}
	return makeVerdict(t, text, unrunnableShell)
}

func makeVerdict(t *testing.T, text string, vars ...string) oracle.Verdict {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	goals := append(slices.Clone(vars), makeTargets(t, dir, vars)...)
	cmd := exec.CommandContext(t.Context(), "make", append([]string{"--dry-run", "--no-print-directory", "-f", "Makefile"}, goals...)...) //nolint:gosec // G204: the goals are names make itself listed
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		return oracle.Verdict{Accepted: true}
	}
	out := stderr.String()
	for _, pattern := range []*regexp.Regexp{makeFatal, makeMissingInclude} {
		if m := pattern.FindStringSubmatch(out); m != nil {
			line, _ := strconv.Atoi(m[1])
			return oracle.Verdict{Line: line, Column: 1, Message: m[2]}
		}
	}
	if strings.Contains(out, "*** No targets.") {
		return oracle.Verdict{Accepted: true}
	}
	for _, o := range stoppedBeforeRecipes {
		if strings.Contains(out, o) {
			return oracle.Verdict{Inconclusive: true, Message: "make stopped before it expanded every recipe: " + o}
		}
	}
	message, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	return oracle.Verdict{Message: message}
}
