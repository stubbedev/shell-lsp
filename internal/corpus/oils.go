package corpus

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const oilsSpecSuffix = ".test.sh"

type SpecCase struct {
	Path   string
	Index  int
	Line   int
	Code   string
	Shells []string
}

func (c SpecCase) ID() string { return fmt.Sprintf("%s#%d", c.Path, c.Index) }

func SpecCases(repos []Repo, cache string) ([]SpecCase, error) {
	var out []SpecCase
	for _, r := range repos {
		if r.Kind != KindOilsSpec {
			continue
		}
		root := r.Dir(cache)
		err := filepath.WalkDir(root, func(file string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), oilsSpecSuffix) {
				return nil
			}
			raw, err := os.ReadFile(file) //nolint:gosec
			if err != nil {
				return err
			}
			name, err := displayName(root, r.Name, file)
			if err != nil {
				return err
			}
			out = append(out, ParseOilsSpec(name, string(raw))...)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("reading the spec tests of %s: %w", r.Name, err)
		}
	}
	return out, nil
}

func ParseOilsSpec(path, text string) []SpecCase {
	var (
		out     []SpecCase
		shells  []string
		current *SpecCase
		code    strings.Builder
		inBlock bool
	)
	flush := func() {
		if current != nil {
			current.Code = code.String()
			out = append(out, *current)
		}
		code.Reset()
	}
	for n, line := range strings.Split(text, "\n") {
		switch {
		case inBlock:
			inBlock = line != "## END"
		case strings.HasPrefix(line, "#### "):
			flush()
			current = &SpecCase{Path: path, Index: len(out) + 1, Line: n + 1, Shells: shells}
		case strings.HasPrefix(line, "## compare_shells:"):
			shells = strings.Fields(strings.TrimPrefix(line, "## compare_shells:"))
		case line == "##" || strings.HasPrefix(line, "## "):
			inBlock = strings.HasSuffix(line, "STDOUT:") || strings.HasSuffix(line, "STDERR:")
		case current != nil:
			code.WriteString(line)
			code.WriteByte('\n')
		}
	}
	flush()
	return out
}
