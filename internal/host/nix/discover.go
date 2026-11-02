package nix

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

const (
	shellApp      = "writeShellApplication"
	shellAppText  = "text"
	shellHook     = "shellHook"
	excludeChecks = "excludeShellChecks"
	systemd       = "systemd"
)

var (
	derivationBindings = phaseHooks(
		[]string{"Unpack", "Patch", "Configure", "Build", "Check", "Install", "Fixup", "InstallCheck", "Dist"},
		"buildPhase", "configurePhase", "checkPhase", "installPhase", "unpackPhase", "patchPhase", "fixupPhase",
		"installCheckPhase", "distPhase", "buildCommand",
	)
	serviceBindings = []string{"script", "preStart", "postStart", "preStop", "postStop", "reload"}
	bashWriters     = []string{"writeShellScript", "writeShellScriptBin", "writeBash", "writeBashBin"}
	dashWriters     = []string{"writeDash", "writeDashBin"}
	scriptWriters   = []string{"writeScript", "writeScriptBin"}
	commandRunner   = []string{"runCommand", "runCommandLocal", "runCommandCC", "runCommandNoCC", "runCommandNoCCLocal"}
	passThrough     = []string{"mkBefore", "mkAfter", "mkForce", "mkDefault"}
	conditional     = []string{"optionalString", "mkIf", "mkOrder"}
	markers         = map[string]dialect.Dialect{"shell": dialect.Bash, "bash": dialect.Bash, "sh": dialect.Posix, "dash": dialect.Dash, "zsh": dialect.Zsh, "ksh": dialect.Ksh}
)

func phaseHooks(stages []string, names ...string) []string {
	for _, stage := range stages {
		names = append(names, "pre"+stage, "post"+stage)
	}
	return names
}

type context struct {
	site          *syntax.Assign
	name          string
	ownerSystemd  bool
	insideSystemd bool
	set           *syntax.AttrSet
	callee        string
}

func (c context) cleared() context {
	c.site, c.name, c.insideSystemd = nil, "", c.ownerSystemd
	return c
}

func (c context) nested() context {
	return context{ownerSystemd: c.insideSystemd, insideSystemd: c.insideSystemd}
}

func (c context) sameBinding(other context) bool { return c.site == other.site && c.set == other.set }

type position struct {
	value  bool
	joined bool
}

type call struct {
	name string
	env  *syntax.AttrSet
}

type shellString struct {
	str        *syntax.String
	env        environment
	detection  dialect.Detection
	prelude    string
	directive  string
	shebang    bool
	derivation bool
	joined     bool
	context    context
}

func (f *file) discover() {
	f.walk(f.tree.Root, context{}, position{})
	for _, e := range f.tree.Stray {
		f.walk(e, context{}, position{})
	}
}

func (f *file) walk(e syntax.Expr, c context, at position) {
	plain := position{value: true}
	switch e := e.(type) {
	case *syntax.String:
		f.visitString(e, c, at, call{})
	case *syntax.Paren:
		c.ownerSystemd = c.insideSystemd
		f.walk(e.Inner, c, at)
	case *syntax.List:
		inner := c.nested()
		for _, item := range e.Items {
			f.walk(item, inner, position{})
		}
	case *syntax.AttrSet:
		f.attrs(e, c, "")
	case *syntax.LegacyLet:
		f.attrs(e.Set, c, "")
	case *syntax.Let:
		f.bindings(e.Bindings, c)
		f.walk(e.Body, c.cleared(), position{})
	case *syntax.With:
		f.walk(e.Scope, c, position{})
		f.walk(e.Body, c.cleared(), position{})
	case *syntax.Assert:
		f.walk(e.Cond, c, position{})
		f.walk(e.Body, c.cleared(), position{})
	case *syntax.If:
		f.walk(e.Cond, c, position{})
		f.walk(e.Then, c, plain)
		f.walk(e.Else, c, plain)
	case *syntax.Lambda:
		f.defaults(e.Param, c)
		f.walk(e.Body, c, position{})
	case *syntax.Apply:
		f.apply(e, c, at)
	case *syntax.Select:
		f.walk(e.Target, c, at)
		if e.Default != nil {
			f.walk(e.Default, c, plain)
		}
	case *syntax.HasAttr:
		f.walk(e.Target, c, at)
	case *syntax.Binary:
		f.walk(e.Left, c, at)
		right := position{}
		if e.Op == syntax.OpAdd {
			right = position{value: true, joined: true}
		}
		f.walk(e.Right, c, right)
	case *syntax.Unary:
		f.walk(e.Operand, c, position{})
	}
}

