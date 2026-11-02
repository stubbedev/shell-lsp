package nix

import (
	"cmp"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/projection"
)

var (
	sourcedDirective = directive.Render(directive.Disable, "SC2154,SC2034,SC2329") + "\n"
	markers          = map[string]dialect.Dialect{"shell": dialect.Bash, "bash": dialect.Bash, "sh": dialect.Posix, "dash": dialect.Dash, "zsh": dialect.Zsh, "ksh": dialect.Ksh}
)

func phaseHooks(stages []string, names ...string) []string {
	for _, stage := range stages {
		names = append(names, "pre"+stage, stageBinding(stage), "post"+stage)
	}
	return names
}

func stageBinding(stage string) string {
	return strings.ToLower(stage[:1]) + stage[1:] + "Phase"
}

type context struct {
	site   *syntax.Assign
	name   string
	set    *syntax.AttrSet
	callee string
	merged syntax.Expr
	prefix string
}

func (c context) cleared() context {
	c.site, c.name = nil, ""
	return c
}

func (c context) sameBinding(other context) bool { return c.site == other.site && c.set == other.set }

type position struct {
	value  bool
	joined bool
	items  bool
}

type call struct {
	name    string
	surface *surface
	env     *syntax.AttrSet
	expr    syntax.Expr
	file    string
}

type hostedText struct {
	via   *projection.Map
	inner Embedding
	base  model.Scope
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
	phase      int
	skip       int
	provides   []string
	hosted     *hostedText
}

func (f *file) discover() {
	f.walk(f.tree.Root, context{}, position{})
	for _, e := range f.tree.Stray {
		f.walk(e, context{}, position{})
	}
	f.path = nil
}

func (f *file) walk(e syntax.Expr, c context, at position) {
	plain := position{value: true}
	switch e := e.(type) {
	case *syntax.String:
		f.visitString(e, c, at, call{})
	case *syntax.Paren:
		f.walk(e.Inner, c, at)
	case *syntax.List:
		if at.items || at.value && f.listSurface() {
			for _, item := range e.Items {
				f.walk(item, c, plain)
			}
			return
		}
		inner := context{}
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
		f.defaults(e.Param)
		body := position{}
		if at.items {
			body = plain
		}
		mark := len(f.path)
		f.path = append(f.path, unknownAttr)
		f.walk(e.Body, c, body)
		f.path = f.path[:mark]
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
		before, after := c, c
		if e.Op == syntax.OpUpdate {
			before.merged, after.merged = e.Right, e.Left
		}
		f.walk(e.Left, before, at)
		right := position{}
		if e.Op == syntax.OpAdd {
			right = position{value: true, joined: true}
		}
		f.walk(e.Right, after, right)
	case *syntax.Unary:
		f.walk(e.Operand, c, position{})
	}
}

func (f *file) listSurface() bool {
	s := attrSurfaces.find(f.path)
	return s != nil && s.role == execRole
}

func (f *file) defaults(param syntax.Param) {
	pattern, ok := param.(*syntax.PatternParam)
	if !ok {
		return
	}
	inner := context{}
	for _, formal := range pattern.Formals {
		if formal.Default != nil {
			f.walk(formal.Default, inner, position{})
		}
	}
}

func (f *file) attrs(set *syntax.AttrSet, c context, callee string) {
	inner := context{}
	inner.set, inner.callee, inner.merged = set, callee, c.merged
	f.bindings(set.Bindings, inner)
}

func (f *file) bindings(bindings []syntax.Binding, c context) {
	for _, b := range bindings {
		switch b := b.(type) {
		case *syntax.Assign:
			inner := c
			inner.site, inner.name, inner.prefix = b, lastIdent(b.Path), pathPrefix(b.Path)
			mark := len(f.path)
			for _, a := range b.Path {
				name, ok := syntax.AttrName(a)
				if !ok {
					name = unknownAttr
				}
				f.path = append(f.path, name)
			}
			f.walk(b.Value, inner, position{value: true})
			f.path = f.path[:mark]
		case *syntax.Inherit:
			if b.From != nil {
				f.walk(b.From, c.cleared(), position{})
			}
		}
	}
}

func pathPrefix(path syntax.AttrPath) string {
	names := make([]string, 0, len(path)-1)
	for _, a := range path[:len(path)-1] {
		name, ok := syntax.AttrName(a)
		if !ok {
			return ""
		}
		names = append(names, name)
	}
	return strings.Join(names, envPathSeparator)
}

func lastIdent(path syntax.AttrPath) string {
	if a, ok := path[len(path)-1].(*syntax.IdentAttr); ok {
		return a.Name
	}
	return ""
}

func (f *file) apply(e *syntax.Apply, c context, at position) {
	carried, carries := carriers[calleeName(e.Fn)]
	f.walkApplied(e.Fn, e, 0, c, at)
	for i, arg := range e.Args {
		k := i + 1
		itemAt := position{}
		if carries && k == carried.arg {
			itemAt = at
			itemAt.items = carried.items && at.value
		}
		f.walkApplied(arg, e, k, c, itemAt)
	}
}

