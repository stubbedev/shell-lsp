package lint

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var (
	update  = flag.Bool("update", false, "label unlabeled fixture cases with shellcheck's findings")
	fixture = flag.String("fixture", "*/*.fixture", "glob, relative to testdata/fixtures, of the fixture files to load")
)

const (
	caseHeader     = "### "
	markBoth       = ">>> "
	markOursOnly   = ">>+ "
	markTheirsOnly = ">>- "
	noFindings     = "none"
	reasonSep      = " -- "
	oracleTimeout  = 20 * time.Second
)

type expectation struct {
	finding string
	mark    string
	reason  string
}

type fixtureCase struct {
	file     string
	line     int
	name     string
	script   string
	expected []expectation
	labeled  bool
	end      int
}

func (c fixtureCase) id() string {
	return fmt.Sprintf("%s:%d %s", filepath.Base(c.file), c.line, c.name)
}

func (c fixtureCase) want(marks ...string) []string {
	var out []string
	for _, e := range c.expected {
		if slices.Contains(marks, e.mark) {
			out = append(out, e.finding)
		}
	}
	slices.Sort(out)
	return out
}

func loadFixtures(t *testing.T) []fixtureCase {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "fixtures", *fixture))
	if err != nil {
		t.Fatal(err)
	}
	var out []fixtureCase
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		cases, err := parseFixtures(path, string(raw))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, cases...)
	}
	return out
}

func parseFixtures(path, raw string) ([]fixtureCase, error) {
	var out []fixtureCase
	var current *fixtureCase
	var script []string
	flush := func() {
		if current == nil {
			return
		}
		for len(script) > 0 && strings.TrimSpace(script[len(script)-1]) == "" {
			script = script[:len(script)-1]
		}
		current.script = strings.Join(script, "\n") + "\n"
		current.end = current.line + len(script)
		out = append(out, *current)
		current, script = nil, nil
	}
	for i, line := range strings.Split(strings.TrimSuffix(raw, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, caseHeader):
			flush()
			current = &fixtureCase{file: path, line: i + 1, name: strings.TrimPrefix(line, caseHeader)}
		case current == nil:
			if strings.TrimSpace(line) != "" {
				return nil, fmt.Errorf("%s:%d: text before the first %q header", path, i+1, caseHeader)
			}
		case strings.HasPrefix(line, markBoth), strings.HasPrefix(line, markOursOnly), strings.HasPrefix(line, markTheirsOnly):
			for len(script) > 0 && strings.TrimSpace(script[len(script)-1]) == "" {
				script = script[:len(script)-1]
			}
			mark, body := line[:len(markBoth)], line[len(markBoth):]
			current.labeled = true
			finding, reason, _ := strings.Cut(body, reasonSep)
			finding = strings.TrimSpace(finding)
			if finding == noFindings {
				continue
			}
			if mark != markBoth && reason == "" {
				return nil, fmt.Errorf("%s:%d: a divergence needs a reason after %q", path, i+1, reasonSep)
			}
			current.expected = append(current.expected, expectation{finding: finding, mark: mark, reason: reason})
		default:
			if current.labeled {
				if strings.TrimSpace(line) == "" {
					continue
				}
				return nil, fmt.Errorf("%s:%d: script text after expectations in case %q", path, i+1, current.name)
			}
			script = append(script, line)
		}
	}
	flush()
	return out, nil
}

func fixtureDialect(script string) (dialect.Dialect, bool) {
	_, fromShebang := dialect.FromShebang(script)
	explicit := fromShebang || strings.Contains(script, "shell=")
	return dialect.Detect(nil, dialect.Input{URI: "file:///fixture", Text: script}), explicit
}

func position(text string, offset int) string {
	line := strings.Count(text[:offset], "\n") + 1
	lineStart := strings.LastIndexByte(text[:offset], '\n') + 1
	return fmt.Sprintf("%d:%d", line, utf8.RuneCountInString(text[lineStart:offset])+1)
}

