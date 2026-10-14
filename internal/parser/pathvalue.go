package parser

import (
	"path"
	"strings"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type PathEnv struct {
	ScriptPath string
	Home       string
}

const maxPathDepth = 8

func (t *Tree) PathValue(w *syntax.Word, at int, env PathEnv) (string, bool) {
	return pathEvaluator{tree: t, env: env}.word(w, at, 0)
}

type pathEvaluator struct {
	tree *Tree
	env  PathEnv
}

func (e pathEvaluator) word(w *syntax.Word, at, depth int) (string, bool) {
	if w == nil || depth > maxPathDepth {
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

func (e pathEvaluator) part(part syntax.WordPart, at, depth int) (string, bool) {
	switch p := part.(type) {
	case *syntax.Lit, *syntax.SglQuoted:
		return Literal(&syntax.Word{Parts: []syntax.WordPart{p}})
	case *syntax.DblQuoted:
		var sb strings.Builder
		for _, inner := range p.Parts {
			if lit, ok := inner.(*syntax.Lit); ok {
				dq := &syntax.DblQuoted{Parts: []syntax.WordPart{lit}}
				value, ok := Literal(&syntax.Word{Parts: []syntax.WordPart{dq}})
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

func (e pathEvaluator) param(p *syntax.ParamExp, at, depth int) (string, bool) {
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
		return pathDir(base), true
	case syntax.DefaultUnset, syntax.DefaultUnsetOrNull:
		if known {
			return base, true
		}
		return e.word(p.Exp.Word, at, depth+1)
	}
	return "", false
}

func (e pathEvaluator) variable(p *syntax.ParamExp, at, depth int) (string, bool) {
	name := p.Param.Value
	kind, _ := ParamKindOf(name)
	if kind == ParamZero || (name == "BASH_SOURCE" && (p.Index == nil || zeroIndexLit(p.Index))) {
		return e.env.ScriptPath, true
	}
	if p.Index != nil {
		return "", false
	}
	if def, ok := e.tree.DefinitionBefore(name, at); ok {
		if def.Value == nil {
			return "", false
		}
		return e.word(def.Value, def.Span.Start, depth+1)
	}
	if name == "HOME" && e.env.Home != "" {
		return e.env.Home, true
	}
	return "", false
}

func zeroIndexLit(index syntax.ArithmExpr) bool {
	lit, ok := IndexLit(index)
	return ok && lit == "0"
}

func (e pathEvaluator) cmdSubst(c *syntax.CmdSubst, at, depth int) (string, bool) {
	if len(c.Stmts) != 1 || c.Stmts[0].Cmd == nil {
		return "", false
	}
	switch cmd := c.Stmts[0].Cmd.(type) {
	case *syntax.CallExpr:
		return e.call(cmd, at, depth)
	case *syntax.BinaryCmd:
		cd, okCD := cmd.X.Cmd.(*syntax.CallExpr)
		pwd, okPwd := cmd.Y.Cmd.(*syntax.CallExpr)
		if cmd.Op != syntax.AndStmt || !okCD || !okPwd || len(cd.Args) != 2 {
			return "", false
		}
		cdName, _ := CallName(cd)
		pwdName, _ := CallName(pwd)
		if cdName != "cd" || pwdName != "pwd" {
			return "", false
		}
		return e.word(cd.Args[1], at, depth+1)
	}
	return "", false
}

func (e pathEvaluator) call(call *syntax.CallExpr, at, depth int) (string, bool) {
	if len(call.Args) < 2 {
		return "", false
	}
	target := call.Args[len(call.Args)-1]
	switch name, _ := CallName(call); name {
	case "dirname":
		value, ok := e.word(target, at, depth+1)
		return pathDir(value), ok
	case "readlink", "realpath":
		return e.word(target, at, depth+1)
	}
	return "", false
}

func pathDir(p string) string {
	if !strings.Contains(p, "/") {
		return "."
	}
	return path.Dir(p)
}