func (f *file) defaults(param syntax.Param, c context) {
	pattern, ok := param.(*syntax.PatternParam)
	if !ok {
		return
	}
	inner := c.nested()
	for _, formal := range pattern.Formals {
		if formal.Default != nil {
			f.walk(formal.Default, inner, position{})
		}
	}
}

func (f *file) attrs(set *syntax.AttrSet, c context, callee string) {
	inner := c.nested()
	inner.set, inner.callee = set, callee
	f.bindings(set.Bindings, inner)
}

func (f *file) bindings(bindings []syntax.Binding, c context) {
	for _, b := range bindings {
		switch b := b.(type) {
		case *syntax.Assign:
			inner := c
			inner.site, inner.name = b, lastIdent(b.Path)
			inner.insideSystemd = c.ownerSystemd || namesSystemd(b.Path)
			f.walk(b.Value, inner, position{value: true})
		case *syntax.Inherit:
			if b.From != nil {
				f.walk(b.From, c.cleared(), position{})
			}
		}
	}
}

func lastIdent(path syntax.AttrPath) string {
	if a, ok := path[len(path)-1].(*syntax.IdentAttr); ok {
		return a.Name
	}
	return ""
}

func namesSystemd(path syntax.AttrPath) bool {
	return slices.ContainsFunc(path, func(a syntax.Attr) bool {
		name, _, ok := syntax.StaticName(a)
		return ok && name == systemd
	})
}

func (f *file) apply(e *syntax.Apply, c context, at position) {
	fn := calleeName(e.Fn)
	f.walkApplied(e.Fn, e, 0, c, at)
	for i, arg := range e.Args {
		k := i + 1
		itemAt := position{}
		if k == 1 && slices.Contains(passThrough, fn) || k == 2 && slices.Contains(conditional, fn) {
			itemAt = at
		}
		f.walkApplied(arg, e, k, c, itemAt)
	}
}

func (f *file) walkApplied(item syntax.Expr, e *syntax.Apply, k int, c context, at position) {
	switch item := item.(type) {
	case *syntax.String:
		f.visitString(item, c, at, callOf(e, k))
	case *syntax.AttrSet:
		callee := ""
		if k > 0 {
			callee = calleeName(applied(e, k-1))
		}
		f.attrs(item, c, callee)
	default:
		f.walk(item, c, at)
	}
}

func applied(e *syntax.Apply, k int) syntax.Expr {
	if k == 0 {
		return e.Fn
	}
	return e.Args[k-1]
}

func callOf(e *syntax.Apply, k int) call {
	if k >= 2 {
		if _, ok := applied(e, k-1).(*syntax.String); ok {
			name := calleeName(applied(e, k-2))
			if slices.Contains(bashWriters, name) || slices.Contains(dashWriters, name) || slices.Contains(scriptWriters, name) {
				return call{name: name}
			}
		}
	}
	if k < 3 {
		return call{}
	}
	set, isSet := applied(e, k-1).(*syntax.AttrSet)
	_, named := applied(e, k-2).(*syntax.String)
	name := calleeName(applied(e, k-3))
	if !isSet || !named || !slices.Contains(commandRunner, name) {
		return call{}
	}
	return call{name: name, env: set}
}

func calleeName(e syntax.Expr) string {
	switch e := e.(type) {
	case *syntax.Ident:
		return e.Name
	case *syntax.Select:
		if _, plain := e.Target.(*syntax.Ident); !plain || e.Default != nil {
			return ""
		}
		for _, a := range e.Path {
			if _, ok := a.(*syntax.IdentAttr); !ok {
				return ""
			}
		}
		return lastIdent(e.Path)
	}
	return ""
}

