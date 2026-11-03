package main

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/host/nix"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parallel"
	"github.com/stubbedev/shell-lsp/internal/regularfile"
	"github.com/stubbedev/shell-lsp/internal/statixconfig"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const (
	nixLintBaselineFile = "corpus/nix-lint.md"
	defaultNixSample    = 2500
	nixFileSuffix       = ".nix"
	statixErrorSeverity = "Error"
	rowUnusedLet        = "deadnix unused let"
	rowUnusedArgument   = "deadnix unused argument"
	rowUnusedPattern    = "deadnix unused pattern"
)

const (
	statixCollapsibleLet = 6
	statixRepeatedKeys   = 20
)

type nixPlace struct {
	line, column int
}

type nixLintKey struct {
	place nixPlace
	row   string
}

type nixLintRow struct {
	agree, ours, theirs int
}

type nixLintDifference struct {
	path  string
	place nixPlace
	row   string
	side  string
}

func (d nixLintDifference) String() string {
	return fmt.Sprintf("%s:%d:%d %s %s", d.path, d.place.line, d.place.column, d.row, d.side)
}

type nixLintResult struct {
	files, skipped int
	rows           map[string]*nixLintRow
	differences    []nixLintDifference
	brokenFixes    []string
	alone          map[lint.Code]int
	alonePlaces    []nixAlone
}

func runNixLint(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("nixlint", flag.ContinueOnError)
	nixpkgs := fs.String("nixpkgs", "", "nixpkgs checkout to sample; defaults to the one flake.lock pins")
	sample := fs.Int("sample", defaultNixSample, "number of nixpkgs files to compare")
	baselineFile := fs.String("baseline", nixLintBaselineFile, "baseline to compare with or rewrite")
	update := fs.Bool("update", false, "rewrite the baseline instead of comparing with it")
	examples := fs.Int("examples", 0, "print this many differences for each lint")
	skip := fs.String("skip", "", "comma-separated directory names to leave out of PATHs")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if err := requireNixLintTools(ctx); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		var paths []nixLintFile
		for _, root := range fs.Args() {
			found, err := nixFilesUnder(root, strings.Split(*skip, ","))
			if err != nil {
				return err
			}
			paths = append(paths, found...)
		}
		result, err := compareNixLint(ctx, paths)
		if err != nil {
			return err
		}
		fmt.Print(renderNixLint(result, *examples))
		return nil
	}
	root := *nixpkgs
	if root == "" {
		pinned, err := pinnedNixpkgs(ctx)
		if err != nil {
			return err
		}
		root = pinned
	}
	found, err := nixFilesUnder(root, nil)
	if err != nil {
		return err
	}
	result, err := compareNixLint(ctx, sampleNixFiles(found, *sample))
	if err != nil {
		return err
	}
	return checkBaseline(textBaseline{
		file:    *baselineFile,
		got:     renderNixLint(result, -1),
		update:  *update,
		name:    "Nix lint",
		matches: fmt.Sprintf("%d Nix files match", result.files),
		stale:   "the comparison with statix and deadnix no longer matches",
		rewrite: "just corpus-nix-lint-baseline",
	})
}

func requireNixLintTools(ctx context.Context) error {
	for _, tool := range [][]string{{"statix", "list"}, {"deadnix", "--version"}} {
		if err := exec.CommandContext(ctx, tool[0], tool[1:]...).Run(); err != nil { //nolint:gosec
			return fmt.Errorf("running %s: %w; use `nix develop`", tool[0], err)
		}
	}
	return nil
}