func (f *file) walkApplied(item syntax.Expr, e *syntax.Apply, k int, c context, at position) {
	switch item := syntax.Unparenthesized(item).(type) {
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
	if k == 0 {
		return call{}
	}
	name := calleeName(syntax.Unparenthesized(e.Fn))
	c := callSurfaces[name]
	if c == nil || !c.positional(k, len(e.Args)) {
		return call{}
	}
	out := call{name: name, surface: c.script}
	switch c.script.role {
	case runnerRole:
		env := applied(e, k-1)
		if set, isSet := syntax.Unparenthesized(env).(*syntax.AttrSet); isSet {
			out.env = set
		} else {
			out.expr = env
		}
	case namedRole:
		if text, literal := syntax.Unparenthesized(e.Args[0]).(*syntax.String); literal {
			out.file = literalOf(text)
		}
	case scriptRole, sourcedRole, phaseRole, hookRole, unitRole, appRole, shebangRole, gitAliasRole, execRole, foreignRole:
	}
	return out
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
		f.markQuotedHoles(s.str, s.detection.Shell)
		f.found = append(f.found, s)
		return
	}
	for _, part := range str.Parts {
		if hole, ok := part.(*syntax.Interpolation); ok {
			f.walk(hole.Value, context{}, position{})
		}
	}
}

func (f *file) classify(str *syntax.String, c context, at position, via call) (shellString, bool) {
	s := shellString{str: str, joined: at.joined, context: c}
	marked, isMarked := f.marker(str)
	found := via.surface
	if found == nil && at.value {
		found = f.attrSurface(c)
	}
	if found != nil {
		if shaped, ok := f.shaped(s, found, via); ok {
			if isMarked && shaped.hosted == nil {
				shaped.detection = marked
			}
			return shaped, true
		}
	}
	if !isMarked {
		return shellString{}, false
	}
	s.detection = marked
	s.directive = sourcedDirective
	return s, true
}

func (f *file) marker(str *syntax.String) (dialect.Detection, bool) {
	comment, ok := f.tree.CommentBefore(str.Span().Start)
	if !ok {
		return dialect.Detection{}, false
	}
	marker := strings.TrimSpace(f.src[comment.Body.Start:comment.Body.End])
	d, known := markers[marker]
	if !known {
		return dialect.Detection{}, false
	}
	return dialect.Detection{Dialect: d, Source: dialect.SourceHostSetting, Evidence: "/* " + marker + " */"}, true
}

func (f *file) attrSurface(c context) *surface {
	if kind := callSurfaces[c.callee]; kind != nil && c.prefix == "" {
		if keyed := kind.keys[c.name]; keyed != nil {
			return keyed
		}
	}
	return attrSurfaces.find(f.path)
}