func (f *file) visitString(str *syntax.String, c context, at position, via call) {
	if strings.TrimSpace(f.src[str.Content.Start:str.Content.End]) == "" {
		return
	}
	if s, ok := f.classify(str, c, at, via); ok {
		f.found = append(f.found, s)
	}
}

func (f *file) classify(str *syntax.String, c context, at position, via call) (shellString, bool) {
	s := shellString{str: str, joined: at.joined, context: c}
	if comment, ok := f.tree.CommentBefore(str.Span().Start); ok {
		marker := strings.TrimSpace(f.src[comment.Body.Start:comment.Body.End])
		if d, ok := markers[marker]; ok {
			s.detection = dialect.Detection{Dialect: d, Source: dialect.SourceHostSetting, Evidence: "/* " + marker + " */"}
			return s, true
		}
	}
	if via.name != "" {
		return f.written(s, via)
	}
	if !at.value {
		return shellString{}, false
	}
	switch {
	case slices.Contains(derivationBindings, c.name):
		s.derivation = true
		s.env = environment{set: c.set, source: derivationEnv}
	case c.name == shellHook:
		s.env = environment{set: c.set, source: derivationEnv}
	case slices.Contains(serviceBindings, c.name) && c.insideSystemd:
		s.env = environment{set: c.set, source: unitEnv}
	case c.name == shellAppText && c.set != nil && c.callee == shellApp:
		s.env = environment{set: c.set, source: appEnv}
		s.prelude = strictMode
		if excluded := f.excluded(c.set); len(excluded) > 0 {
			s.directive = directive.Render(directive.Disable, strings.Join(excluded, ",")) + "\n"
		}
	default:
		return shellString{}, false
	}
	s.detection = dialect.Detection{Dialect: dialect.Bash, Source: dialect.SourceHostDefault, Evidence: tool}
	return s, true
}

func (f *file) excluded(set *syntax.AttrSet) []string {
	var out []string
	for _, b := range set.Bindings {
		a, ok := b.(*syntax.Assign)
		if !ok || lastIdent(a.Path) != excludeChecks {
			continue
		}
		out = nil
		list, ok := a.Value.(*syntax.List)
		if !ok {
			continue
		}
		for _, item := range list.Items {
			if s, ok := item.(*syntax.String); ok && s.Kind == syntax.Quoted {
				out = append(out, f.src[s.Content.Start:s.Content.End])
			}
		}
	}
	return out
}

func (f *file) written(s shellString, via call) (shellString, bool) {
	switch {
	case slices.Contains(dashWriters, via.name):
		s.detection = dialect.Detection{Dialect: dialect.Dash, Source: dialect.SourceHostSetting, Evidence: via.name}
	case slices.Contains(scriptWriters, via.name):
		first := strings.TrimSpace(f.firstLine(s.str))
		d, ok := dialect.FromShebang(first)
		if !ok {
			return shellString{}, false
		}
		s.detection = dialect.Detection{Dialect: d, Source: dialect.SourceHostSetting, Evidence: first}
		s.shebang = true
	case slices.Contains(commandRunner, via.name):
		s.detection = dialect.Detection{Dialect: dialect.Bash, Source: dialect.SourceHostDefault, Evidence: tool}
		s.derivation = true
		s.env = environment{set: via.env, source: derivationEnv}
	default:
		s.detection = dialect.Detection{Dialect: dialect.Bash, Source: dialect.SourceHostDefault, Evidence: tool}
	}
	return s, true
}

func (f *file) firstLine(str *syntax.String) string {
	text := f.src[str.Content.Start:str.Content.End]
	if nl := strings.IndexByte(text, '\n'); nl >= 0 && str.Kind == syntax.Indented && strings.TrimSpace(text[:nl]) == "" {
		text = text[nl+1:]
	}
	line, _, _ := strings.Cut(text, "\n")
	return line
}
