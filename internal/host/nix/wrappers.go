package nix

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

const (
	runtimeEnvAttr   = "runtimeEnv"
	derivationWriter = "mkDerivation"
	wholeArgument    = ""
	scriptHeadLines  = 4
)

var shellEscapes = []string{"escapeShellArg", "escapeShellArgs"}

type lambdaSinks struct {
	scripts  map[string]string
	envs     map[string]bool
	forwards []sinkForward
}

type sinkForward struct {
	from string
	call *syntax.Apply
	arg  int
	key  string
}

type sinkOwner struct {
	fn    *syntax.Lambda
	key   string
	whole bool
}

type sinkWalk struct {
	f      *file
	owners map[*scope.Def]sinkOwner
	out    map[*syntax.Lambda]*lambdaSinks
}

func (f *file) sinks() map[*syntax.Lambda]*lambdaSinks {
	w := &sinkWalk{f: f, owners: map[*scope.Def]sinkOwner{}, out: map[*syntax.Lambda]*lambdaSinks{}}
	visit := func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Lambda:
			w.enter(n)
		case *syntax.Apply:
			w.apply(n)
		case *syntax.AttrSet:
			w.derivation(n)
		}
		return true
	}
	syntax.Inspect(f.tree.Root, visit)
	for _, stray := range f.tree.Stray {
		syntax.Inspect(stray, visit)
	}
	return w.out
}

func (w *sinkWalk) enter(fn *syntax.Lambda) {
	own := func(id *syntax.Ident, owner sinkOwner) {
		if def, ok := w.f.table.Binder(id); ok {
			w.owners[def] = owner
		}
	}
	switch p := fn.Param.(type) {
	case *syntax.IdentParam:
		own(p.Name, sinkOwner{fn: fn, whole: true})
	case *syntax.PatternParam:
		for _, formal := range p.Formals {
			own(formal.Name, sinkOwner{fn: fn, key: formal.Name.Name})
		}
		if p.Alias != nil {
			own(p.Alias, sinkOwner{fn: fn, whole: true})
		}
	}
}

func (w *sinkWalk) of(fn *syntax.Lambda) *lambdaSinks {
	s := w.out[fn]
	if s == nil {
		s = &lambdaSinks{scripts: map[string]string{}, envs: map[string]bool{}}
		w.out[fn] = s
	}
	return s
}

func (w *sinkWalk) read(e syntax.Expr) (sinkOwner, bool) {
	switch e := syntax.Unparenthesized(e).(type) {
	case *syntax.Ident:
		return w.ofIdent(e)
	case *syntax.Select:
		id, plain := syntax.Unparenthesized(e.Target).(*syntax.Ident)
		if !plain || len(e.Path) != 1 {
			return sinkOwner{}, false
		}
		owner, known := w.ofIdent(id)
		name, static := syntax.AttrName(e.Path[0])
		if !known || !owner.whole || !static {
			return sinkOwner{}, false
		}
		return sinkOwner{fn: owner.fn, key: name}, true
	}
	return sinkOwner{}, false
}

func (w *sinkWalk) ofIdent(id *syntax.Ident) (sinkOwner, bool) {
	ref, ok := w.f.table.Of(id)
	if !ok || ref.Kind != scope.Definition {
		return sinkOwner{}, false
	}
	owner, known := w.owners[ref.Def]
	return owner, known
}

func (w *sinkWalk) readInherit(stmt *syntax.Inherit, attr syntax.Attr) (sinkOwner, bool) {
	name, static := syntax.AttrName(attr)
	if !static {
		return sinkOwner{}, false
	}
	if stmt.From != nil {
		id, plain := syntax.Unparenthesized(stmt.From.Inner).(*syntax.Ident)
		if !plain {
			return sinkOwner{}, false
		}
		owner, known := w.ofIdent(id)
		if !known || !owner.whole {
			return sinkOwner{}, false
		}
		return sinkOwner{fn: owner.fn, key: name}, true
	}
	ref, ok := w.f.table.Inherited(attr)
	if !ok || ref.Kind != scope.Definition {
		return sinkOwner{}, false
	}
	owner, known := w.owners[ref.Def]
	return owner, known
}

