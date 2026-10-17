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

	"github.com/stubbedev/shell-lsp/internal/lint"
)

type Case struct {
	Name string
	Text string
}

const (
	sectionOpen  = "-- "
	sectionClose = " --"
)

func Sections(t *testing.T, path string) []Case {
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

type Verdict struct {
	Accepted bool
	Line     int
	Column   int
	Message  string
}

func (v Verdict) String() string {
	if v.Accepted {
		return "accepted"
	}
	return fmt.Sprintf("rejected %d:%d %s", v.Line, v.Column, v.Message)
}

func parseVerdict(s string) (Verdict, bool) {
	if s == "accepted" {
		return Verdict{Accepted: true}, true
	}
	rest, ok := strings.CutPrefix(s, "rejected ")
	if !ok {
		return Verdict{}, false
	}
	position, message, _ := strings.Cut(rest, " ")
	lineText, columnText, _ := strings.Cut(position, ":")
	line, err1 := strconv.Atoi(lineText)
	column, err2 := strconv.Atoi(columnText)
	if err1 != nil || err2 != nil {
		return Verdict{}, false
	}
	return Verdict{Line: line, Column: column, Message: message}, true
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

type Oracle func(t *testing.T, text string) Verdict

type Analyze func(text string) []lint.Finding

type golden struct {
	verdict  Verdict
	findings []string
}

func Check(t *testing.T, path string, analyze Analyze, oracle Oracle, update bool) {
	t.Helper()
	cases := Sections(t, path)
	goldenPath := strings.TrimSuffix(path, ".txt") + ".golden"
	recorded := map[string]golden{}
	if update {
		var sb strings.Builder
		for _, c := range cases {
			v := oracle(t, c.Text)
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
		v, ok := parseVerdict(lines[0])
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
			agree(t, c.Text, want.verdict, findings)
		})
	}
}

func agree(t *testing.T, text string, v Verdict, findings []lint.Finding) {
	t.Helper()
	var errorLines []int
	for _, f := range findings {
		if f.Severity == lint.SeverityError {
			line, _ := position(text, f.Span.Start)
			errorLines = append(errorLines, line)
		}
	}
	switch {
	case v.Accepted && len(errorLines) > 0:
		t.Errorf("the reference tool accepts this file, but errors are reported on lines %v", errorLines)
	case !v.Accepted && v.Line == 0 && len(errorLines) == 0:
		t.Errorf("the reference tool rejects this file (%s), but no error is reported", v.Message)
	case !v.Accepted && v.Line > 0 && !slices.Contains(errorLines, v.Line):
		t.Errorf("the reference tool rejects line %d (%s), but errors are reported on lines %v", v.Line, v.Message, errorLines)
	}
}

const (
	JustVersion = "just 1.58.0"
	MakeVersion = "GNU Make 4.4.1"
)

func RequireTool(t *testing.T, name, want string) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), name, "--version").Output()
	if err != nil {
		t.Fatalf("-update needs %s on PATH as the reference oracle: %v", name, err)
	}
	got, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if got != want {
		t.Fatalf("the golden files are recorded with %s, but %s is installed; run -update from the nix dev shell", want, got)
	}
}

var justLocation = regexp.MustCompile(`(?m)^\s*——▶ [^:]+:(\d+):(\d+)`)

func JustVerdict(t *testing.T, text string) Verdict {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "justfile")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "just", "--justfile", path, "--working-directory", dir, "--dump") //nolint:gosec // G204: the reference oracle reads a file in the test's own temporary directory
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		return Verdict{Accepted: true}
	}
	out := stderr.String()
	message, _, _ := strings.Cut(strings.TrimPrefix(out, "error: "), "\n")
	v := Verdict{Message: message}
	if m := justLocation.FindStringSubmatch(out); m != nil {
		v.Line, _ = strconv.Atoi(m[1])
		v.Column, _ = strconv.Atoi(m[2])
	}
	return v
}

var makeLocation = regexp.MustCompile(`(?m)^Makefile:(\d+): (?:\*\*\* )?(.*?)(?:\.  Stop\.)?$`)

func MakeVerdict(t *testing.T, text string) Verdict {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "make", "--dry-run", "--no-print-directory", "-f", "Makefile")
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		return Verdict{Accepted: true}
	}
	out := stderr.String()
	for _, m := range makeLocation.FindAllStringSubmatch(out, -1) {
		if strings.HasPrefix(m[2], "warning:") {
			continue
		}
		line, _ := strconv.Atoi(m[1])
		return Verdict{Line: line, Column: 1, Message: m[2]}
	}
	message, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	return Verdict{Message: message}
}