func (f *file) shaped(s shellString, found *surface, via call) (shellString, bool) {
	c := s.context
	switch found.role {
	case scriptRole:
	case foreignRole:
		return shellString{}, false
	case phaseRole:
		s.derivation = true
		s.env = environment{set: c.set, source: derivationEnv, expr: c.merged}
		s.phase = phaseRank(c.name)
		s.provides = declaredOutputs(c.set)
	case hookRole:
		s.env = environment{set: c.set, source: derivationEnv, expr: c.merged}
		s.phase = phaseRank(c.name)
		s.directive = sourcedDirective
	case unitRole:
		s.env = environment{set: c.set, source: unitEnv, prefix: c.prefix, expr: c.merged}
		s.directive = readsEnvironment(s.env)
	case appRole:
		s.env = environment{set: c.set, source: appEnv, expr: c.merged}
		s.prelude = strictMode
		s.directive = readsEnvironment(s.env)
		if excluded := f.excluded(c.set); len(excluded) > 0 {
			s.directive += directive.Render(directive.Disable, strings.Join(excluded, ",")) + "\n"
		}
	case runnerRole:
		s.derivation = true
		s.env = environment{set: via.env, source: derivationEnv, expr: via.expr}
		s.phase = phaseRank("buildCommand")
		s.provides = declaredOutputs(via.env)
	case shebangRole:
		return f.withShebang(s)
	case namedRole:
		return f.namedText(s, via)
	case sourcedRole:
		s.directive = sourcedDirective
		s.provides = found.provides
	case gitAliasRole:
		if !strings.HasPrefix(f.src[s.str.Content.Start:s.str.Content.End], "!") {
			return shellString{}, false
		}
		s.skip = len("!")
		s.directive = sourcedDirective
	case execRole:
		text := f.src[s.str.Content.Start:s.str.Content.End]
		s.skip = len(text) - len(strings.TrimLeft(text, "-@:+!"))
		s.directive = sourcedDirective
	}
	s.detection = found.detection()
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

func readsEnvironment(env environment) string {
	if env.set != nil && len(vars(env)) > 0 || env.expr != nil {
		return phaseDirective
	}
	return ""
}

func (f *file) written(s shellString, via call) (shellString, bool) {
	if marked, isMarker := markers[via.name]; isMarker {
		s.detection = dialect.Detection{Dialect: marked, Source: dialect.SourceHostSetting, Evidence: via.name}
		return s, true
	}
	if c := callSurfaces[via.name]; c != nil && c.script != nil {
		return f.shaped(s, c.script, via)
	}
	s.detection = dialect.Detection{Dialect: dialect.Bash, Source: dialect.SourceHostDefault, Evidence: tool}
	return s, true
}

func (f *file) shebangOf(str *syntax.String) (dialect.Dialect, string, bool, bool) {
	first := strings.TrimSpace(f.firstLine(str))
	if !strings.HasPrefix(first, shebangPrefix) {
		return 0, "", false, false
	}
	d, shell := dialect.FromShebang(f.shebangLine(str))
	return d, first, shell, true
}

const (
	shebangPrefix = "#!"
	bashPath      = "/bin/bash"
)

func (f *file) shebangLine(str *syntax.String) string {
	var line strings.Builder
	for _, part := range str.Parts {
		switch part := part.(type) {
		case *syntax.Literal:
			text := part.Text
			if line.Len() == 0 {
				text = strings.TrimLeft(text, " \t\r\n")
			}
			before, _, ended := strings.Cut(text, "\n")
			line.WriteString(before)
			if ended {
				return line.String()
			}
		case *syntax.Interpolation:
			line.WriteString(interpreterOf(part.Value))
		case *syntax.Escape:
			return line.String()
		}
	}
	return line.String()
}

func interpreterOf(e syntax.Expr) string {
	value := syntax.Unparenthesized(e)
	if selected, isSelect := value.(*syntax.Select); isSelect && lastIdent(selected.Path) == "shell" && calleeName(selected.Target) == "stdenv" {
		return bashPath
	}
	if calleeName(value) == "runtimeShell" {
		return bashPath
	}
	if _, exe, named := programOf(value); named && exe != "" {
		return "/" + exe
	}
	return ""
}

func (f *file) withShebang(s shellString) (shellString, bool) {
	d, first, shell, _ := f.shebangOf(s.str)
	if !shell {
		return shellString{}, false
	}
	s.detection = dialect.Detection{Dialect: d, Source: dialect.SourceHostSetting, Evidence: first}
	s.shebang = true
	return s, true
}

func (f *file) namedText(s shellString, via call) (shellString, bool) {
	name := cmp.Or(via.file, f.fileName(s.context))
	if f.embed != nil && name != "" {
		if analyze, known := f.embed(name); known {
			return f.hostText(s, analyze), true
		}
	}
	if _, _, _, has := f.shebangOf(s.str); has {
		return f.withShebang(s)
	}
	d, named := dialect.FromFileName(name)
	if name == "" || !named {
		return shellString{}, false
	}
	s.detection = dialect.Detection{Dialect: d, Source: dialect.SourceHostSetting, Evidence: name}
	s.directive = sourcedDirective
	return s, true
}

func (f *file) fileName(c context) string {
	for _, key := range []string{"destination", "target"} {
		if name := siblingLiteral(c.set, key); name != "" {
			return name
		}
	}
	if c.callee == writeTextFile {
		return siblingLiteral(c.set, "name")
	}
	if n := len(f.path); n >= 2 {
		return f.path[n-2]
	}
	return ""
}

func siblingLiteral(set *syntax.AttrSet, key string) string {
	if set == nil {
		return ""
	}
	for _, b := range set.Bindings {
		a, assigned := b.(*syntax.Assign)
		if !assigned || len(a.Path) != 1 || lastIdent(a.Path) != key {
			continue
		}
		if text, literal := syntax.Unparenthesized(a.Value).(*syntax.String); literal {
			return literalOf(text)
		}
	}
	return ""
}

func (f *file) hostText(s shellString, analyze func(string) Embedding) shellString {
	b := projection.NewBuilder(f.src)
	b.StandIn(holeStandIn)
	f.body(b, s, "", textBody)
	via := b.Map()
	inner := analyze(via.Text())
	s.hosted = &hostedText{via: via, inner: inner}
	s.detection = inner.Detection
	s.joined = false
	return s
}

func (f *file) firstLine(str *syntax.String) string {
	text := f.src[str.Content.Start:str.Content.End]
	if nl := strings.IndexByte(text, '\n'); nl >= 0 && str.Kind == syntax.Indented && strings.TrimSpace(text[:nl]) == "" {
		text = text[nl+1:]
	}
	line, _, _ := strings.Cut(text, "\n")
	return line
}
