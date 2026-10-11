package manpage

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

const (
	maxDescription  = 3
	sectionName     = "NAME"
	sectionSynopsis = "SYNOPSIS"
	sectionDescribe = "DESCRIPTION"
)

var validName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._+-]*$`)

var ErrNoPage = errors.New("no manual page")

type Page struct {
	Summary     string
	Synopsis    []string
	Description []string
	options     []Option
	tagline     string
	subcommands []Subcommand
}

func Render(src []byte) (Page, error) {
	doc := parseDocument(string(src))
	name, ok := doc.section(sectionName)
	if !ok {
		return Page{}, fmt.Errorf("%w: the page has no %s section", ErrNoPage, sectionName)
	}
	page := Page{Summary: strings.Join(texts(name.blocks), " "), options: collectOptions(doc)}
	names, tagline := splitName(page.Summary)
	page.tagline = tagline
	if synopsis, ok := doc.section(sectionSynopsis); ok {
		page.Synopsis = synopsisLines(synopsis)
	}
	page.subcommands = collectSubcommands(doc, names, page.Synopsis, page.options)
	if description, ok := doc.section(sectionDescribe); ok {
		for _, b := range description.blocks {
			if b.kind == paragraphBlock && len(page.Description) < maxDescription {
				page.Description = append(page.Description, b.text)
			}
		}
	}
	return page, nil
}

func texts(blocks []block) []string {
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, b.text)
	}
	return out
}

func synopsisLines(s section) []string {
	var lines []string
	for _, b := range s.blocks {
		switch b.kind {
		case literalBlock:
			lines = append(lines, strings.Split(b.text, "\n")...)
		case taggedBlock:
			lines = append(lines, strings.TrimSpace(b.tag+" "+b.text))
		default:
			lines = append(lines, b.text)
		}
	}
	return lines
}

func (p Page) Options() []Option { return p.options }

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

type Lookup struct {
	library  *library
	mu       sync.Mutex
	cache    map[string]cached
	listings map[string][]Subcommand
}

type cached struct {
	page Page
	err  error
}

func NewLookup(manpath, path string) *Lookup {
	return &Lookup{library: newLibrary(manpath, path), cache: map[string]cached{}, listings: map[string][]Subcommand{}}
}

func (l *Lookup) Has(name string) bool {
	_, ok := l.library.index()[name]
	return ok
}

func (l *Lookup) cached(name string) (cached, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	hit, ok := l.cache[name]
	return hit, ok
}

func (l *Lookup) Page(ctx context.Context, name string) (Page, error) {
	if !validName.MatchString(name) {
		return Page{}, fmt.Errorf("%w: %q is not a command name", ErrNoPage, name)
	}
	if hit, ok := l.cached(name); ok {
		return hit.page, hit.err
	}
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	src, err := l.library.source(name)
	var page Page
	if err == nil {
		page, err = Render(src)
	}
	l.mu.Lock()
	l.cache[name] = cached{page: page, err: err}
	l.mu.Unlock()
	return page, err
}
