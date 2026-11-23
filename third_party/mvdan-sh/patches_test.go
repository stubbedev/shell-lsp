package mvdansh_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/referencetest"
)

const (
	upstreamFile = "UPSTREAM"
	patchesDir   = "patches"
	guardFile    = "patches_test.go"
	readmeFile   = "README.md"
)

var (
	vendoredRoots   = []string{"LICENSE", "syntax", "expand", "pattern", "fileutil", "internal"}
	unvendoredTrees = []string{"syntax/typedjson"}
)

type tree map[string][]byte

func TestPatchesReproduceFork(t *testing.T) {
	upstreamModule, upstreamVersion := upstream(t)
	pristineDir := filepath.Join(goOutput(t, "env", "GOMODCACHE"), filepath.FromSlash(upstreamModule)+"@"+upstreamVersion)
	pristine, err := readTree(pristineDir, isVendored)
	if errors.Is(err, fs.ErrNotExist) {
		referencetest.Unavailable(t, fmt.Sprintf("pristine %s@%s is not in the module cache; run go mod download %[1]s@%[2]s", upstreamModule, upstreamVersion))
	}
	if err != nil {
		t.Fatal(err)
	}
	patches, err := readPatches(patchesDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range patches {
		if err := p.apply(pristine); err != nil {
			t.Fatal(err)
		}
	}
	rewriteImports(pristine, upstreamModule+"/", goOutput(t, "list", "-f", "{{.ImportPath}}", ".")+"/")

	fork, err := readTree(".", isForkCopy)
	if err != nil {
		t.Fatal(err)
	}
	if diffs := differingFiles(pristine, fork); len(diffs) > 0 {
		t.Fatalf("%s@%s with %s/ applied differs from the fork in:\n\t%s\nevery change to the fork belongs in a new patch",
			upstreamModule, upstreamVersion, patchesDir, strings.Join(diffs, "\n\t"))
	}
}

func upstream(t *testing.T) (string, string) {
	t.Helper()
	raw, err := os.ReadFile(upstreamFile)
	if err != nil {
		t.Fatal(err)
	}
	module, version, ok := strings.Cut(strings.TrimSpace(string(raw)), "@")
	if !ok || module == "" || version == "" {
		t.Fatalf("%s holds %q, want module@version", upstreamFile, raw)
	}
	return module, version
}

func goOutput(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("go", args...).Output()
	if err != nil {
		t.Fatalf("go %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

func isVendored(rel string) bool {
	if slices.ContainsFunc(unvendoredTrees, func(dir string) bool { return within(rel, dir) }) {
		return false
	}
	return slices.ContainsFunc(vendoredRoots, func(root string) bool { return within(rel, root) })
}

func isForkCopy(rel string) bool {
	return rel != readmeFile && rel != guardFile && rel != upstreamFile && !within(rel, patchesDir)
}

func within(rel, dir string) bool {
	return rel == dir || strings.HasPrefix(rel, dir+"/")
}

func readTree(root string, keep func(rel string) bool) (tree, error) {
	files := tree{}
	err := fs.WalkDir(os.DirFS(root), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if !keep(rel) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		files[rel] = data
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", root, err)
	}
	return files, nil
}

func rewriteImports(files tree, from, to string) {
	for name, data := range files {
		files[name] = bytes.ReplaceAll(data, []byte(from), []byte(to))
	}
}

func differingFiles(want, got tree) []string {
	var diffs []string
	for name, data := range want {
		other, ok := got[name]
		switch {
		case !ok:
			diffs = append(diffs, name+" (missing from the fork)")
		case !bytes.Equal(data, other):
			diffs = append(diffs, name)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			diffs = append(diffs, name+" (in no patch)")
		}
	}
	slices.Sort(diffs)
	return diffs
}

type patch struct {
	name  string
	files []filePatch
}

type filePatch struct {
	oldPath, newPath string
	hunks            []hunk
}

type hunk struct {
	header             string
	oldStart           int
	oldLines, newLines []string
}

func readPatches(dir string) ([]patch, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("listing patches: %w", err)
	}
	var patches []patch
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".patch" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading patch: %w", err)
		}
		p, err := parsePatch(e.Name(), string(data))
		if err != nil {
			return nil, err
		}
		patches = append(patches, p)
	}
	return patches, nil
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

const devNull = "/dev/null"

func parsePatch(name, text string) (patch, error) {
	p := patch{name: name}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	i := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "diff --git ") })
	if i < 0 {
		return p, fmt.Errorf("patch %s: no diff --git section", name)
	}
	for i < len(lines) {
		fp, next, err := parseFilePatch(lines, i)
		if err != nil {
			return p, fmt.Errorf("patch %s, line %d: %w", name, next+1, err)
		}
		p.files = append(p.files, fp)
		i = next
	}
	return p, nil
}

