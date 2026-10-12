package navigation

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/platform"
	"github.com/stubbedev/shell-lsp/internal/textline"
	"github.com/stubbedev/shell-lsp/internal/workspace"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const maxChain = 64

type Loader interface {
	Load(path string) (*workspace.File, error)
	Generation() uint64
}

type Env struct {
	Loader   Loader
	Roots    []string
	Home     string
	PathDirs []string
}

type SourceRef struct {
	Command   parser.Command
	File      *syntax.Word
	Arg       parser.Span
	Directive *parser.Span
	Target    *workspace.File
}

func (r SourceRef) Names(offset int) bool {
	return r.Arg.Touches(offset) || r.Directive != nil && r.Directive.Touches(offset)
}

type resolvedSource struct {
	command   parser.Command
	file      *syntax.Word
	arg       parser.Span
	directive *parser.Span
	path      string
}

func Sources(file *workspace.File, env Env) []SourceRef {
	resolved := workspace.Memo(file, "navigation.sources", env.Loader.Generation(), func() []resolvedSource { return resolveSources(file, env) })
	refs := make([]SourceRef, len(resolved))
	for i, r := range resolved {
		refs[i] = SourceRef{Command: r.command, File: r.file, Arg: r.arg, Directive: r.directive}
		if r.path == "" {
			continue
		}
		if target, err := env.Loader.Load(r.path); err == nil {
			refs[i].Target = target
		}
	}
	return refs
}

func resolveSources(file *workspace.File, env Env) []resolvedSource {
	var out []resolvedSource
	for _, cmd := range file.Tree.Commands {
		word, ok := cmd.SourcedFile()
		if !ok {
			continue
		}
		span, ok := file.Tree.NodeSpan(word)
		if !ok {
			continue
		}
		r := resolvedSource{command: cmd, file: word, arg: span}
		var candidates []string
		if d, ok := directiveTarget(file.Tree, cmd); ok {
			r.directive = &d.span
			candidates = directiveCandidates(file, d.target, env)
		} else {
			candidates = evaluatedCandidates(file, cmd, word, env)
		}
		if target := loadFirst(candidates, env); target != nil {
			r.path = target.Path
		}
		out = append(out, r)
	}
	return out
}

func Imports(file *workspace.File, env Env) []parser.Import {
	chain := Chain(file, env)
	imports := make([]parser.Import, len(chain))
	for i, f := range chain {
		imports[i] = parser.Import{Name: RelativeName(file.Path, f.Path), Tree: f.Tree}
	}
	return imports
}

func RelativeName(from, to string) string {
	name, _ := relativeTo(filepath.Dir(from), to)
	return name
}

func relativeTo(dir, path string) (string, bool) {
	rel, ok := platform.RelativeWithin(dir, path)
	if !ok {
		return filepath.ToSlash(path), false
	}
	return filepath.ToSlash(rel), true
}

func Chain(file *workspace.File, env Env) []*workspace.File {
	chain, _ := Closure(file, env)
	return chain
}

func Closure(file *workspace.File, env Env) ([]*workspace.File, bool) {
	visited := map[string]bool{file.Path: true}
	queue := []*workspace.File{file}
	var chain []*workspace.File
	complete := true
	for len(queue) > 0 && len(chain) < maxChain {
		current := queue[0]
		queue = queue[1:]
		for _, ref := range Sources(current, env) {
			if ref.Target == nil {
				complete = false
				continue
			}
			if visited[ref.Target.Path] {
				continue
			}
			visited[ref.Target.Path] = true
			chain = append(chain, ref.Target)
			queue = append(queue, ref.Target)
		}
	}
	return chain, complete && len(queue) == 0
}

func loadFirst(candidates []string, env Env) *workspace.File {
	for _, candidate := range candidates {
		if target, err := env.Loader.Load(candidate); err == nil {
			return target
		}
	}
	return nil
}

func directiveCandidates(file *workspace.File, target string, env Env) []string {
	if target == "/dev/null" {
		return nil
	}
	scriptDir := filepath.Dir(file.Path)
	if rest, ok := strings.CutPrefix(target, directive.ScriptDir+"/"); ok {
		return []string{filepath.Join(scriptDir, rest)}
	}
	return relativeCandidates(target, append(slices.Clone(env.Roots), scriptDir), nil, env.Home)
}

func evaluatedCandidates(file *workspace.File, cmd parser.Command, word *syntax.Word, env Env) []string {
	value, ok := file.Tree.PathValue(word, cmd.Span.Start, parser.PathEnv{ScriptPath: file.Path, Home: env.Home})
	if !ok {
		return nil
	}
	bases := append([]string{filepath.Dir(file.Path)}, env.Roots...)
	return relativeCandidates(value, bases, env.PathDirs, env.Home)
}

func relativeCandidates(target string, bases, pathDirs []string, home string) []string {
	if rest, ok := strings.CutPrefix(target, "~/"); ok && home != "" {
		return []string{filepath.Join(home, rest)}
	}
	if filepath.IsAbs(target) {
		return []string{filepath.Clean(target)}
	}
	var out []string
	add := func(p string) {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	for _, base := range bases {
		add(filepath.Join(base, target))
	}
	if !strings.Contains(target, "/") {
		for _, dir := range pathDirs {
			add(filepath.Join(dir, target))
		}
	}
	return out
}

type sourceDirective struct {
	target string
	span   parser.Span
}

func directiveTarget(tree *parser.Tree, cmd parser.Command) (sourceDirective, bool) {
	text := tree.Text()
	for start, end := range textline.CommentsAbove(text, textline.Start(text, cmd.Span.Start)) {
		raw := text[start:end]
		d, ok := directive.Parse(strings.TrimSpace(raw))
		if !ok {
			continue
		}
		if pair, ok := d.Pair(directive.Source); ok {
			base := start + len(raw) - len(strings.TrimLeft(raw, " \t"))
			return sourceDirective{target: pair.Value, span: parser.Span{Start: base + pair.ValueOffset, End: base + pair.ValueEnd}}, true
		}
	}
	return sourceDirective{}, false
}