func (w *sinkWalk) apply(call *syntax.Apply) {
	name := calleeName(syntax.Unparenthesized(call.Fn))
	switch {
	case positionalWriter(name):
		w.script(call.Args[len(call.Args)-1], name)
	case runner(name):
		if arg := callSurfaces[name].script.arg; len(call.Args) == arg {
			w.env(call.Args[arg-2])
			w.script(call.Args[arg-1], name)
		}
	case name == shellApp:
		w.attrSinks(call.Args[0], shellApp)
	case name == writeTextFile:
		w.textFile(call.Args[0])
	default:
		w.forwards(call)
	}
}

func (w *sinkWalk) attrSinks(arg syntax.Expr, writer string) {
	set, literal := syntax.Unparenthesized(arg).(*syntax.AttrSet)
	if !literal {
		return
	}
	w.bound(set, func(name string, value syntax.Expr, inherited sinkOwner, isInherit bool) {
		var sink func(syntax.Expr, string)
		switch name {
		case shellAppText:
			sink = w.script
		case runtimeEnvAttr:
			sink = func(e syntax.Expr, _ string) { w.env(e) }
		default:
			return
		}
		if isInherit {
			w.markScriptOrEnv(name, inherited, writer)
			return
		}
		sink(value, writer)
	})
}

func (w *sinkWalk) markScriptOrEnv(attr string, owner sinkOwner, writer string) {
	if attr == runtimeEnvAttr || attr == "env" {
		w.of(owner.fn).envs[owner.key] = true
		return
	}
	w.of(owner.fn).scripts[owner.key] = writer
}

func (w *sinkWalk) bound(set *syntax.AttrSet, visit func(name string, value syntax.Expr, inherited sinkOwner, isInherit bool)) {
	for _, b := range set.Bindings {
		switch b := b.(type) {
		case *syntax.Assign:
			if name, ok := syntax.AttrName(b.Path[0]); ok && len(b.Path) == 1 {
				visit(name, b.Value, sinkOwner{}, false)
			}
		case *syntax.Inherit:
			for _, attr := range b.Attrs {
				name, ok := syntax.AttrName(attr)
				if !ok {
					continue
				}
				if owner, known := w.readInherit(b, attr); known {
					visit(name, nil, owner, true)
				}
			}
		}
	}
}

func (w *sinkWalk) textFile(arg syntax.Expr) {
	set, literal := syntax.Unparenthesized(arg).(*syntax.AttrSet)
	if !literal {
		return
	}
	w.bound(set, func(name string, value syntax.Expr, _ sinkOwner, isInherit bool) {
		if name != shellAppText || isInherit {
			return
		}
		str, isString := syntax.Unparenthesized(value).(*syntax.String)
		if !isString {
			return
		}
		if writer, shell := w.shebangWriter(str); shell {
			w.script(str, writer)
		}
	})
}

func (w *sinkWalk) shebangWriter(str *syntax.String) (string, bool) {
	first := strings.TrimSpace(w.f.firstLine(str))
	if !strings.HasPrefix(first, "#!") {
		return "", false
	}
	for _, marker := range []string{"zsh", "dash", "ksh"} {
		if strings.Contains(first, marker) {
			return marker, true
		}
	}
	for _, marker := range []string{"bash", "/sh", "runtimeShell", "stdenv.shell"} {
		if strings.Contains(first, marker) {
			return "bash", true
		}
	}
	return "", false
}