func pinnedNixpkgs(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "nix", "eval", "--raw", "--no-warn-dirty", "--inputs-from", ".", "nixpkgs#path").Output()
	if err != nil {
		return "", fmt.Errorf("finding the nixpkgs flake.lock pins: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

type nixLintFile struct {
	path string
	rel  string
}

func nixFilesUnder(root string, skip []string) ([]nixLintFile, error) {
	var out []nixLintFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && slices.Contains(skip, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(path, nixFileSuffix) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("naming %s under %s: %w", path, root, err)
		}
		out = append(out, nixLintFile{path: path, rel: filepath.ToSlash(rel)})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing Nix files under %s: %w", root, err)
	}
	return out, nil
}

func sampleNixFiles(files []nixLintFile, n int) []nixLintFile {
	type ranked struct {
		rank string
		file nixLintFile
	}
	all := make([]ranked, len(files))
	for i, f := range files {
		sum := sha256.Sum256([]byte(f.rel))
		all[i] = ranked{rank: hex.EncodeToString(sum[:]), file: f}
	}
	slices.SortFunc(all, func(a, b ranked) int { return cmp.Compare(a.rank, b.rank) })
	out := make([]nixLintFile, 0, min(n, len(all)))
	for _, r := range all[:min(n, len(all))] {
		out = append(out, r.file)
	}
	slices.SortFunc(out, func(a, b nixLintFile) int { return cmp.Compare(a.rel, b.rel) })
	return out
}

type nixFileComparison struct {
	ours, theirs map[nixLintKey]bool
	skipped      bool
	broken       []string
	alone        []nixAlone
	err          error
}

func compareNixLint(ctx context.Context, files []nixLintFile) (nixLintResult, error) {
	configs := statixconfig.NewResolver(regularfile.ReadConfig, time.Now, time.Hour)
	results := make([]nixFileComparison, len(files))
	parallel.For(referenceWorkers(), len(files), func(i int) {
		results[i] = compareNixFile(ctx, configs, files[i])
	})
	result := nixLintResult{rows: map[string]*nixLintRow{}, alone: map[lint.Code]int{}}
	var errs []error
	for _, l := range lint.StatixLints() {
		result.rows[l.Name] = &nixLintRow{}
	}
	for _, row := range []string{rowUnusedLet, rowUnusedArgument, rowUnusedPattern} {
		result.rows[row] = &nixLintRow{}
	}
	for i, r := range results {
		switch {
		case r.err != nil:
			errs = append(errs, fmt.Errorf("%s: %w", files[i].rel, r.err))
			continue
		case r.skipped:
			result.skipped++
			continue
		}
		result.files++
		result.add(files[i].rel, r)
		for _, a := range r.alone {
			result.alone[a.code]++
			a.path = files[i].rel
			result.alonePlaces = append(result.alonePlaces, a)
		}
		for _, b := range r.broken {
			result.brokenFixes = append(result.brokenFixes, files[i].rel+":"+b)
		}
	}
	slices.SortFunc(result.differences, func(a, b nixLintDifference) int {
		return cmp.Or(cmp.Compare(a.path, b.path), cmp.Compare(a.place.line, b.place.line), cmp.Compare(a.place.column, b.place.column), cmp.Compare(a.row, b.row), cmp.Compare(a.side, b.side))
	})
	return result, errors.Join(errs...)
}

func (r *nixLintResult) add(path string, c nixFileComparison) {
	for key := range c.theirs {
		row := r.rows[key.row]
		if c.ours[key] {
			row.agree++
			continue
		}
		row.theirs++
		r.differences = append(r.differences, nixLintDifference{path: path, place: key.place, row: key.row, side: "theirs"})
	}
	for key := range c.ours {
		if c.theirs[key] {
			continue
		}
		r.rows[key.row].ours++
		r.differences = append(r.differences, nixLintDifference{path: path, place: key.place, row: key.row, side: "ours"})
	}
}

func compareNixFile(ctx context.Context, configs *statixconfig.Resolver, f nixLintFile) nixFileComparison {
	raw, err := os.ReadFile(f.path)
	if err != nil {
		return nixFileComparison{err: err}
	}
	text := string(raw)
	if !utf8.ValidString(text) {
		return nixFileComparison{skipped: true}
	}
	analysis := nix.Analyze(text)
	if len(analysis.Errors) > 0 {
		return nixFileComparison{skipped: true}
	}
	disabled, err := configs.ExcludedFor(f.path)
	if err != nil {
		return nixFileComparison{err: err}
	}
	theirs, ok, err := runStatix(ctx, f.path)
	if err != nil {
		return nixFileComparison{err: err}
	}
	if !ok {
		return nixFileComparison{skipped: true}
	}
	dead, ok, err := runDeadnix(ctx, f.path)
	if err != nil {
		return nixFileComparison{err: err}
	}
	if !ok {
		return nixFileComparison{skipped: true}
	}
	merged := nixBinderPlaces(text, analysis)
	for key := range dead {
		key.place = merged.canonical(key.place)
		theirs[key] = true
	}
	return nixFileComparison{ours: ourNixLints(text, analysis, disabled), theirs: theirs, broken: brokenNixFixes(text, analysis), alone: nixChecksAlone(text, analysis)}
}

type statixOutput struct {
	Report []struct {
		Code        int    `json:"code"`
		Severity    string `json:"severity"`
		Diagnostics []struct {
			At struct {
				From struct {
					Line   int `json:"line"`
					Column int `json:"column"`
				} `json:"from"`
			} `json:"at"`
		} `json:"diagnostics"`
	} `json:"report"`
}

func runStatix(ctx context.Context, path string) (map[nixLintKey]bool, bool, error) {
	cmd := exec.CommandContext(ctx, "statix", "check", "-o", "json", "-c", filepath.Dir(path), path) //nolint:gosec
	cmd.Dir = os.TempDir()
	cmd.Env = isolatedEnv()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil && stdout.Len() == 0 {
		if _, exited := errors.AsType[*exec.ExitError](err); !exited {
			return nil, false, fmt.Errorf("running statix: %w", err)
		}
	}
	out := map[nixLintKey]bool{}
	if stdout.Len() == 0 {
		return out, true, nil
	}
	var parsed statixOutput
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return nil, false, fmt.Errorf("reading statix's report: %w", err)
	}
	names := statixNames()
	for _, report := range parsed.Report {
		if report.Severity == statixErrorSeverity || report.Code == 0 {
			return nil, false, nil
		}
		if len(report.Diagnostics) == 0 {
			continue
		}
		at := report.Diagnostics[0]
		if report.Code == statixCollapsibleLet {
			at = report.Diagnostics[len(report.Diagnostics)-1]
		}
		name, known := names[report.Code]
		if !known {
			return nil, false, fmt.Errorf("statix reports W%02d, which no lint name is mapped to", report.Code)
		}
		out[nixLintKey{place: nixPlace{at.At.From.Line, at.At.From.Column}, row: name}] = true
	}
	return out, true, nil
}

