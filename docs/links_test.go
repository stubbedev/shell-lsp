package docs

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const repoRoot = ".."

var (
	markdownLink  = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	inlineCode    = regexp.MustCompile("`[^`]*`")
	heading       = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	notSlug       = regexp.MustCompile(`[^\p{L}\p{N}_\- ]`)
	ownRepository = regexp.MustCompile(`^https://github\.com/stubbedev/shell-lsp/(?:blob|tree)/main/([^#]*)(?:#(.*))?$`)
	skippedDirs   = map[string]bool{".git": true, ".claude": true, "node_modules": true, "result": true, "out": true}
)

func markdownFiles(t *testing.T) []string {
	t.Helper()

	var out []string
	err := filepath.WalkDir(repoRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && skippedDirs[d.Name()] {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") {
			rel, err := filepath.Rel(repoRoot, p)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository for Markdown files: %v", err)
	}
	return out
}

func proseLines(t *testing.T, file string) []string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(file)))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	fenced := false
	for i, line := range lines {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			lines[i] = ""
			continue
		}
		if fenced {
			lines[i] = ""
		}
	}
	return lines
}

func anchors(t *testing.T, file string) map[string]bool {
	t.Helper()

	out := map[string]bool{}
	seen := map[string]int{}
	for _, line := range proseLines(t, file) {
		m := heading.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		slug := strings.ReplaceAll(notSlug.ReplaceAllString(strings.ToLower(strings.ReplaceAll(m[1], "`", "")), ""), " ", "-")
		if n := seen[slug]; n > 0 {
			out[slug+"-"+strconv.Itoa(n)] = true
		} else {
			out[slug] = true
		}
		seen[slug]++
	}
	return out
}

func linkTarget(from, target string) (file, anchor string, local bool) {
	if m := ownRepository.FindStringSubmatch(target); m != nil {
		return m[1], m[2], true
	}
	if strings.Contains(target, ":") {
		return "", "", false
	}
	file, anchor, _ = strings.Cut(target, "#")
	if file == "" {
		return from, anchor, true
	}
	return path.Clean(path.Join(path.Dir(from), file)), anchor, true
}

func TestMarkdownLinksResolve(t *testing.T) {
	t.Parallel()

	headings := map[string]map[string]bool{}
	for _, file := range markdownFiles(t) {
		for n, line := range proseLines(t, file) {
			for _, m := range markdownLink.FindAllStringSubmatch(inlineCode.ReplaceAllString(line, ""), -1) {
				target, anchor, local := linkTarget(file, m[1])
				if !local {
					continue
				}
				if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(target))); err != nil {
					t.Errorf("%s:%d links to %s, which does not exist", file, n+1, m[1])
					continue
				}
				if anchor == "" || !strings.HasSuffix(target, ".md") {
					continue
				}
				if headings[target] == nil {
					headings[target] = anchors(t, target)
				}
				if !headings[target][anchor] {
					t.Errorf("%s:%d links to %s, but %s has no heading #%s", file, n+1, m[1], target, anchor)
				}
			}
		}
	}
}
