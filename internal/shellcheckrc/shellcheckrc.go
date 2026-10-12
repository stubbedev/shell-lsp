package shellcheckrc

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/prose"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const FileName = ".shellcheckrc"

var Names = []string{FileName, "shellcheckrc"}

func SplitList(v string) []string {
	var out []string
	for item := range strings.SplitSeq(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

type File struct {
	Path        string
	Text        string
	Disabled    lint.CodeSet
	Enabled     []string
	Shell       *dialect.Dialect
	SourcePaths []string
	Extended    *bool

	relativePaths bool
}

func (f File) Relocatable() bool { return !f.relativePaths }

func Parse(path, text string) (File, error) {
	rc := File{Path: path, Text: text}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := rc.apply(line); err != nil {
			return File{}, fmt.Errorf("%s:%d: %w", path, i+1, err)
		}
	}
	return rc, nil
}

func (rc *File) apply(line string) error {
	d, ok := directive.ParseSettings(line)
	if !ok || d.Problem != nil {
		return fmt.Errorf("expected key=value settings such as disable=SC2034, got %q", line)
	}
	for _, pair := range d.Pairs {
		if err := rc.set(pair); err != nil {
			return fmt.Errorf("%s=%s: %w", pair.Name, pair.Value, err)
		}
	}
	return nil
}

var settingsKeys = prose.List(slices.DeleteFunc(directive.KeyNames(), func(name string) bool { return name == directive.Source.String() }), "and")

func (rc *File) set(pair directive.Pair) error {
	key, _ := pair.Key()
	switch key {
	case directive.Disable:
		set, err := lint.ParseCodeSet(pair.Value)
		if err != nil {
			return err
		}
		rc.Disabled = rc.Disabled.Union(set)
	case directive.Enable:
		rc.Enabled = append(rc.Enabled, SplitList(pair.Value)...)
	case directive.Shell:
		d, ok := dialect.FromName(pair.Value)
		if !ok {
			return fmt.Errorf("unknown shell %q", pair.Value)
		}
		rc.Shell = &d
	case directive.SourcePath:
		for _, dir := range filepath.SplitList(pair.Value) {
			if dir != directive.ScriptDir && !filepath.IsAbs(dir) {
				dir = filepath.Join(filepath.Dir(rc.Path), dir)
				rc.relativePaths = true
			}
			rc.SourcePaths = append(rc.SourcePaths, dir)
		}
	case directive.ExternalSources:
		if _, err := strconv.ParseBool(pair.Value); err != nil {
			return fmt.Errorf("want true or false: %w", err)
		}
	case directive.ExtendedAnalysis:
		b, err := strconv.ParseBool(pair.Value)
		if err != nil {
			return fmt.Errorf("want true or false: %w", err)
		}
		rc.Extended = &b
	default:
		return fmt.Errorf("%q is not a .shellcheckrc key; known keys are %s", pair.Name, settingsKeys)
	}
	return nil
}

func DisableEdit(text string, code lint.Code) (textedit.Edit, bool) {
	edit, merged, ok := mergeDisable(directive.SettingsLines(text), code)
	if !ok || merged {
		return edit, ok
	}
	return textedit.InsertLine(text, len(text), directive.Disable.String()+"="+code.String()), true
}

func HeaderDisableEdit(text string, code lint.Code) (textedit.Edit, bool) {
	edit, merged, ok := mergeDisable(directive.HeaderLines(text), code)
	if !ok || merged {
		return edit, ok
	}
	return directive.HeaderInsert(text, directive.Disable, code.String()), true
}

func StatementDisableEdit(text string, lineStart int, indent string, code lint.Code) (textedit.Edit, bool) {
	edit, merged, ok := mergeDisable(directive.CommentLinesAbove(text, lineStart), code)
	if !ok || merged {
		return edit, ok
	}
	return textedit.InsertLine(text, lineStart, indent+directive.Render(directive.Disable, code.String())), true
}

func mergeDisable(lines []directive.Line, code lint.Code) (textedit.Edit, bool, bool) {
	var merge *textedit.Edit
	for _, line := range lines {
		for _, pair := range line.Directive.Pairs {
			if !pair.Is(directive.Disable) {
				continue
			}
			if set, err := lint.ParseCodeSet(pair.Value); err == nil && set.Has(code) {
				return textedit.Edit{}, false, false
			}
			if merge == nil && pair.ValueEnd-pair.ValueOffset == len(pair.Value) {
				at := line.Start + pair.ValueEnd
				merge = &textedit.Edit{Start: at, End: at, NewText: "," + code.String()}
			}
		}
	}
	if merge != nil {
		return *merge, true, true
	}
	return textedit.Edit{}, false, true
}