func (w *sinkWalk) derivation(set *syntax.AttrSet) {
	phased := slices.ContainsFunc(set.Bindings, func(b syntax.Binding) bool {
		a, assigned := b.(*syntax.Assign)
		return assigned && (slices.Contains(derivationBindings, lastIdent(a.Path)) || lastIdent(a.Path) == shellHook)
	})
	w.bound(set, func(name string, value syntax.Expr, inherited sinkOwner, isInherit bool) {
		switch {
		case slices.Contains(derivationBindings, name) || name == shellHook:
			if isInherit {
				w.markScriptOrEnv(name, inherited, derivationWriter)
				return
			}
			w.script(value, derivationWriter)
		case name == envChildren[derivationEnv] && phased:
			if isInherit {
				w.markScriptOrEnv(name, inherited, derivationWriter)
				return
			}
			w.env(value)
		}
	})
}

func (w *sinkWalk) script(e syntax.Expr, writer string) {
	if owner, read := w.read(e); read {
		w.of(owner.fn).scripts[owner.key] = writer
		return
	}
	switch e := syntax.Unparenthesized(e).(type) {
	case *syntax.String:
		w.scriptHoles(e, writer)
	case *syntax.Binary:
		if e.Op == syntax.OpAdd {
			w.script(e.Left, writer)
			w.script(e.Right, writer)
		}
	case *syntax.If:
		w.script(e.Then, writer)
		w.script(e.Else, writer)
	case *syntax.Apply:
		if stringWrap(calleeName(syntax.Unparenthesized(e.Fn))) {
			w.script(e.Args[len(e.Args)-1], writer)
		}
	}
}

func (w *sinkWalk) scriptHoles(str *syntax.String, writer string) {
	for _, part := range str.Parts {
		hole, ok := part.(*syntax.Interpolation)
		if !ok || !w.commandHole(str, hole) {
			continue
		}
		value := syntax.Unparenthesized(hole.Value)
		if escaped, isCall := value.(*syntax.Apply); isCall && slices.Contains(shellEscapes, calleeName(syntax.Unparenthesized(escaped.Fn))) {
			value = escaped.Args[len(escaped.Args)-1]
		}
		if owner, read := w.read(value); read {
			w.of(owner.fn).scripts[owner.key] = writer
		}
	}
}

func (w *sinkWalk) commandHole(str *syntax.String, hole *syntax.Interpolation) bool {
	span := hole.Span()
	start := max(strings.LastIndexByte(w.f.src[:span.Start], '\n')+1, str.Content.Start)
	before := w.f.src[start:span.Start]
	if strings.HasSuffix(strings.TrimRight(before, " \t"), " -c") {
		return true
	}
	after, _, _ := strings.Cut(w.f.src[span.End:str.Content.End], "\n")
	return strings.TrimSpace(before) == "" && strings.TrimSpace(after) == ""
}

func (w *sinkWalk) env(e syntax.Expr) {
	if owner, read := w.read(e); read {
		w.of(owner.fn).envs[owner.key] = true
		return
	}
	if update, ok := syntax.Unparenthesized(e).(*syntax.Binary); ok && update.Op == syntax.OpUpdate {
		w.env(update.Left)
		w.env(update.Right)
	}
}

func (w *sinkWalk) forwards(call *syntax.Apply) {
	for i, arg := range call.Args {
		if owner, read := w.read(arg); read {
			s := w.of(owner.fn)
			s.forwards = append(s.forwards, sinkForward{from: owner.key, call: call, arg: i, key: wholeArgument})
			continue
		}
		set, literal := syntax.Unparenthesized(arg).(*syntax.AttrSet)
		if !literal {
			continue
		}
		w.bound(set, func(name string, value syntax.Expr, inherited sinkOwner, isInherit bool) {
			owner, read := inherited, isInherit
			if !isInherit {
				owner, read = w.read(value)
			}
			if read {
				s := w.of(owner.fn)
				s.forwards = append(s.forwards, sinkForward{from: owner.key, call: call, arg: i, key: name})
			}
		})
	}
}

