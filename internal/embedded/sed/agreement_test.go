package sed_test

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/embedded/sed"
	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
	"github.com/stubbedev/shell-lsp/internal/host/oracle"
)

var update = flag.Bool("update", false, "record GNU sed's verdict on every agreement case")

const gnuSedVersion = "sed (GNU sed) 4.10"

var undetected = map[string]bool{
	"bre: a newer sed version":              true,
	"bre: a version that is not a number":   true,
	"bre: a multi-character collating name": true,
}

var gnuLocation = regexp.MustCompile(`^sed: -e expression #1, char (\d+): (.*)$`)

func gnuVerdict(t *testing.T, script string, syntax sed.Syntax) oracle.Verdict {
	t.Helper()
	args := []string{"-n"}
	if syntax == sed.Extended {
		args = append(args, "-E")
	}
	cmd := exec.CommandContext(t.Context(), "sed", append(args, "-e", script)...)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"LC_ALL=C"}
	cmd.Stdin = strings.NewReader("")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return oracle.Verdict{Accepted: true}
	}
	if code := cmd.ProcessState.ExitCode(); code != 1 && code != 4 {
		t.Fatalf("running sed on %q: %v: %s", script, err, stderr.String())
	}
	message, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
	m := gnuLocation.FindStringSubmatch(message)
	if m == nil {
		return oracle.Verdict{Message: strings.TrimPrefix(message, "sed: ")}
	}
	char, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("reading the position in %q: %v", message, err)
	}
	v := oracle.Verdict{Message: m[2]}
	if char > 0 {
		v.Line, v.Column = position(script, min(char, len(script))-1)
		v.Column++
	}
	return v
}

func position(text string, offset int) (int, int) {
	before := text[:offset]
	return strings.Count(before, "\n") + 1, offset - strings.LastIndexByte(before, '\n') - 1
}

func syntaxOf(t *testing.T, name string) sed.Syntax {
	t.Helper()
	switch {
	case strings.HasPrefix(name, "bre: "):
		return sed.Basic
	case strings.HasPrefix(name, "ere: "):
		return sed.Extended
	}
	t.Fatalf("agreement case %q names neither bre: nor ere:", name)
	return sed.Basic
}

func TestParseAgreesWithGNUSed(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "agreement.txt")
	cases := hosttest.Sections(t, path)
	goldenPath := strings.TrimSuffix(path, ".txt") + ".golden"
	if *update {
		hosttest.RequireTool(t, "sed", gnuSedVersion)
		var sb strings.Builder
		for _, c := range cases {
			fmt.Fprintf(&sb, "-- %s --\n%s\n", c.Name, gnuVerdict(t, strings.TrimSuffix(c.Text, "\n"), syntaxOf(t, c.Name)))
		}
		if err := os.WriteFile(goldenPath, []byte(sb.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	recorded := map[string]oracle.Verdict{}
	for _, g := range hosttest.Sections(t, goldenPath) {
		v, ok := oracle.Parse(strings.TrimSuffix(g.Text, "\n"))
		if !ok {
			t.Fatalf("%s: section %q holds %q, not a verdict", goldenPath, g.Name, g.Text)
		}
		recorded[g.Name] = v
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			v, ok := recorded[c.Name]
			if !ok {
				t.Fatalf("%s has no section %q; record it with -update", goldenPath, c.Name)
			}
			script := strings.TrimSuffix(c.Text, "\n")
			var errorLines []int
			for _, e := range sed.Parse(script, syntaxOf(t, c.Name)).Errors {
				line, _ := position(script, e.Start)
				errorLines = append(errorLines, line)
			}
			if undetected[c.Name] {
				if v.Accepted || len(errorLines) > 0 {
					t.Errorf("%q is listed as a rejection left undetected, but sed says %s and errors are reported on lines %v", script, v, errorLines)
				}
				return
			}
			lines, _ := position(script, len(script))
			if problem, ok := oracle.Disagreement(v, lines, errorLines); ok {
				t.Errorf("%s: %q", problem, script)
			}
		})
	}
}