func parseFilePatch(lines []string, i int) (filePatch, int, error) {
	var fp filePatch
	header := lines[i]
	i++
	for ; i < len(lines) && !strings.HasPrefix(lines[i], "--- "); i++ {
		switch l := lines[i]; {
		case strings.HasPrefix(l, "index "), strings.HasPrefix(l, "new file mode "), strings.HasPrefix(l, "deleted file mode "):
		default:
			return fp, i, fmt.Errorf("unsupported extended header %q after %q", l, header)
		}
	}
	if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "+++ ") {
		return fp, i, fmt.Errorf("no ---/+++ lines after %q", header)
	}
	fp.oldPath = stripPrefix(strings.TrimPrefix(lines[i], "--- "), "a/")
	fp.newPath = stripPrefix(strings.TrimPrefix(lines[i+1], "+++ "), "b/")
	i += 2
	for i < len(lines) && !strings.HasPrefix(lines[i], "diff --git ") {
		h, next, err := parseHunk(lines, i)
		if err != nil {
			return fp, i, err
		}
		fp.hunks = append(fp.hunks, h)
		i = next
	}
	return fp, i, nil
}

func stripPrefix(p, prefix string) string {
	if p == devNull {
		return p
	}
	return strings.TrimPrefix(p, prefix)
}

func parseHunk(lines []string, i int) (hunk, int, error) {
	m := hunkHeader.FindStringSubmatch(lines[i])
	if m == nil {
		return hunk{}, i, fmt.Errorf("expected a hunk header, got %q", lines[i])
	}
	h := hunk{header: lines[i]}
	oldStart, oldCount, err := rangeOf(m[1], m[2])
	if err != nil {
		return h, i, fmt.Errorf("hunk %q: %w", h.header, err)
	}
	_, newCount, err := rangeOf(m[3], m[4])
	if err != nil {
		return h, i, fmt.Errorf("hunk %q: %w", h.header, err)
	}
	h.oldStart = oldStart
	i++
	for len(h.oldLines) < oldCount || len(h.newLines) < newCount {
		if i >= len(lines) || lines[i] == "" {
			return h, i, fmt.Errorf("hunk %q ends early", h.header)
		}
		text := lines[i][1:] + "\n"
		if i+1 < len(lines) && strings.HasPrefix(lines[i+1], `\ `) {
			text = strings.TrimSuffix(text, "\n")
		}
		switch lines[i][0] {
		case ' ':
			h.oldLines = append(h.oldLines, text)
			h.newLines = append(h.newLines, text)
		case '-':
			h.oldLines = append(h.oldLines, text)
		case '+':
			h.newLines = append(h.newLines, text)
		default:
			return h, i, fmt.Errorf("hunk %q: unexpected line %q", h.header, lines[i])
		}
		i++
		if i < len(lines) && strings.HasPrefix(lines[i], `\ `) {
			i++
		}
	}
	if len(h.oldLines) != oldCount || len(h.newLines) != newCount {
		return h, i, fmt.Errorf("hunk %q holds %d old and %d new lines", h.header, len(h.oldLines), len(h.newLines))
	}
	return h, i, nil
}

func rangeOf(start, count string) (int, int, error) {
	s, err := strconv.Atoi(start)
	if err != nil {
		return 0, 0, err
	}
	if count == "" {
		return s, 1, nil
	}
	c, err := strconv.Atoi(count)
	return s, c, err
}

func (p patch) apply(files tree) error {
	for _, fp := range p.files {
		if err := fp.apply(files); err != nil {
			return fmt.Errorf("applying %s: %w", p.name, err)
		}
	}
	return nil
}

func (fp filePatch) apply(files tree) error {
	name := fp.newPath
	if name == devNull {
		name = fp.oldPath
	}
	data, exists := files[name]
	switch {
	case fp.oldPath == devNull && exists:
		return fmt.Errorf("%s: creating a file that exists", name)
	case fp.oldPath != devNull && !exists:
		return fmt.Errorf("%s: patching a file that does not exist", name)
	case fp.oldPath != devNull && fp.newPath != devNull && fp.oldPath != fp.newPath:
		return fmt.Errorf("%s: renames from %s are not supported", name, fp.oldPath)
	}
	lines := splitLines(string(data))
	offset, lowest := 0, 0
	for _, h := range fp.hunks {
		expected := h.oldIndex() + offset
		at, ok := findLines(lines, h.oldLines, expected, lowest)
		if !ok {
			return fmt.Errorf("%s: hunk %q does not apply", name, h.header)
		}
		lines = slices.Concat(lines[:at], h.newLines, lines[at+len(h.oldLines):])
		offset += at - expected + len(h.newLines) - len(h.oldLines)
		lowest = at + len(h.newLines)
	}
	if fp.newPath == devNull {
		if len(lines) > 0 {
			return fmt.Errorf("%s: deleting a file leaves %d lines", name, len(lines))
		}
		delete(files, name)
		return nil
	}
	files[name] = []byte(strings.Join(lines, ""))
	return nil
}

func (h hunk) oldIndex() int {
	if len(h.oldLines) == 0 {
		return h.oldStart
	}
	return h.oldStart - 1
}

func splitLines(s string) []string {
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func findLines(lines, want []string, expected, lowest int) (int, bool) {
	matches := func(at int) bool {
		return at >= lowest && at+len(want) <= len(lines) && slices.Equal(lines[at:at+len(want)], want)
	}
	for d := 0; expected-d >= lowest || expected+d+len(want) <= len(lines); d++ {
		if matches(expected + d) {
			return expected + d, true
		}
		if matches(expected - d) {
			return expected - d, true
		}
	}
	return 0, false
}
