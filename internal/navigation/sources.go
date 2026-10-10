package navigation

import (
	"path"
	"path/filepath"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const (
	maxChain     = 64
	maxEvalDepth = 8
	directive    = "shellcheck"
	sourceKey    = "source="
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
	Command parser.Command
	Arg     parser.Span
	Target  *workspace.File
}

func IsSource(cmd parser.Command) bool {
	return (cmd.Name == "source" || cmd.Name == ".") && len(cmd.Args) > 0
}

func Sources(file *workspace.File, env Env) []SourceRef {
	refs, _ := file.Memo("navigation.sources", env.Loader.Generation(), func() any { return resolveSources(file, env) }).([]SourceRef)
	return refs
}

func resolveSources(file *workspace.File, env Env) []SourceRef {
	var refs []SourceRef
	for _, cmd := range file.Tree.Commands {
		if !IsSource(cmd) {
			continue
		}
		span, ok := file.Tree.NodeSpan(cmd.Args[0])
		if !ok {
			continue
		}
		refs = append(refs, SourceRef{Command: cmd, Arg: span, Target: resolveSource(file, cmd, env)})
	}
	return refs
}

func Chain(file *workspace.File, env Env) []*workspace.File {
	visited := map[string]bool{file.Path: true}
	queue := []*workspace.File{file}
	var chain []*workspace.File
	for len(queue) > 0 && len(chain) < maxChain {
		current := queue[0]
		queue = queue[1:]
		for _, ref := range Sources(current, env) {
			if ref.Target == nil || visited[ref.Target.Path] {
				continue
			}
			visited[ref.Target.Path] = true
			chain = append(chain, ref.Target)
			queue = append(queue, ref.Target)
		}
	}
	return chain
}

func resolveSource(file *workspace.File, cmd parser.Command, env Env) *workspace.File {
	for _, candidate := range sourceCandidates(file, cmd, env) {
		if target, err := env.Loader.Load(candidate); err == nil {
			return target
		}
	}
	return nil
}

func sourceCandidates(file *workspace.File, cmd parser.Command, env Env) []string {
	scriptDir := filepath.Dir(file.Path)
	if target, ok := directiveTarget(file.Tree, cmd); ok {
		if target == "/dev/null" {
			return nil
		}
		if rest, ok := strings.CutPrefix(target, scriptDirVar+"/"); ok {
			return []string{filepath.Join(scriptDir, rest)}
		}
		return relativeCandidates(target, append(slices.Clone(env.Roots), scriptDir), nil, env.Home)
	}
	e := evaluator{tree: file.Tree, scriptPath: file.Path, home: env.Home}
	value, ok := e.word(cmd.Args[0], cmd.Span.Start, 0)
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

func directiveTarget(tree *parser.Tree, cmd parser.Command) (string, bool) {
	text := tree.Text()
	lineStart := strings.LastIndexByte(text[:cmd.Span.Start], '\n') + 1
	for lineStart > 0 {
		prevStart := strings.LastIndexByte(text[:lineStart-1], '\n') + 1
		line := strings.TrimSpace(text[prevStart : lineStart-1])
		body, isComment := strings.CutPrefix(line, "#")
		if !isComment {
			return "", false
		}
		fields := strings.Fields(body)
		if len(fields) > 0 && fields[0] == directive {
			for _, field := range fields[1:] {
				if target, ok := strings.CutPrefix(field, sourceKey); ok {
					return target, true
				}
			}
		}
		lineStart = prevStart
	}
	return "", false
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
