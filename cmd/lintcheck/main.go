package main

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const (
	maxFileSize       = 1 << 20
	shellcheckTimeout = 60 * time.Second
)

type finding struct {
	code      lint.Code
	line, col int
}

type fileResult struct {
	path   string
	text   string
	ours   []finding
	theirs []finding
	err    error
}

type tally struct {
	agree, oursOnly, theirsOnly int
	examples                    []string
}

func main() {
	examples := flag.Int("examples", 5, "mismatches to print per code")
	only := flag.String("code", "", "only report this code, e.g. SC2086")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: lintcheck [-examples N] [-code SCxxxx] PATH...")
		os.Exit(2)
	}
	files, err := collect(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	results := run(context.Background(), files)
	report(results, *examples, *only)
}

func collect(roots []string) ([]string, error) {
	var out []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrPermission) {
					return nil
				}
				return err
			}
			if !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.Size() > maxFileSize || info.Size() == 0 {
				return nil
			}
			if isShellScript(path) {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walking %s: %w", root, err)
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

func isShellScript(path string) bool {
	switch filepath.Ext(path) {
	case ".sh", ".bash", ".ksh", ".dash":
		return true
	}
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return false
	}
	defer f.Close()
	first, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && first == "" {
		return false
	}
	d, ok := dialect.FromShebang(first)
	return ok && d != dialect.Zsh
}

func shellName(d dialect.Dialect) (string, bool) {
	switch d {
	case dialect.Bash, dialect.Dash, dialect.Ksh, dialect.Busybox:
		return d.String(), true
	case dialect.Posix:
		return "sh", true
	}
	return "", false
}

func run(ctx context.Context, files []string) []fileResult {
	results := make([]fileResult, len(files))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for i, path := range files {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = analyze(ctx, path)
		})
	}
	wg.Wait()
	return results
}

func analyze(ctx context.Context, path string) fileResult {
	raw, err := os.ReadFile(path) //nolint:gosec
	if err != nil || !utf8.Valid(raw) || bytes.ContainsRune(raw, '\r') {
		return fileResult{path: path, err: errors.New("unreadable, not UTF-8 or CRLF")}
	}
	text := string(raw)
	d := dialect.Detect(nil, dialect.Input{URI: fileuri.FromPath(path), Text: text})
	shell, ok := shellName(d)
	if !ok {
		return fileResult{path: path, err: errors.New("dialect not supported by shellcheck")}
	}
	theirs, err := shellcheck(ctx, shell, text)
	if err != nil {
		return fileResult{path: path, err: err}
	}
	tree := parser.Parse(text, d)
	var ours []finding
	for _, f := range lint.Check(tree, nil, lint.Options{DialectSource: lint.DialectDetected}) {
		line, col := position(text, f.Span.Start)
		ours = append(ours, finding{code: f.Code, line: line, col: col})
	}
	return fileResult{path: path, text: text, ours: ours, theirs: theirs}
}

func position(text string, offset int) (int, int) {
	line := strings.Count(text[:offset], "\n") + 1
	start := strings.LastIndexByte(text[:offset], '\n') + 1
	return line, utf8.RuneCountInString(text[start:offset]) + 1
}

func shellcheck(ctx context.Context, shell, text string) ([]finding, error) {
	ctx, cancel := context.WithTimeout(ctx, shellcheckTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "shellcheck", "--format=json1", "--norc", "--shell="+shell, "-") //nolint:gosec
	cmd.Stdin = strings.NewReader(text)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); !ok || exitErr.ExitCode() != 1 {
			return nil, fmt.Errorf("shellcheck: %w: %s", err, stderr.String())
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

func report(results []fileResult, examples int, only string) {
	implemented := slices.DeleteFunc(lint.Codes(), lint.Code.Native)
	tallies := map[lint.Code]*tally{}
	for _, code := range implemented {
		tallies[code] = &tally{}
	}
	analyzed, skipped := 0, 0
	for _, r := range results {
		if r.err != nil {
			skipped++
			continue
		}
		analyzed++
		ours := set(r.ours, implemented)
		theirs := set(r.theirs, implemented)
		for f := range ours {
			t := tallies[f.code]
			if theirs[f] {
				t.agree++
				continue
			}
			t.oursOnly++
			t.example(examples, "+", r, f)
		}
		for f := range theirs {
			if !ours[f] {
				t := tallies[f.code]
				t.theirsOnly++
				t.example(examples, "-", r, f)
			}
		}
	}
	fmt.Printf("%d files analyzed, %d skipped\n\n", analyzed, skipped)
	fmt.Println("| Code | Agree | Ours only | Shellcheck only | Precision | Recall |")
	fmt.Println("| --- | --- | --- | --- | --- | --- |")
	for _, code := range implemented {
		if only != "" && code.String() != only {
			continue
		}
		t := tallies[code]
		fmt.Printf("| %s | %d | %d | %d | %s | %s |\n", code, t.agree, t.oursOnly, t.theirsOnly, ratio(t.agree, t.agree+t.oursOnly), ratio(t.agree, t.agree+t.theirsOnly))
	}
	for _, code := range implemented {
		if only != "" && code.String() != only {
			continue
		}
		t := tallies[code]
		if len(t.examples) == 0 {
			continue
		}
		fmt.Printf("\n%s\n", code)
		slices.SortFunc(t.examples, cmp.Compare)
		for _, e := range t.examples {
			fmt.Println(e)
		}
	}
}

func (t *tally) example(limit int, sign string, r fileResult, f finding) {
	shown := 0
	for _, e := range t.examples {
		if strings.HasPrefix(e, "  "+sign) {
			shown++
		}
	}
	if shown >= limit {
		return
	}
	lines := strings.Split(r.text, "\n")
	source := ""
	if f.line-1 < len(lines) {
		source = strings.TrimSpace(lines[f.line-1])
	}
	t.examples = append(t.examples, fmt.Sprintf("  %s %s:%d:%d  %s", sign, r.path, f.line, f.col, source))
}

func set(findings []finding, implemented []lint.Code) map[finding]bool {
	out := map[finding]bool{}
	for _, f := range findings {
		if slices.Contains(implemented, f.code) {
			out[f] = true
		}
	}
	return out
}

func ratio(n, d int) string {
	if d == 0 {
		return "-"
	}
	return strconv.FormatFloat(100*float64(n)/float64(d), 'f', 1, 64) + "%"
}
