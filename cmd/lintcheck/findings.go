package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/shell-lsp/internal/corpus"
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parallel"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const shellcheckTimeout = 5 * time.Minute

var errShellcheckCrashed = errors.New("shellcheck crashed")

type finding struct {
	code      lint.Code
	line, col int
}

type side uint8

const (
	oursOnly side = iota
	theirsOnly
)

func (s side) sign() string {
	if s == oursOnly {
		return "+"
	}
	return "-"
}

type mismatch struct {
	side side
	path string
	finding
	source string
}

type tally struct {
	agree, oursOnly, theirsOnly int
}

type comparison struct {
	compared, skipped int
	crashed           []string
	tallies           map[lint.Code]tally
	mismatches        []mismatch
}

type outcome uint8

const (
	unsupported outcome = iota
	checked
	crashed
)

type checkResult struct {
	outcome      outcome
	ours, theirs []finding
	err          error
}

func implementedCodes() []lint.Code {
	return slices.DeleteFunc(lint.Codes(), lint.Code.Native)
}

func shellcheckShell(d dialect.Dialect) (string, bool) {
	switch d {
	case dialect.Bash, dialect.Dash, dialect.Ksh, dialect.Busybox:
		return d.String(), true
	case dialect.Posix:
		return "sh", true
	case dialect.Zsh:
	}
	return "", false
}

func largestFirst(scripts []corpus.Script) []int {
	order := make([]int, len(scripts))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return len(scripts[b].Text) - len(scripts[a].Text) })
	return order
}

func compareWithShellcheck(ctx context.Context, listing corpus.Listing) (comparison, error) {
	implemented := implementedCodes()
	known := map[lint.Code]bool{}
	for _, code := range implemented {
		known[code] = true
	}
	results := make([]checkResult, len(listing.Scripts))
	order := largestFirst(listing.Scripts)
	parallel.For(runtime.NumCPU(), len(order), func(n int) {
		i := order[n]
		results[i] = check(ctx, listing.Scripts[i])
	})
	c := comparison{skipped: listing.Skipped, tallies: map[lint.Code]tally{}}
	for _, code := range implemented {
		c.tallies[code] = tally{}
	}
	var errs []error
	for i, r := range results {
		s := listing.Scripts[i]
		switch r.outcome {
		case unsupported:
		case crashed:
			c.crashed = append(c.crashed, s.Path)
		case checked:
			c.compared++
			c.add(s, keep(r.ours, known), keep(r.theirs, known))
		}
		if r.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Path, r.err))
		}
	}
	slices.SortFunc(c.mismatches, cmpMismatch)
	return c, errors.Join(errs...)
}

func check(ctx context.Context, s corpus.Script) checkResult {
	shell, ok := shellcheckShell(s.Dialect)
	if !ok {
		return checkResult{outcome: unsupported}
	}
	theirs, err := shellcheck(ctx, shell, s.Text)
	if errors.Is(err, errShellcheckCrashed) {
		return checkResult{outcome: crashed}
	}
	if err != nil {
		return checkResult{err: err}
	}
	return checkResult{outcome: checked, ours: ours(s), theirs: theirs}
}

func cmpMismatch(a, b mismatch) int {
	if a.code != b.code {
		return int(a.code) - int(b.code)
	}
	if a.side != b.side {
		return int(a.side) - int(b.side)
	}
	if n := strings.Compare(a.path, b.path); n != 0 {
		return n
	}
	if a.line != b.line {
		return a.line - b.line
	}
	return a.col - b.col
}

func (c *comparison) add(s corpus.Script, ours, theirs map[finding]bool) {
	lines := strings.Split(s.Text, "\n")
	record := func(sd side, f finding) {
		source := ""
		if f.line >= 1 && f.line <= len(lines) {
			source = strings.TrimSpace(lines[f.line-1])
		}
		c.mismatches = append(c.mismatches, mismatch{side: sd, path: s.Path, finding: f, source: source})
	}
	for f := range ours {
		t := c.tallies[f.code]
		if theirs[f] {
			t.agree++
		} else {
			t.oursOnly++
			record(oursOnly, f)
		}
		c.tallies[f.code] = t
	}
	for f := range theirs {
		if ours[f] {
			continue
		}
		t := c.tallies[f.code]
		t.theirsOnly++
		c.tallies[f.code] = t
		record(theirsOnly, f)
	}
}

func keep(findings []finding, known map[lint.Code]bool) map[finding]bool {
	out := map[finding]bool{}
	for _, f := range findings {
		if known[f.code] {
			out[f] = true
		}
	}
	return out
}

func ours(s corpus.Script) []finding {
	tree := parser.Parse(s.Text, s.Dialect)
	at := docstore.NewDocument("", "", 0, s.Text).Positions(docstore.EncodingUTF32)
	var out []finding
	for _, f := range lint.Check(tree, nil, lint.Options{DialectSource: lint.DialectDetected}) {
		p := at.Position(f.Span.Start)
		out = append(out, finding{code: f.Code, line: int(p.Line) + 1, col: int(p.Character) + 1})
	}
	return out
}

func shellcheck(ctx context.Context, shell, text string) ([]finding, error) {
	ctx, cancel := context.WithTimeout(ctx, shellcheckTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "shellcheck", "--format=json1", "--norc", "--shell="+shell, "-") //nolint:gosec
	cmd.Stdin = strings.NewReader(text)
	cmd.Env = isolatedEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); !ok || exitErr.ExitCode() != 1 {
			return nil, fmt.Errorf("shellcheck --shell=%s: %w: %s", shell, err, strings.TrimSpace(stderr.String()))
		}
		if stdout.Len() == 0 && strings.HasPrefix(stderr.String(), "shellcheck: ") {
			return nil, fmt.Errorf("%w: %s", errShellcheckCrashed, strings.TrimSpace(stderr.String()))
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
		return nil, fmt.Errorf("decoding shellcheck output: %w", err)
	}
	out := make([]finding, 0, len(parsed.Comments))
	for _, c := range parsed.Comments {
		out = append(out, finding{code: lint.Code(c.Code), line: c.Line, col: c.Column}) //nolint:gosec
	}
	return out, nil
}