func (b *symbolTable) wrapperCandidates() []*syntax.Apply {
	var out []*syntax.Apply
	for _, call := range b.calls {
		name := calleeName(syntax.Unparenthesized(call.Fn))
		if knownWriter(name) || slices.Contains(transparentCalls, name) || name == importName || name == mkMergeName {
			continue
		}
		if slices.ContainsFunc(call.Args, stringBearing) {
			out = append(out, call)
		}
	}
	return out
}

func stringBearing(arg syntax.Expr) bool {
	switch arg := syntax.Unparenthesized(arg).(type) {
	case *syntax.String:
		return true
	case *syntax.AttrSet:
		return slices.ContainsFunc(arg.Bindings, func(b syntax.Binding) bool {
			a, assigned := b.(*syntax.Assign)
			return assigned && len(a.Path) == 1 && len(scriptStrings(a.Value)) > 0
		})
	}
	return false
}

func scriptStrings(e syntax.Expr) []*syntax.String {
	switch e := syntax.Unparenthesized(e).(type) {
	case *syntax.String:
		return []*syntax.String{e}
	case *syntax.Binary:
		if e.Op == syntax.OpAdd {
			return slices.Concat(scriptStrings(e.Left), scriptStrings(e.Right))
		}
	case *syntax.If:
		return slices.Concat(scriptStrings(e.Then), scriptStrings(e.Else))
	case *syntax.Apply:
		if stringWrap(calleeName(syntax.Unparenthesized(e.Fn))) {
			return scriptStrings(e.Args[len(e.Args)-1])
		}
	}
	return nil
}

type provenString struct {
	str    *syntax.String
	writer string
	env    environment
}

type sinkKey struct {
	fn  flowLambda
	key string
}

func writerArgument(name string, arg int, key string) (string, bool, bool) {
	switch {
	case name == shellApp && arg == 0 && key == shellAppText:
		return shellApp, true, false
	case name == shellApp && arg == 0 && key == runtimeEnvAttr:
		return "", false, true
	case runner(name) && arg == callSurfaces[name].script.arg-1 && key == wholeArgument:
		return name, true, false
	case runner(name) && arg == callSurfaces[name].script.arg-2 && key == wholeArgument:
		return "", false, true
	case positionalWriter(name) && arg >= 1 && key == wholeArgument:
		return name, true, false
	}
	return "", false, false
}

func (p *Project) scriptSink(fn flowLambda, key string) (string, bool) {
	k := sinkKey{fn: fn, key: key}
	if done, ok := p.scriptMemo[k]; ok {
		return done, done != ""
	}
	if p.sinkVisiting[k] {
		return "", false
	}
	p.sinkVisiting[k] = true
	defer delete(p.sinkVisiting, k)
	writer := ""
	if s := fn.unit.sinks[fn.fn]; s != nil {
		writer = s.scripts[key]
		for _, f := range s.forwards {
			if writer != "" {
				break
			}
			if f.from == key {
				writer = p.forwardedScript(fn.unit, f)
			}
		}
	}
	p.scriptMemo[k] = writer
	return writer, writer != ""
}

func (p *Project) forwardedScript(u *symbolTable, f sinkForward) string {
	for _, v := range p.calleeAt(u, f.call, f.arg) {
		switch v := v.(type) {
		case flowLambda:
			if writer, ok := p.scriptSink(v, f.key); ok {
				return writer
			}
		case flowWriter:
			if writer, script, _ := writerArgument(v.name, f.arg, f.key); script {
				return writer
			}
		}
	}
	return ""
}

func (p *Project) envSink(fn flowLambda, key string) bool {
	k := sinkKey{fn: fn, key: key}
	if done, ok := p.envMemo[k]; ok {
		return done
	}
	if p.sinkVisiting[k] {
		return false
	}
	p.sinkVisiting[k] = true
	defer delete(p.sinkVisiting, k)
	found := p.envForwarded(fn, key)
	p.envMemo[k] = found
	return found
}

