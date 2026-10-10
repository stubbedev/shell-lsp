package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const (
	ShellcheckSource  = "shellcheck"
	shellcheckTimeout = 10 * time.Second
	wikiURL           = "https://www.shellcheck.net/wiki/SC"
)

var ErrDialectUnsupported = errors.New("shellcheck does not support this dialect")

var shellcheckTags = map[int]lsp.DiagnosticTag{
	2034: lsp.TagUnnecessary,
	2317: lsp.TagUnnecessary,
	2003: lsp.TagDeprecated,
	2006: lsp.TagDeprecated,
	2166: lsp.TagDeprecated,
	2196: lsp.TagDeprecated,
	2197: lsp.TagDeprecated,
}

func ShellcheckArgs(settings config.Shellcheck, d dialect.Dialect) ([]string, error) {
	shell, ok := d.ShellcheckName()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrDialectUnsupported, d)
	}
	args := []string{"--format=json1", "--shell=" + shell, "--external-sources"}
	if len(settings.Exclude) > 0 {
		codes := make([]string, len(settings.Exclude))
		for i, code := range settings.Exclude {
			codes[i] = "SC" + strconv.Itoa(code)
		}
		args = append(args, "--exclude="+strings.Join(codes, ","))
	}
	args = append(args, settings.Arguments...)
	return append(args, "-"), nil
}

type shellcheckOutput struct {
	Comments []shellcheckComment `json:"comments"`
}

type shellcheckComment struct {
	Line      uint32         `json:"line"`
	EndLine   uint32         `json:"endLine"`
	Column    uint32         `json:"column"`
	EndColumn uint32         `json:"endColumn"`
	Level     string         `json:"level"`
	Code      int            `json:"code"`
	Message   string         `json:"message"`
	Fix       *shellcheckFix `json:"fix"`
}

type shellcheckFix struct {
	Replacements []shellcheckReplacement `json:"replacements"`
}

type shellcheckReplacement struct {
	Line        uint32 `json:"line"`
	EndLine     uint32 `json:"endLine"`
	Column      uint32 `json:"column"`
	EndColumn   uint32 `json:"endColumn"`
	Replacement string `json:"replacement"`
}

type FixData struct {
	Edits []lsp.TextEdit `json:"edits"`
}

func ParseShellcheck(output []byte, doc *docstore.Document, enc docstore.Encoding) ([]lsp.Diagnostic, error) {
	var parsed shellcheckOutput
	if err := json.Unmarshal(output, &parsed); err != nil {
		return nil, fmt.Errorf("parsing shellcheck json1 output %q: %w", output, err)
	}
	out := make([]lsp.Diagnostic, 0, len(parsed.Comments))
	for _, c := range parsed.Comments {
		severity, err := severityOf(c.Level)
		if err != nil {
			return nil, err
		}
		code := "SC" + strconv.Itoa(c.Code)
		d := lsp.Diagnostic{
			Range:           shellcheckRange(doc, enc, c.Line, c.Column, c.EndLine, c.EndColumn),
			Severity:        severity,
			Code:            code,
			CodeDescription: &lsp.CodeDescription{Href: wikiURL + strconv.Itoa(c.Code)},
			Source:          ShellcheckSource,
			Message:         c.Message,
		}
		if tag, ok := shellcheckTags[c.Code]; ok {
			d.Tags = []lsp.DiagnosticTag{tag}
		}
		if c.Fix != nil && len(c.Fix.Replacements) > 0 {
			data, err := json.Marshal(fixData(c.Fix, doc, enc))
			if err != nil {
				return nil, fmt.Errorf("encoding fix for %s: %w", code, err)
			}
			d.Data = data
		}
		out = append(out, d)
	}
	return out, nil
}

func fixData(fix *shellcheckFix, doc *docstore.Document, enc docstore.Encoding) FixData {
	edits := make([]lsp.TextEdit, len(fix.Replacements))
	for i, r := range fix.Replacements {
		edits[i] = lsp.TextEdit{
			Range:   shellcheckRange(doc, enc, r.Line, r.Column, r.EndLine, r.EndColumn),
			NewText: r.Replacement,
		}
	}
	return FixData{Edits: edits}
}

func severityOf(level string) (lsp.DiagnosticSeverity, error) {
	switch level {
	case "error":
		return lsp.SeverityError, nil
	case "warning":
		return lsp.SeverityWarning, nil
	case "info":
		return lsp.SeverityInformation, nil
	case "style":
		return lsp.SeverityHint, nil
	}
	return 0, fmt.Errorf("unknown shellcheck level %q", level)
}

func shellcheckRange(doc *docstore.Document, enc docstore.Encoding, line, col, endLine, endCol uint32) lsp.Range {
	start := doc.PositionToOffset(codePointPosition(line, col), docstore.EncodingUTF32)
	end := doc.PositionToOffset(codePointPosition(endLine, endCol), docstore.EncodingUTF32)
	return doc.RangeFor(start, end, enc)
}

func codePointPosition(line, col uint32) lsp.Position {
	return lsp.Position{Line: max(line, 1) - 1, Character: max(col, 1) - 1}
}

type Binary struct {
	Path string
	ID   string
}

func ResolveBinary(name string) (Binary, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return Binary{}, fmt.Errorf("locating shellcheck %q: %w", name, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return Binary{}, fmt.Errorf("inspecting shellcheck binary %s: %w", path, err)
	}
	return Binary{Path: path, ID: fmt.Sprintf("%s@%d.%d", path, info.Size(), info.ModTime().UnixNano())}, nil
}

func RunShellcheck(ctx context.Context, bin Binary, args []string, dir, text string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, shellcheckTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin.Path, args...) //nolint:gosec
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(text)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() == 1 {
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("running %s %s: %w: %s", bin.Path, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
