package shelldocs

import (
	"fmt"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

type Entry struct {
	Name     string
	shells   dialect.Shells
	Synopsis string
	Doc      string
	Notes    map[dialect.Dialect]string
}

func (e Entry) In(s dialect.Shell) bool { return e.shells.Has(s) }

func (e Entry) inDialect(d dialect.Dialect) bool { return e.shells&dialect.SetOf(d).Shells() != 0 }

func (e Entry) Markdown(s dialect.Shell) string {
	var sb strings.Builder
	if e.Synopsis != "" {
		fmt.Fprintf(&sb, "```%s\n%s\n```\n\n", FenceLanguage(s.Dialect), e.Synopsis)
	}
	sb.WriteString(e.Doc)
	if note, ok := e.Notes[s.Dialect]; ok {
		fmt.Fprintf(&sb, "\n\n**%s:** %s", s.Dialect, note)
	}
	if !e.In(s) {
		fmt.Fprintf(&sb, "\n\n_Not available in %s._", e.missingShell(s))
	}
	return sb.String()
}

func (e Entry) missingShell(s dialect.Shell) string {
	if !e.inDialect(s.Dialect) {
		return s.Dialect.String()
	}
	return s.Lang().String()
}

func FenceLanguage(d dialect.Dialect) string {
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

func Builtins(s dialect.Shell) []Entry  { return available(builtins, s) }
func Keywords(s dialect.Shell) []Entry  { return available(keywords, s) }
func Variables(s dialect.Shell) []Entry { return available(variables, s) }

func available(entries []Entry, s dialect.Shell) []Entry {
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.In(s) {
			out = append(out, e)
		}
	}
	return out
}
