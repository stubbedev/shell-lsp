package manpage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	commandSection          = "COMMAND"
	minMentionedSubcommands = 2
)

var (
	nameSeparators      = []string{" - ", " — ", " – "}
	commandPlaceholders = []string{"command", "commands", "subcommand", "subcommands"}
)

type Subcommand struct {
	Name    string
	Summary string
	Doc     string
	Options []Option
}

type Listing struct {
	Subcommands []Subcommand
	Unloaded    []string
}

func SubcommandWord(args []string) (string, bool) {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return arg, true
		}
	}
	return "", false
}

func splitName(line string) ([]string, string) {
	cut, tagline := len(line), ""
	for _, sep := range nameSeparators {
		if i := strings.Index(line, sep); i >= 0 && i < cut {
			cut, tagline = i, strings.TrimSpace(line[i+len(sep):])
		}
	}
	var names []string
	for name := range strings.SplitSeq(line[:cut], ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names, tagline
}

func collectSubcommands(doc document, names, synopsis []string, fallback []Option) []Subcommand {
	if len(names) == 0 {
		return nil
	}
	var subs []Subcommand
	seen := map[string]bool{}
	for _, s := range doc.sections {
		if !strings.Contains(s.name, commandSection) {
			continue
		}
		for _, sub := range sectionSubcommands(s.blocks, names, fallback) {
			if !seen[sub.Name] {
				seen[sub.Name] = true
				subs = append(subs, sub)
			}
		}
	}
	if !takesSubcommand(synopsis, seen) {
		return nil
	}
	slices.SortFunc(subs, bySubcommandName)
	return subs
}

func sectionSubcommands(blocks []block, names []string, fallback []Option) []Subcommand {
	level := -1
	for _, b := range blocks {
		if b.kind == taggedBlock && (level < 0 || b.depth < level) {
			level = b.depth
		}
	}
	var subs []Subcommand
	for i, b := range blocks {
		name, usage, ok := commandHead(b, names)
		if !ok || b.depth != level {
			continue
		}
		subs = append(subs, Subcommand{
			Name:    name,
			Summary: firstSentence(b.text),
			Doc:     entryDoc(usage, b.text),
			Options: firstOptions(appendOptions(nil, entryBody(blocks[i+1:], level), map[string]bool{}), fallback),
		})
	}
	return subs
}

func commandHead(b block, names []string) (string, string, bool) {
	if b.kind != taggedBlock {
		return "", "", false
	}
	fields := strings.Fields(b.tag)
	if len(fields) == 0 {
		return "", "", false
	}
	word, referenced := splitReference(strings.TrimSuffix(fields[0], ","))
	usage := names[0] + " " + b.tag
	switch {
	case referenced:
		word = subcommandOf(word, names)
		usage = names[0] + " " + word
	case slices.Contains(names, word):
		word, usage = "", b.tag
		if len(fields) > 1 {
			word = strings.TrimSuffix(fields[1], ",")
		}
	}
	return word, usage, bareWord.MatchString(word)
}

func subcommandOf(page string, names []string) string {
	for _, name := range names {
		if sub, ok := strings.CutPrefix(page, name+"-"); ok {
			return sub
		}
	}
	return ""
}

func entryBody(blocks []block, level int) []block {
	end := slices.IndexFunc(blocks, func(b block) bool { return b.kind == taggedBlock && b.depth <= level })
	if end < 0 {
		return blocks
	}
	return blocks[:end]
}

func takesSubcommand(synopsis []string, names map[string]bool) bool {
	mentioned := map[string]bool{}
	for _, line := range synopsis {
		for field := range strings.FieldsSeq(line) {
			word := strings.Trim(field, "[]{}<>().|,")
			if slices.Contains(commandPlaceholders, strings.ToLower(word)) {
				return true
			}
			if names[word] {
				mentioned[word] = true
			}
		}
	}
	return len(mentioned) >= minMentionedSubcommands
}

func firstSentence(text string) string {
	if i := strings.Index(text, ". "); i >= 0 {
		return text[:i+1]
	}
	return text
}

func entryDoc(usage, text string) string {
	doc := "```\n" + usage + "\n```"
	if text == "" {
		return doc
	}
	return doc + "\n\n" + text
}

func firstOptions(lists ...[]Option) []Option {
	for _, options := range lists {
		if len(options) > 0 {
			return options
		}
	}
	return nil
}

func bySubcommandName(a, b Subcommand) int { return strings.Compare(a.Name, b.Name) }

func (p Page) subcommand(name string) (Subcommand, bool) {
	i, found := slices.BinarySearchFunc(p.subcommands, name, func(s Subcommand, name string) int { return strings.Compare(s.Name, name) })
	if !found {
		return Subcommand{}, false
	}
	return p.subcommands[i], true
}

func (p Page) invokes(command, sub string) bool {
	for _, line := range p.Synopsis {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[0] == command && slices.Contains(fields[1:], sub) {
			return true
		}
	}
	return false
}

func fromPage(name string, page Page, listed Subcommand, parent Page) Subcommand {
	return Subcommand{
		Name:    name,
		Summary: page.tagline,
		Doc:     page.Markdown(),
		Options: firstOptions(page.options, listed.Options, parent.options),
	}
}

func (l *Lookup) Subcommands(ctx context.Context, command string) (Listing, error) {
	l.mu.Lock()
	memo, ok := l.listings[command]
	l.mu.Unlock()
	if ok {
		return Listing{Subcommands: memo}, nil
	}
	if err := ctx.Err(); err != nil {
		return Listing{}, err
	}
	var parent Page
	if l.Has(command) {
		hit, loaded := l.cached(command)
		if !loaded {
			return Listing{Unloaded: []string{command}}, nil
		}
		if hit.err != nil && !errors.Is(hit.err, ErrNoPage) {
			return Listing{}, hit.err
		}
		parent = hit.page
	}
	listing := Listing{Subcommands: slices.Clone(parent.subcommands)}
	prefix := command + "-"
	for _, pageName := range l.library.prefixed(prefix) {
		name := pageName[len(prefix):]
		if _, listed := parent.subcommand(name); listed || !bareWord.MatchString(name) {
			continue
		}
		hit, loaded := l.cached(pageName)
		switch {
		case !loaded:
			listing.Unloaded = append(listing.Unloaded, pageName)
		case hit.err == nil && hit.page.invokes(command, name):
			listing.Subcommands = append(listing.Subcommands, fromPage(name, hit.page, Subcommand{}, parent))
		case hit.err != nil && !errors.Is(hit.err, ErrNoPage):
			return Listing{}, hit.err
		}
	}
	slices.SortFunc(listing.Subcommands, bySubcommandName)
	if len(listing.Unloaded) == 0 {
		l.mu.Lock()
		l.listings[command] = listing.Subcommands
		l.mu.Unlock()
	}
	return listing, nil
}

func (l *Lookup) Subcommand(ctx context.Context, command, name string) (Subcommand, error) {
	if !bareWord.MatchString(name) {
		return Subcommand{}, fmt.Errorf("%w: %q is not a subcommand name", ErrNoPage, name)
	}
	parent, err := l.Page(ctx, command)
	if err != nil && !errors.Is(err, ErrNoPage) {
		return Subcommand{}, err
	}
	listed, isListed := parent.subcommand(name)
	if pageName := command + "-" + name; l.Has(pageName) {
		page, err := l.Page(ctx, pageName)
		if err != nil && !errors.Is(err, ErrNoPage) {
			return Subcommand{}, err
		}
		if err == nil && (isListed || page.invokes(command, name)) {
			return fromPage(name, page, listed, parent), nil
		}
	}
	if isListed {
		return listed, nil
	}
	return Subcommand{}, fmt.Errorf("%w: %s %s is not a documented subcommand", ErrNoPage, command, name)
}

func (l *Lookup) Options(ctx context.Context, command string, args []string) ([]Option, error) {
	if name, ok := SubcommandWord(args); ok {
		sub, err := l.Subcommand(ctx, command, name)
		if err == nil {
			return sub.Options, nil
		}
		if !errors.Is(err, ErrNoPage) {
			return nil, err
		}
	}
	page, err := l.Page(ctx, command)
	if err != nil {
		return nil, err
	}
	return page.Options(), nil
}
