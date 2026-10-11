package manpage

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/getopt"
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
	Name        string
	Summary     string
	Doc         string
	Options     []Option
	Subcommands []Subcommand
}

func (s *Subcommand) arity(name string) getopt.Arity {
	for _, o := range s.Options {
		if !slices.Contains(o.Names, name) {
			continue
		}
		if o.Valued {
			return getopt.Valued
		}
		return getopt.Flag
	}
	return getopt.Unknown
}

func findSubcommand(subs []Subcommand, name string) (Subcommand, bool) {
	i, found := slices.BinarySearchFunc(subs, name, func(s Subcommand, name string) int { return strings.Compare(s.Name, name) })
	if !found {
		return Subcommand{}, false
	}
	return subs[i], true
}

func bySubcommandName(a, b Subcommand) int { return strings.Compare(a.Name, b.Name) }

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

type entryScope struct {
	names      []string
	invocation string
	fallback   []Option
}

type collected struct {
	subs []Subcommand
	seen map[string]bool
}

func (c *collected) add(subs []Subcommand) {
	for _, sub := range subs {
		if !c.seen[sub.Name] {
			c.seen[sub.Name] = true
			c.subs = append(c.subs, sub)
		}
	}
}

func (c *collected) sorted(usage []string) []Subcommand {
	if !takesSubcommand(usage, c.seen) {
		return nil
	}
	slices.SortFunc(c.subs, bySubcommandName)
	return c.subs
}

func collectSubcommands(doc document, names, synopsis []string, fallback []Option) []Subcommand {
	if len(names) == 0 {
		return nil
	}
	scope := entryScope{names: names, invocation: invocation(names[0], synopsis), fallback: fallback}
	found := collected{seen: map[string]bool{}}
	for _, s := range doc.sections {
		if strings.Contains(s.name, commandSection) {
			found.add(scope.listed(s.blocks))
		}
		found.add(scope.headed(s.blocks))
	}
	return found.sorted(synopsis)
}

func invocation(name string, synopsis []string) string {
	for _, line := range synopsis {
		fields := strings.Fields(line)
		if n := dashedPrefix(fields, name); n > 0 {
			return strings.Join(fields[:n], " ")
		}
	}
	return name
}

func dashedPrefix(fields []string, name string) int {
	rest := name
	for i, field := range fields {
		after, ok := strings.CutPrefix(rest, field)
		if !ok {
			return 0
		}
		if after == "" {
			return i + 1
		}
		if rest, ok = strings.CutPrefix(after, "-"); !ok {
			return 0
		}
	}
	return 0
}

func (s entryScope) listed(blocks []block) []Subcommand {
	level := -1
	for _, b := range blocks {
		if b.kind == taggedBlock && (level < 0 || b.depth < level) {
			level = b.depth
		}
	}
	var subs []Subcommand
	for i, b := range blocks {
		if b.kind != taggedBlock || b.depth != level {
			continue
		}
		name, usage, ok := s.head(b.tag, false)
		if !ok {
			continue
		}
		body := entryBody(blocks[i+1:], level)
		sub := Subcommand{
			Name:    name,
			Summary: firstSentence(b.text),
			Doc:     entryDoc(usage, b.text),
			Options: first(appendOptions(nil, body, map[string]bool{}), s.fallback),
		}
		sub.Subcommands = s.within(sub).nested(body, usage)
		subs = append(subs, sub)
	}
	return subs
}

func (s entryScope) within(sub Subcommand) entryScope {
	return entryScope{names: []string{s.names[0] + "-" + sub.Name, sub.Name}, invocation: s.invocation + " " + sub.Name, fallback: sub.Options}
}

func (s entryScope) nested(body []block, usage string) []Subcommand {
	found := collected{seen: map[string]bool{}}
	found.add(s.listed(body))
	return found.sorted([]string{usage})
}

func (s entryScope) headed(blocks []block) []Subcommand {
	var subs []Subcommand
	for i, b := range blocks {
		if b.kind != headingBlock {
			continue
		}
		heads, summary := splitName(b.text)
		if len(heads) != 1 {
			continue
		}
		name, usage, ok := s.head(heads[0], true)
		if !ok {
			continue
		}
		body := blocks[i+1:]
		if end := slices.IndexFunc(body, func(b block) bool { return b.kind == headingBlock }); end >= 0 {
			body = body[:end]
		}
		subs = append(subs, Subcommand{
			Name:    name,
			Summary: summary,
			Doc:     entryDoc(usage, summary),
			Options: first(appendOptions(nil, body, map[string]bool{}), s.fallback),
		})
	}
	return subs
}

func (s entryScope) head(text string, prefixed bool) (string, string, bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", "", false
	}
	for _, name := range s.names {
		switch n := dashedPrefix(fields, name); {
		case n == len(fields):
			return "", "", false
		case n > 0:
			word := strings.TrimSuffix(fields[n], ",")
			return word, text, bareWord.MatchString(word)
		}
	}
	if prefixed {
		return "", "", false
	}
	word, referenced := splitReference(strings.TrimSuffix(fields[0], ","))
	if referenced {
		word = subcommandOf(word, s.names)
		return word, s.invocation + " " + word, bareWord.MatchString(word)
	}
	return word, s.invocation + " " + text, bareWord.MatchString(word)
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
	end := slices.IndexFunc(blocks, func(b block) bool {
		return b.kind == headingBlock || b.kind == taggedBlock && b.depth <= level
	})
	if end < 0 {
		return blocks
	}
	return blocks[:end]
}

func takesSubcommand(usage []string, names map[string]bool) bool {
	mentioned := map[string]bool{}
	for _, line := range usage {
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

func first[T any](lists ...[]T) []T {
	for _, list := range lists {
		if len(list) > 0 {
			return list
		}
	}
	return nil
}

func (p Page) invokes(path []string, sub string) bool {
	for _, line := range p.Synopsis {
		fields := strings.Fields(line)
		if len(fields) > len(path) && slices.Equal(fields[:len(path)], path) && slices.Contains(fields[len(path):], sub) {
			return true
		}
	}
	return false
}

func fromPage(name string, page Page, listed, parent Subcommand) Subcommand {
	return Subcommand{
		Name:        name,
		Summary:     page.tagline,
		Doc:         page.Markdown(),
		Options:     first(page.options, listed.Options, parent.Options),
		Subcommands: first(page.subcommands, listed.Subcommands),
	}
}
