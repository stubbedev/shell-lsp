package manpage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	lookupTimeout   = 5 * time.Second
	maxDescription  = 3
	pageWidth       = "80"
	sectionName     = "NAME"
	sectionSynopsis = "SYNOPSIS"
	sectionDescribe = "DESCRIPTION"
	hyphenBreak     = "\u2010"
)

var (
	validName  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._+-]*$`)
	ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")
	spaceRuns  = regexp.MustCompile(`[ \t]+`)
)

var ErrNoPage = errors.New("no manual page")

type Page struct {
	Summary     string
	Synopsis    []string
	Description []string
	Sections    map[string][]string
}

func Parse(raw []byte) (Page, error) {
	sections := splitSections(clean(raw))
	name, ok := sections[sectionName]
	if !ok {
		return Page{}, fmt.Errorf("%w: output has no %s section", ErrNoPage, sectionName)
	}
	page := Page{
		Synopsis: trimBlank(dedent(sections[sectionSynopsis])),
		Sections: sections,
	}
	if summary := paragraphs(name); len(summary) > 0 {
		page.Summary = summary[0]
	}
	description := paragraphs(sections[sectionDescribe])
	page.Description = description[:min(maxDescription, len(description))]
	return page, nil
}

func (p Page) Markdown() string {
	var sb strings.Builder
	if p.Summary != "" {
		fmt.Fprintf(&sb, "**%s**\n\n", p.Summary)
	}
	if len(p.Synopsis) > 0 {
		fmt.Fprintf(&sb, "```\n%s\n```\n\n", strings.Join(p.Synopsis, "\n"))
	}
	sb.WriteString(strings.Join(p.Description, "\n\n"))
	return strings.TrimSpace(sb.String())
}

func clean(raw []byte) []string {
	var out strings.Builder
	for i := 0; i < len(raw); i++ {
		if i+1 < len(raw) && raw[i+1] == '\b' {
			i++
			continue
		}
		out.WriteByte(raw[i])
	}
	text := ansiEscape.ReplaceAllString(out.String(), "")
	return strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
}

func splitSections(lines []string) map[string][]string {
	sections := map[string][]string{}
	current := ""
	for i, line := range lines {
		if i == 0 && strings.Contains(line, "(") {
			continue
		}
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			current = strings.TrimSpace(line)
			if _, ok := sections[current]; !ok {
				sections[current] = nil
			}
			continue
		}
		if current != "" {
			sections[current] = append(sections[current], line)
		}
	}
	return sections
}

func dedent(lines []string) []string {
	indent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		n := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		if len(line) >= indent && indent > 0 {
			line = line[indent:]
		}
		out[i] = strings.TrimRight(line, " \t")
	}
	return out
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func paragraphs(lines []string) []string {
	var out []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			out = append(out, spaceRuns.ReplaceAllString(current.String(), " "))
			current.Reset()
		}
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flush()
			continue
		}
		joined := current.String()
		if strings.HasSuffix(joined, hyphenBreak) {
			current.Reset()
			current.WriteString(strings.TrimSuffix(joined, hyphenBreak))
		} else if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(trimmed)
	}
	flush()
	return out
}

type Lookup struct {
	run   func(ctx context.Context, name string) ([]byte, error)
	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	page Page
	err  error
}

func NewLookup() *Lookup {
	return &Lookup{run: runMan, cache: map[string]cached{}}
}

func (l *Lookup) Page(ctx context.Context, name string) (Page, error) {
	if !validName.MatchString(name) {
		return Page{}, fmt.Errorf("%w: %q is not a command name", ErrNoPage, name)
	}
	l.mu.Lock()
	hit, ok := l.cache[name]
	l.mu.Unlock()
	if ok {
		return hit.page, hit.err
	}
	raw, err := l.run(ctx, name)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Page{}, ctxErr
	}
	var page Page
	if err == nil {
		page, err = Parse(raw)
	}
	l.mu.Lock()
	l.cache[name] = cached{page: page, err: err}
	l.mu.Unlock()
	return page, err
}

func runMan(ctx context.Context, name string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "man", "-P", "cat", "--", name) //nolint:gosec
	cmd.Env = append(os.Environ(), "MANWIDTH="+pageWidth, "MAN_KEEP_FORMATTING=", "GROFF_NO_SGR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w for %s: %w: %s", ErrNoPage, name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
