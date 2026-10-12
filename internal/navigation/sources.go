package navigation

import (
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/platform"
	"github.com/stubbedev/shell-lsp/internal/workspace"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const (
	maxChain     = 64
	maxEvalDepth = 8
	scriptDirVar = "SCRIPTDIR"
)

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
		if d, ok := directiveTarget(file.Tree, cmd); ok {
			r.directive = &d.span
		}
		if target := resolveSource(file, cmd, word, env); target != nil {
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

func resolveSource(file *workspace.File, cmd parser.Command, word *syntax.Word, env Env) *workspace.File {
	for _, candidate := range sourceCandidates(file, cmd, word, env) {
		if target, err := env.Loader.Load(candidate); err == nil {
			return target
		}
	}
	return nil
}

func sourceCandidates(file *workspace.File, cmd parser.Command, word *syntax.Word, env Env) []string {
	scriptDir := filepath.Dir(file.Path)
	if d, ok := directiveTarget(file.Tree, cmd); ok {
		target := d.target
		if target == "/dev/null" {
			return nil
		}
		if rest, ok := strings.CutPrefix(target, scriptDirVar+"/"); ok {
			return []string{filepath.Join(scriptDir, rest)}
		}
		return relativeCandidates(target, append(slices.Clone(env.Roots), scriptDir), nil, env.Home)
	}
	e := evaluator{tree: file.Tree, scriptPath: file.Path, home: env.Home}
	value, ok := e.word(word, cmd.Span.Start, 0)
	if !ok {
		return nil
	}
	bases := append([]string{scriptDir}, env.Roots...)
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
	lineStart := strings.LastIndexByte(text[:cmd.Span.Start], '\n') + 1
	for lineStart > 0 {
		prevStart := strings.LastIndexByte(text[:lineStart-1], '\n') + 1
		raw := text[prevStart : lineStart-1]
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "#") {
			return sourceDirective{}, false
		}
		if d, ok := directive.Parse(line); ok {
			base := prevStart + len(raw) - len(strings.TrimLeft(raw, " \t"))
			for _, pair := range d.Pairs {
				if pair.Key == directive.Source {
					return sourceDirective{target: pair.Value, span: parser.Span{Start: base + pair.ValueOffset, End: base + pair.ValueEnd}}, true
				}
			}
		}
		lineStart = prevStart
	}
	return sourceDirective{}, false
}

type evaluator struct {
	tree       *parser.Tree
	scriptPath string
	home       string
}

func (e evaluator) word(w *syntax.Word, at, depth int) (string, bool) {
	if w == nil || depth > maxEvalDepth {
		return "", false
	}
	var sb strings.Builder
	for _, part := range w.Parts {
		value, ok := e.part(part, at, depth)
		if !ok {
			return "", false
		}
		sb.WriteString(value)
	}
	return sb.String(), true
}

func (e evaluator) part(part syntax.WordPart, at, depth int) (string, bool) {
	switch p := part.(type) {
	case *syntax.Lit, *syntax.SglQuoted:
		return parser.Literal(&syntax.Word{Parts: []syntax.WordPart{p}})
	case *syntax.DblQuoted:
		var sb strings.Builder
		for _, inner := range p.Parts {
			if lit, ok := inner.(*syntax.Lit); ok {
				dq := &syntax.DblQuoted{Parts: []syntax.WordPart{lit}}
				value, ok := parser.Literal(&syntax.Word{Parts: []syntax.WordPart{dq}})
				if !ok {
					return "", false
				}
				sb.WriteString(value)
				continue
			}
			value, ok := e.part(inner, at, depth)
			if !ok {
				return "", false
			}
			sb.WriteString(value)
		}
		return sb.String(), true
	case *syntax.ParamExp:
		return e.param(p, at, depth)
	case *syntax.CmdSubst:
		return e.cmdSubst(p, at, depth)
	}
	return "", false
}

func (e evaluator) param(p *syntax.ParamExp, at, depth int) (string, bool) {
	if p.Param == nil || p.Excl || p.Length || p.Slice != nil || p.Repl != nil || p.Names != 0 || p.Flags != nil {
		return "", false
	}
	base, known := e.variable(p, at, depth)
	if p.Exp == nil {
		return base, known
	}
	switch p.Exp.Op {
	case syntax.RemSmallSuffix:
		if !known || p.Exp.Word == nil || p.Exp.Word.Lit() != "/*" {
			return "", false
		}
		return dir(base), true
	case syntax.DefaultUnset, syntax.DefaultUnsetOrNull:
		if known {
			return base, true
		}
		return e.word(p.Exp.Word, at, depth+1)
	}
	return "", false
}

func (e evaluator) variable(p *syntax.ParamExp, at, depth int) (string, bool) {
	name := p.Param.Value
	if name == "0" || (name == "BASH_SOURCE" && (p.Index == nil || indexIsZero(p.Index))) {
		return e.scriptPath, true
	}
	if p.Index != nil {
		return "", false
	}
	defs := e.tree.Visible(name, e.tree.ScopeAt(at))
	for def := range defs.Backward() {
		if def.Span.Start < at && def.Value != nil {
			return e.word(def.Value, def.Span.Start, depth+1)
		}
	}
	if name == "HOME" && e.home != "" {
		return e.home, true
	}
	return "", false
}

func indexIsZero(index syntax.ArithmExpr) bool {
	w, ok := index.(*syntax.Word)
	return ok && w.Lit() == "0"
}

func (e evaluator) cmdSubst(c *syntax.CmdSubst, at, depth int) (string, bool) {
	if len(c.Stmts) != 1 || c.Stmts[0].Cmd == nil {
		return "", false
	}
	switch cmd := c.Stmts[0].Cmd.(type) {
	case *syntax.CallExpr:
		return e.call(cmd, at, depth)
	case *syntax.BinaryCmd:
		cd, okCD := cmd.X.Cmd.(*syntax.CallExpr)
		pwd, okPwd := cmd.Y.Cmd.(*syntax.CallExpr)
		if cmd.Op != syntax.AndStmt || !okCD || !okPwd || len(cd.Args) != 2 || len(pwd.Args) == 0 ||
			cd.Args[0].Lit() != "cd" || pwd.Args[0].Lit() != "pwd" {
			return "", false
		}
		return e.word(cd.Args[1], at, depth+1)
	}
	return "", false
}

func (e evaluator) call(call *syntax.CallExpr, at, depth int) (string, bool) {
	if len(call.Args) < 2 {
		return "", false
	}
	target := call.Args[len(call.Args)-1]
	switch call.Args[0].Lit() {
	case "dirname":
		value, ok := e.word(target, at, depth+1)
		return dir(value), ok
	case "readlink", "realpath":
		return e.word(target, at, depth+1)
	}
	return "", false
}

func dir(p string) string {
	if !strings.Contains(p, "/") {
		return "."
	}
	return path.Dir(p)
}