func statixNames() map[int]string {
	out := map[int]string{}
	for _, l := range lint.StatixLints() {
		out[l.ID] = l.Name
	}
	return out
}

type deadnixOutput struct {
	Results []struct {
		Message string `json:"message"`
		Line    *int   `json:"line"`
		Column  *int   `json:"column"`
	} `json:"results"`
}

func runDeadnix(ctx context.Context, path string) (map[nixLintKey]bool, bool, error) {
	cmd := exec.CommandContext(ctx, "deadnix", "-o", "json", path) //nolint:gosec
	cmd.Dir = os.TempDir()
	cmd.Env = isolatedEnv()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		if _, exited := errors.AsType[*exec.ExitError](err); !exited {
			return nil, false, fmt.Errorf("running deadnix: %w", err)
		}
	}
	out := map[nixLintKey]bool{}
	for line := range bytes.SplitSeq(stdout.Bytes(), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var parsed deadnixOutput
		if err := json.Unmarshal(line, &parsed); err != nil {
			return nil, false, fmt.Errorf("reading deadnix's report: %w", err)
		}
		for _, r := range parsed.Results {
			if r.Line == nil || r.Column == nil {
				return nil, false, nil
			}
			out[nixLintKey{place: nixPlace{*r.Line, *r.Column}, row: deadnixRow(r.Message)}] = true
		}
	}
	return out, true, nil
}

func deadnixRow(message string) string {
	switch {
	case strings.HasPrefix(message, "Unused let binding"):
		return rowUnusedLet
	case strings.HasPrefix(message, "Unused lambda argument"):
		return rowUnusedArgument
	}
	return rowUnusedPattern
}