func (p *Project) envForwarded(fn flowLambda, key string) bool {
	s := fn.unit.sinks[fn.fn]
	if s == nil {
		return false
	}
	if s.envs[key] {
		return true
	}
	for _, f := range s.forwards {
		if f.from != key {
			continue
		}
		for _, v := range p.calleeAt(fn.unit, f.call, f.arg) {
			switch v := v.(type) {
			case flowLambda:
				if p.envSink(v, f.key) {
					return true
				}
			case flowWriter:
				if _, _, env := writerArgument(v.name, f.arg, f.key); env {
					return true
				}
			}
		}
	}
	return false
}

func (p *Project) proveCall(u *symbolTable, call *syntax.Apply) []provenString {
	if done, ok := p.provedCalls[call]; ok {
		return done
	}
	p.provedCalls[call] = nil
	values := p.eval(u, syntax.Unparenthesized(call.Fn))
	var out []provenString
	env := environment{}
	for i, arg := range call.Args {
		for _, v := range values {
			found, given := p.proveArgument(v, i, arg)
			out = append(out, found...)
			if given.set != nil || given.expr != nil {
				env = given
			}
		}
		values = p.applied(values, 1)
	}
	for i := range out {
		out[i].env = env
	}
	p.provedCalls[call] = out
	return out
}

func (p *Project) sinksOfValue(v flowValue, arg int, key string) (string, bool) {
	switch v := v.(type) {
	case flowLambda:
		writer, script := p.scriptSink(v, key)
		return writer, script
	case flowWriter:
		writer, script, _ := writerArgument(v.name, arg, key)
		return writer, script
	}
	return "", false
}

func (p *Project) envOfValue(v flowValue, arg int, key string) bool {
	switch v := v.(type) {
	case flowLambda:
		return p.envSink(v, key)
	case flowWriter:
		_, _, env := writerArgument(v.name, arg, key)
		return env
	}
	return false
}

func (p *Project) proveArgument(v flowValue, i int, arg syntax.Expr) ([]provenString, environment) {
	set, literal := syntax.Unparenthesized(arg).(*syntax.AttrSet)
	if !literal {
		var out []provenString
		if writer, script := p.sinksOfValue(v, i, wholeArgument); script {
			for _, str := range scriptStrings(arg) {
				out = append(out, provenString{str: str, writer: writer})
			}
		}
		if p.envOfValue(v, i, wholeArgument) {
			return out, environment{source: wrapperEnv, expr: arg}
		}
		return out, environment{}
	}
	var out []provenString
	env := environment{}
	for _, b := range set.Bindings {
		a, assigned := b.(*syntax.Assign)
		if !assigned {
			continue
		}
		key, static := syntax.AttrName(a.Path[0])
		if !static {
			continue
		}
		if writer, script := p.sinksOfValue(v, i, key); script && len(a.Path) == 1 {
			for _, str := range scriptStrings(a.Value) {
				out = append(out, provenString{str: str, writer: writer})
			}
		}
		if p.envOfValue(v, i, key) {
			env = environment{set: set, source: wrapperEnv, child: key}
		}
	}
	return out, env
}

func (p *Project) provenIn(file int) []provenString {
	if done, ok := p.provenFiles[file]; ok {
		return done
	}
	u := p.units[file]
	var out []provenString
	for _, call := range u.candidates {
		for _, s := range p.proveCall(u, call) {
			if !u.ownsShell(s.str) {
				out = append(out, s)
			}
		}
	}
	slices.SortFunc(out, func(a, b provenString) int { return cmp.Compare(a.str.Span().Start, b.str.Span().Start) })
	out = slices.CompactFunc(out, func(a, b provenString) bool { return a.str == b.str })
	p.provenFiles[file] = out
	return out
}

func (b *symbolTable) ownsShell(str *syntax.String) bool {
	return slices.ContainsFunc(b.ix.Shells, func(s model.Shell) bool {
		return str.Content.Start <= s.Span.Start && s.Span.End <= str.Content.End
	})
}

