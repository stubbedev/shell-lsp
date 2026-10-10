package shelldocs

import (
	"fmt"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

type Entry struct {
	Name     string
	Dialects dialect.Set
	Synopsis string
	Doc      string
	Notes    map[dialect.Dialect]string
}

func (e Entry) Markdown(d dialect.Dialect) string {
	var sb strings.Builder
	if e.Synopsis != "" {
		fmt.Fprintf(&sb, "```%s\n%s\n```\n\n", fence(d), e.Synopsis)
	}
	sb.WriteString(e.Doc)
	if note, ok := e.Notes[d]; ok {
		fmt.Fprintf(&sb, "\n\n**%s:** %s", d, note)
	}
	if !e.Dialects.Has(d) {
		fmt.Fprintf(&sb, "\n\n_Not available in %s._", d)
	}
	return sb.String()
}

func fence(d dialect.Dialect) string {
	if d == dialect.Zsh {
		return "zsh"
	}
	return "sh"
}

type table map[string]Entry

func index(entries []Entry) table {
	t := make(table, len(entries))
	for _, e := range entries {
		if _, dup := t[e.Name]; dup {
			panic("shelldocs: duplicate entry " + e.Name)
		}
		t[e.Name] = e
	}
	return t
}

var (
	builtinIndex   = index(builtins)
	keywordIndex   = index(keywords)
	variableIndex  = index(variables)
	parameterIndex = index(parameters)
)

func Builtin(name string) (Entry, bool)   { e, ok := builtinIndex[name]; return e, ok }
func Keyword(name string) (Entry, bool)   { e, ok := keywordIndex[name]; return e, ok }
func Variable(name string) (Entry, bool)  { e, ok := variableIndex[name]; return e, ok }
func Parameter(name string) (Entry, bool) { e, ok := parameterIndex[name]; return e, ok }

func Builtins(d dialect.Dialect) []Entry  { return available(builtins, d) }
func Keywords(d dialect.Dialect) []Entry  { return available(keywords, d) }
func Variables(d dialect.Dialect) []Entry { return available(variables, d) }

func available(entries []Entry, d dialect.Dialect) []Entry {
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Dialects.Has(d) {
			out = append(out, e)
		}
	}
	return out
}

func Expansion(key string) (string, bool) {
	doc, ok := expansions[key]
	return doc, ok
}

func ZshFlag(flag byte) (string, bool) {
	doc, ok := zshFlags[flag]
	return doc, ok
}

func ZshModifier(modifier string) (string, bool) {
	doc, ok := zshModifiers[modifier]
	return doc, ok
}