type binderPlaces map[nixPlace]nixPlace

func (b binderPlaces) canonical(p nixPlace) nixPlace {
	if first, merged := b[p]; merged {
		return first
	}
	return p
}

func nixBinderPlaces(text string, analysis nix.Analysis) binderPlaces {
	out := binderPlaces{}
	for _, d := range analysis.Scope.Defs() {
		if len(d.Binders) < 2 {
			continue
		}
		first := placeOf(text, d.Binders[0].Span().Start)
		for _, b := range d.Binders[1:] {
			out[placeOf(text, b.Span().Start)] = first
		}
	}
	return out
}

func placeOf(text string, offset int) nixPlace {
	line, _ := lineColumn(text, offset)
	start := strings.LastIndexByte(text[:offset], '\n') + 1
	return nixPlace{line: line, column: utf8.RuneCountInString(text[start:offset]) + 1}
}

func ourNixLints(text string, analysis nix.Analysis, disabled []lint.Code) map[nixLintKey]bool {
	rows := map[lint.Code]string{}
	for _, l := range lint.StatixLints() {
		rows[l.Code] = l.Name
	}
	rows[lint.CodeNixUnusedLet] = rowUnusedLet
	rows[lint.CodeNixUnusedArgument] = rowUnusedArgument
	rows[lint.CodeNixUnusedFormal] = rowUnusedPattern
	findings := slices.Concat(analysis.Index.Findings, nix.OptionalLintFindings(text, lint.CodesOf(nix.OptionalLintCodes()...)))
	kinds := map[nixPlace]scope.DefKind{}
	for _, d := range analysis.Scope.Defs() {
		kinds[placeOf(text, d.Binders[0].Span().Start)] = d.Kind
	}
	out := map[nixLintKey]bool{}
	for _, f := range findings {
		row, shared := rows[f.Code]
		place := placeOf(text, f.Span.Start)
		switch f.Code {
		case lint.CodeNixUnusedUnderscore:
			row, shared = rowUnusedPattern, true
			if kind := kinds[place]; kind == scope.LetDef || kind == scope.InheritDef {
				row = rowUnusedLet
			}
		case lint.CodeNixUnusedArgument:
			if kinds[place] == scope.AliasDef {
				row = rowUnusedPattern
			}
		default:
		}
		if !shared || slices.Contains(disabled, f.Code) {
			continue
		}
		out[nixLintKey{place: place, row: row}] = true
	}
	return out
}

func renderNixLint(r nixLintResult, examples int) string {
	var sb strings.Builder
	sb.WriteString("# Nix lint comparison\n\n")
	sb.WriteString("shell-lsp's Nix checks against statix and deadnix, written by `just corpus-nix-lint-baseline`;\n")
	sb.WriteString("see docs/corpus.md. Positions only, one difference a line.\n\n")
	fmt.Fprintf(&sb, "%d files compared, %d left out because a parser rejected them.\n\n", r.files, r.skipped)
	sb.WriteString(tableRow("Lint", "Agree", "Ours only", "Theirs only") + "\n" + tableRow("---", "---:", "---:", "---:") + "\n")
	names := make([]string, 0, len(r.rows))
	for name := range r.rows {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		row := r.rows[name]
		sb.WriteString(tableRow(name, strconv.Itoa(row.agree), strconv.Itoa(row.ours), strconv.Itoa(row.theirs)) + "\n")
	}
	sb.WriteString("\n" + tableRow("Check of ours alone", "Findings") + "\n" + tableRow("---", "---:") + "\n")
	codes := slices.Sorted(maps.Keys(r.alone))
	for _, code := range codes {
		sb.WriteString(tableRow(code.String(), strconv.Itoa(r.alone[code])) + "\n")
	}
	fmt.Fprintf(&sb, "\nFixes in files up to 128 KiB that broke the file or changed what a name means: %d.\n", len(r.brokenFixes))
	for _, b := range r.brokenFixes {
		sb.WriteString(b + "\n")
	}
	if examples == 0 {
		return sb.String()
	}
	sb.WriteString("\n## Differences\n\n")
	printed := map[string]int{}
	for _, d := range r.differences {
		key := d.row + d.side
		if examples > 0 && printed[key] >= examples {
			continue
		}
		printed[key]++
		sb.WriteString(d.String() + "\n")
	}
	if examples > 0 {
		for _, a := range r.alonePlaces {
			if printed[a.code.String()] < examples {
				printed[a.code.String()]++
				fmt.Fprintf(&sb, "%s:%d:%d %s\n", a.path, a.place.line, a.place.column, a.code)
			}
		}
	}
	return sb.String()
}