func (p *Project) Shells(file int) []model.Shell {
	p.begin()
	defer p.mu.Unlock()
	var out []model.Shell
	for _, s := range p.provenIn(file) {
		at := provenKey{file: file, str: s.str}
		id, ok := p.provenScopes[at]
		if !ok {
			p.nextShared--
			id = p.nextShared
			p.provenScopes[at] = id
			p.provenEnvs[model.Home{File: file, Scope: id}] = s.env
		}
		phase := 0
		if runner(s.writer) {
			phase = phaseRank("buildCommand")
		}
		for _, piece := range p.units[file].f.pieces(s.str) {
			out = append(out, model.Shell{Span: piece, Scope: id, Phase: phase})
		}
	}
	return out
}

type provenKey struct {
	file int
	str  *syntax.String
}

func (f *file) discoverWrapped() {
	if len(f.sinks()) == 0 && !f.aliasesWriter() {
		return
	}
	ix := &model.Index{}
	for _, s := range f.found {
		for _, piece := range f.pieces(s.str) {
			ix.Shells = append(ix.Shells, model.Shell{Span: piece})
		}
	}
	slices.SortFunc(ix.Shells, func(a, b model.Shell) int { return cmp.Compare(a.Span.Start, b.Span.Start) })
	u := newSymbolTable(f, ix)
	u.build()
	p := standalone(ix)
	proven := p.provenIn(0)
	if len(proven) == 0 {
		return
	}
	for _, s := range proven {
		if found, ok := f.provenShell(s); ok {
			f.found = append(f.found, found)
		}
	}
	f.found = outermost(f.found)
}

func (f *file) aliasesWriter() bool {
	found := false
	visit := func(n syntax.Node) bool {
		if a, assigned := n.(*syntax.Assign); assigned && knownWriter(calleeName(syntax.Unparenthesized(a.Value))) {
			found = true
		}
		return !found
	}
	syntax.Inspect(f.tree.Root, visit)
	for _, stray := range f.tree.Stray {
		syntax.Inspect(stray, visit)
	}
	return found
}

func (f *file) provenShell(s provenString) (shellString, bool) {
	found, ok := f.written(shellString{str: s.str}, call{name: s.writer})
	if !ok {
		return shellString{}, false
	}
	if s.writer == shellApp {
		found.prelude = strictMode
	}
	if s.env.set != nil || s.env.expr != nil {
		found.env = s.env
		if !found.derivation {
			found.directive = phaseDirective
		}
	}
	return found, true
}

func outermost(found []shellString) []shellString {
	slices.SortStableFunc(found, func(a, b shellString) int { return cmp.Compare(a.str.Span().Start, b.str.Span().Start) })
	end := 0
	return slices.DeleteFunc(found, func(s shellString) bool {
		if s.str.Span().Start < end {
			return true
		}
		end = s.str.Span().End
		return false
	})
}

type ProvenString struct {
	Start  int
	Writer string
}

func (p *Project) Proven(file int) []ProvenString {
	p.begin()
	defer p.mu.Unlock()
	proven := p.provenIn(file)
	out := make([]ProvenString, len(proven))
	for i, s := range proven {
		out[i] = ProvenString{Start: s.str.Span().Start, Writer: s.writer}
	}
	return out
}

func (p *Project) FileOf(path string, ix *model.Index) (int, bool) {
	i, known := p.byPath[path]
	return i, known && p.units[i].ix == ix
}

func MayWrap(ix *model.Index) bool {
	r, ok := ix.Renames.(renames)
	if !ok {
		panic("nix: this index was not analyzed as Nix")
	}
	return len(r.unit.candidates) > 0
}