func ours(script string) []string {
	d, explicit := fixtureDialect(script)
	tree := parser.Parse(script, d)
	var out []string
	for _, f := range Check(tree, nil, Options{ForcedDialect: !explicit}) {
		out = append(out, f.Code.String()+" "+position(script, f.Span.Start))
	}
	slices.Sort(out)
	return out
}

func implemented(findings []string) []string {
	var out []string
	for _, f := range findings {
		code, _, _ := strings.Cut(f, " ")
		n, err := ParseCode(code)
		if err != nil {
			continue
		}
		if _, ok := catalog[n]; ok {
			out = append(out, f)
		}
	}
	return out
}

func TestFixtures(t *testing.T) {
	t.Parallel()

	for _, c := range loadFixtures(t) {
		if !c.labeled {
			t.Errorf("%s: unlabeled; run go test ./internal/lint -run Shellcheck -update", c.id())
			continue
		}
		got := implemented(ours(c.script))
		want := implemented(c.want(markBoth, markOursOnly))
		if !slices.Equal(got, want) {
			t.Errorf("%s:\n%s\ngot  %v\nwant %v", c.id(), c.script, got, want)
		}
	}
}

func TestFixturesAgreeWithShellcheck(t *testing.T) {
	if testing.Short() {
		t.Skip("runs shellcheck for every fixture")
	}
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck is not installed; it is only used as a reference oracle")
	}
	cases := loadFixtures(t)
	results := make([][]string, len(cases))
	errs := make([]error, len(cases))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for i, c := range cases {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i], errs[i] = theirs(t.Context(), c.script)
		})
	}
	wg.Wait()
	labels := map[string][]int{}
	for i, c := range cases {
		if errs[i] != nil {
			t.Errorf("%s: %v", c.id(), errs[i])
			continue
		}
		if !c.labeled {
			labels[c.file] = append(labels[c.file], i)
			continue
		}
		if want := c.want(markBoth, markTheirsOnly); !slices.Equal(results[i], want) {
			t.Errorf("%s:\n%s\nshellcheck %v\nfixture    %v", c.id(), c.script, results[i], want)
		}
	}
	if len(labels) == 0 {
		return
	}
	if !*update {
		t.Errorf("%d fixture files have unlabeled cases; rerun with -update", len(labels))
		return
	}
	for file, indexes := range labels {
		if err := label(file, cases, results, indexes); err != nil {
			t.Fatal(err)
		}
	}
}

func theirs(ctx context.Context, script string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, oracleTimeout)
	defer cancel()
	args := []string{"--format=json1", "--norc"}
	if _, explicit := fixtureDialect(script); !explicit {
		args = append(args, "--shell=bash")
	}
	cmd := exec.CommandContext(ctx, "shellcheck", append(args, "-")...)
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); !ok || exitErr.ExitCode() != 1 {
			return nil, fmt.Errorf("running shellcheck: %w: %s", err, stderr.String())
		}
	}
	var parsed struct {
		Comments []struct {
			Code   int `json:"code"`
			Line   int `json:"line"`
			Column int `json:"column"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return nil, fmt.Errorf("decoding shellcheck output %q: %w", stdout.String(), err)
	}
	out := make([]string, 0, len(parsed.Comments))
	for _, c := range parsed.Comments {
		out = append(out, "SC"+strconv.Itoa(c.Code)+" "+strconv.Itoa(c.Line)+":"+strconv.Itoa(c.Column))
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

func label(file string, cases []fixtureCase, results [][]string, indexes []int) error {
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	insertAfter := map[int][]string{}
	for _, i := range indexes {
		c := cases[i]
		labels := []string{markBoth + noFindings}
		if len(results[i]) > 0 {
			labels = labels[:0]
			for _, f := range results[i] {
				labels = append(labels, markBoth+f)
			}
		}
		insertAfter[c.end] = labels
	}
	var out strings.Builder
	for i, line := range lines {
		out.WriteString(line)
		out.WriteByte('\n')
		for _, l := range insertAfter[i+1] {
			out.WriteString(l)
			out.WriteByte('\n')
		}
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, []byte(out.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
