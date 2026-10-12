package lint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const nullDevice = "/dev/null"

var sourcesFamily = family{
	codes: map[Code]meta{
		1090: {severity: SeverityWarning, summary: "A sourced file is only known at run time, so it cannot be followed."},
		1091: {severity: SeverityInfo, summary: "A sourced file was not found, so it is not followed."},
		1094: {severity: SeverityWarning, summary: "A sourced file has syntax errors, so what it defines may be missing."},
	},
	rules: []rule{
		{finish: checkSources},
	},
}

func checkSources(p *pass) {
	for _, cmd := range p.tree.Commands {
		if cmd.IsSourceBuiltin() {
			p.checkSource(cmd)
		}
	}
}

func (p *pass) checkSource(cmd parser.Command) {
	word, hasFile := cmd.SourcedFile()
	if !hasFile {
		p.report(1090, cmd.NameSpan, cmd.Name+" has no file argument, so there is nothing to follow.")
		return
	}
	span, ok := p.span(word)
	if !ok {
		return
	}
	if i := slices.IndexFunc(p.sources, func(s Source) bool { return s.Command == cmd.Span }); i >= 0 {
		if slices.ContainsFunc(p.sources[i].Tree.Errors, func(e parser.Error) bool { return e.Kind == parser.ErrSyntax }) {
			p.report(1094, span, "The sourced file has syntax errors, so what it defines may be missing here; fix the errors reported in that file.")
		}
		return
	}
	name, ok := p.sourceDirective(cmd.Span.Start)
	if !ok {
		name, ok = staticSourceName(word)
	}
	switch {
	case !ok:
		p.report(1090, span, fmt.Sprintf("The file %s is only known at run time, so it can't be followed; name it with a `# shellcheck source=path` directive, or use source=/dev/null to skip it.", p.text[span.Start:span.End]))
	case name == nullDevice:
	case strings.HasPrefix(name, "~/"):
		p.report(1090, span, name+" depends on the home directory of whoever runs the script, so it can't be followed; name the file with a `# shellcheck source=path` directive.")
	default:
		p.report(1091, span, fmt.Sprintf("Not following %s: the file was not found next to the script or in the workspace; point a `# shellcheck source=path` directive at it, or use source=/dev/null to skip it.", name))
	}
}

func (p *pass) sourceDirective(at int) (string, bool) {
	name, found := "", false
	for _, d := range p.directives {
		value, ok := d.parsed.Value(directive.Source)
		if !ok {
			continue
		}
		if scope, ok := p.directiveScope(d); ok && scope.Contains(at) {
			name, found = value, true
		}
	}
	return name, found
}

func staticSourceName(word *syntax.Word) (string, bool) {
	if name, ok := parser.Literal(word); ok {
		return name, true
	}
	parts := slices.Collect(unquotedParts(word))
	if len(parts) < 2 || !isExpansion(parts[0]) {
		return "", false
	}
	var rest strings.Builder
	for _, part := range parts[1:] {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			return "", false
		}
		rest.WriteString(lit.Value)
	}
	if !strings.HasPrefix(rest.String(), "/") {
		return "", false
	}
	return "." + rest.String(), true
}