const (
	maxFixRounds = 20
	maxFixedFile = 128 << 10
)

type nixAlone struct {
	path  string
	place nixPlace
	code  lint.Code
}

func nixChecksAlone(text string, analysis nix.Analysis) []nixAlone {
	findings := slices.Concat(analysis.Index.Findings, nix.OptionalLintFindings(text, lint.CodesOf(nix.OptionalLintCodes()...)))
	var out []nixAlone
	for _, f := range findings {
		if f.Code < lint.CodeNixUsedUnderscore || !f.Code.Nix() {
			continue
		}
		out = append(out, nixAlone{place: placeOf(text, f.Span.Start), code: f.Code})
	}
	return out
}

func nixResolutions(text string) ([]string, bool) {
	tree := syntax.Parse(text)
	if len(tree.Errors) > 0 {
		return nil, false
	}
	table := scope.Resolve(tree)
	out := make([]string, 0, len(table.Uses()))
	for _, u := range table.Uses() {
		out = append(out, fmt.Sprintf("%s/%d/%s", u.Name, u.Ref.Kind, binderClass(u.Ref.Def)))
	}
	slices.Sort(out)
	return slices.Compact(out), true
}

func binderClass(d *scope.Def) string {
	if d == nil {
		return "none"
	}
	switch d.Kind {
	case scope.ArgDef, scope.FormalDef, scope.AliasDef:
		return "argument"
	case scope.LetDef, scope.RecDef, scope.InheritDef:
	}
	return "binding"
}

func brokenNixFixes(text string, analysis nix.Analysis) []string {
	if len(text) > maxFixedFile {
		return nil
	}
	var out []string
	before, ok := nixResolutions(text)
	if !ok {
		return nil
	}
	findings := slices.Concat(analysis.Index.Findings, nix.OptionalLintFindings(text, lint.CodesOf(nix.OptionalLintCodes()...)))
	for _, f := range sortedFindings(findings) {
		if !f.Code.Nix() {
			continue
		}
		for _, fix := range f.Fixes {
			after, parsed := nixResolutions(textedit.Apply(text, fix.Edits))
			place := placeOf(text, f.Span.Start)
			switch {
			case !parsed:
				out = append(out, fmt.Sprintf("%d:%d %s fix does not parse", place.line, place.column, f.Code))
			case !subsetOf(after, before) && !introducesLib(f.Code):
				out = append(out, fmt.Sprintf("%d:%d %s fix changes a name", place.line, place.column, f.Code))
			}
		}
	}
	return append(out, fixAllProblems(text)...)
}

func introducesLib(code lint.Code) bool {
	return code == lint.CodeNixPkgsLib || code == lint.CodeNixModuleRecursion
}

func subsetOf(names, of []string) bool {
	for _, name := range names {
		if _, found := slices.BinarySearch(of, name); !found {
			return false
		}
	}
	return true
}

func fixAllProblems(text string) []string {
	current := text
	for range maxFixRounds {
		edits := lint.FindingsSafeEdits(nix.Analyze(current).Index.Findings)
		if len(edits) == 0 {
			return nil
		}
		next := textedit.Apply(current, edits)
		if _, parsed := nixResolutions(next); !parsed {
			return []string{"fix-all does not parse"}
		}
		current = next
	}
	return []string{"fix-all does not settle"}
}
