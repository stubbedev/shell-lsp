package lint

import (
	"regexp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const codeRelativeSource = nativeBase + 2013

var relativeSourceFamily = family{
	codes: map[Code]meta{
		codeRelativeSource: {severity: SeverityWarning, optional: optionalRelativeSources},
	},
	finish: []func(*pass){checkRelativeSources},
}

var (
	scriptPathEnv       = parser.PathEnv{ScriptPath: directive.ScriptDir + "/script"}
	buildPlaceholder    = regexp.MustCompile(`@[^@\s/]+@`)
	defaultingOperators = []syntax.ParExpOperator{syntax.DefaultUnset, syntax.DefaultUnsetOrNull}
)

func checkRelativeSources(p *pass) {
	if p.rcFile || p.bats {
		return
	}
	changesDirectory := p.firstDirectoryChange()
	for _, cmd := range p.tree.Commands {
		word, ok := cmd.SourcedFile()
		if !ok || cmd.Span.Start > changesDirectory {
			continue
		}
		if _, named := p.sourceDirective(cmd.Span.Start); named {
			continue
		}
		target, ok := p.tree.PathValue(word, cmd.Span.Start, scriptPathEnv)
		if !ok || !relativePath(target) || buildPlaceholder.MatchString(target) || hasDefault(word) {
			continue
		}
		span, ok := p.span(word)
		if !ok {
			continue
		}
		anchored := p.anchoredSource(strings.TrimPrefix(target, "./"))
		var fixes []Fix
		if lit, ok := parser.Literal(word); ok && !strings.ContainsAny(lit, "\"$`\\") {
			fixes = append(fixes, reviewFix("Load "+lit+" from the script's directory", textedit.Edit{Start: span.Start, End: span.End, NewText: anchored}))
		}
		p.report(codeRelativeSource, span, target+" "+p.sourceLookup(cmd.Name, target)+"; write "+anchored+" to load the file next to the script.", fixes...)
	}
}

func hasDefault(w *syntax.Word) bool {
	return parser.Any(w, func(n syntax.Node) bool {
		pe, ok := n.(*syntax.ParamExp)
		return ok && pe.Exp != nil && slices.Contains(defaultingOperators, pe.Exp.Op)
	})
}

func relativePath(target string) bool {
	return target != "" && !strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "~") && !strings.HasPrefix(target, directive.ScriptDir)
}

func (p *pass) sourceLookup(name, target string) string {
	switch {
	case strings.Contains(target, "/"):
		return "is resolved against the directory the script is run from, so running it from anywhere else loads another file or none"
	case p.dialect == dialect.Bash || name == "source" && p.dialect == dialect.Zsh:
		return "is looked up in PATH and then in the directory the script is run from, so running it from anywhere else loads another file or none"
	}
	return "is looked up only in PATH, so it is not found next to the script unless that directory is in PATH"
}

func (p *pass) anchoredSource(rest string) string {
	if p.dialect == dialect.Bash {
		return `"$(dirname "${BASH_SOURCE[0]}")/` + rest + `"`
	}
	return `"$(dirname "$0")/` + rest + `"`
}

func (p *pass) firstDirectoryChange() int {
	first := len(p.text)
	for _, cmd := range p.tree.Commands {
		if (cmd.Name == "cd" || cmd.Name == "pushd") && cmd.Span.Start < first {
			first = cmd.Span.Start
		}
	}
	return first
}