func RebasedStrings(proven []ProvenString, before, after string) []ProvenString {
	limit := min(len(before), len(after))
	prefix := 0
	for prefix < limit && before[prefix] == after[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < limit-prefix && before[len(before)-1-suffix] == after[len(after)-1-suffix] {
		suffix++
	}
	out := make([]ProvenString, 0, len(proven))
	for _, s := range proven {
		switch {
		case s.Start < prefix:
			out = append(out, s)
		case s.Start >= len(before)-suffix:
			s.Start += len(after) - len(before)
			out = append(out, s)
		}
	}
	return out
}

func ProjectedStrings(src string, proven []ProvenString) (Analysis, bool) {
	writers := make(map[int]string, len(proven))
	for _, s := range proven {
		writers[s.Start] = s.Writer
	}
	tree := syntax.Parse(src)
	f := &file{src: src, tree: tree, table: scope.Resolve(tree)}
	visit := func(n syntax.Node) bool {
		str, isString := n.(*syntax.String)
		if !isString {
			return true
		}
		writer, wanted := writers[str.Span().Start]
		if !wanted {
			return true
		}
		if found, ok := f.provenShell(provenString{str: str, writer: writer}); ok {
			if !found.derivation {
				found.directive = phaseDirective
			}
			f.found = append(f.found, found)
		}
		return true
	}
	syntax.Inspect(tree.Root, visit)
	for _, stray := range tree.Stray {
		syntax.Inspect(stray, visit)
	}
	if len(f.found) == 0 {
		return Analysis{}, false
	}
	f.found = outermost(f.found)
	m, detection := f.projection()
	index := &model.Index{}
	for _, s := range f.found {
		index.Holes = append(index.Holes, f.holes(s.str)...)
	}
	return Analysis{Map: m, Detection: detection, Index: index}, true
}

func scriptOf(u *symbolTable, call *syntax.Apply) model.Script {
	var name string
	var text syntax.Expr
	for _, arg := range call.Args {
		switch arg := syntax.Unparenthesized(arg).(type) {
		case *syntax.AttrSet:
			for _, b := range arg.Bindings {
				a, assigned := b.(*syntax.Assign)
				if !assigned || len(a.Path) != 1 {
					continue
				}
				switch lastIdent(a.Path) {
				case "name":
					if str, isString := syntax.Unparenthesized(a.Value).(*syntax.String); isString {
						name = literalOf(str)
					}
				case shellAppText:
					text = a.Value
				}
			}
		default:
			if str, isString := text.(*syntax.String); isString && name == "" {
				name = literalOf(str)
			}
			text = arg
		}
	}
	script := model.Script{Name: name, Fence: scriptFence(calleeName(syntax.Unparenthesized(call.Fn)))}
	if text == nil {
		return script
	}
	span := syntax.Unparenthesized(text).Span()
	if str, isString := syntax.Unparenthesized(text).(*syntax.String); isString {
		span = str.Content
	}
	var head []string
	for line := range strings.SplitSeq(u.f.src[span.Start:span.End], "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			head = append(head, trimmed)
		}
		if len(head) == scriptHeadLines {
			break
		}
	}
	script.Head = strings.Join(head, "\n")
	return script
}

func literalOf(str *syntax.String) string {
	if len(str.Parts) != 1 {
		return ""
	}
	literal, plain := str.Parts[0].(*syntax.Literal)
	if !plain {
		return ""
	}
	return literal.Text
}

func (p *Project) Script(file, symbol int) (model.Script, bool) {
	p.begin()
	defer p.mu.Unlock()
	u := p.units[file]
	o := u.owners[symbol]
	var values []flowValue
	switch {
	case o.def != nil:
		values = p.evalDef(u, o.def)
	case o.set != nil:
		values = p.selectFrom(flowSet{unit: u, set: o.set}, u.ix.Symbols[symbol].Name)
	}
	for _, v := range values {
		if script, ok := v.(flowScript); ok {
			return scriptOf(script.unit, script.call), true
		}
	}
	return model.Script{}, false
}

func overlayPath(path syntax.AttrPath) bool {
	return slices.ContainsFunc(path, func(a syntax.Attr) bool {
		name, ok := syntax.AttrName(a)
		return ok && name == overlaysAttr
	})
}
