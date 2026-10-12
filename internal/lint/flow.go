package lint

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const (
	maxLoopRounds = 3
	batsStatus    = "status"
)

type space uint8

const (
	spaceEmpty space = 1 << iota
	spaceClean
	spaceDirty
	spaceEntry
)

func (s space) clean() bool { return s == spaceClean }

func (s space) normalize() space {
	entry := s & spaceEntry
	switch {
	case s&spaceDirty != 0:
		return spaceDirty | entry
	case s&spaceClean != 0:
		return spaceClean | entry
	}
	return s
}

func appendSpace(a, b space) space {
	switch {
	case a == 0:
		return b
	case b == 0:
		return a
	}
	var out space
	for _, x := range []space{spaceEmpty, spaceClean, spaceDirty, spaceEntry} {
		if a&x == 0 {
			continue
		}
		for _, y := range []space{spaceEmpty, spaceClean, spaceDirty, spaceEntry} {
			if b&y != 0 {
				out |= appendOne(x, y)
			}
		}
	}
	return out
}

func appendOne(a, b space) space {
	switch {
	case a == spaceEmpty:
		return b
	case b == spaceEmpty:
		return a
	case a == spaceClean && b == spaceClean:
		return spaceClean
	}
	return spaceDirty
}

type integerAttr uint8

const (
	integerYes integerAttr = 1 << iota
	integerNo
	integerEntry
)

type value struct {
	space        space
	integer      integerAttr
	holdsInteger integerAttr
}

func (v value) join(o value) value {
	space := (v.space | o.space).normalize()
	if v.space == 0 || o.space == 0 {
		space = 0
	}
	return value{space: space, integer: v.integer | o.integer, holdsInteger: v.holdsInteger | o.holdsInteger}
}

func (v value) substitute(entry value) value {
	out := value{space: v.space &^ spaceEntry, integer: v.integer &^ integerEntry, holdsInteger: v.holdsInteger &^ integerEntry}
	if v.space&spaceEntry != 0 {
		out.space = (out.space | entry.space).normalize()
	}
	if v.integer&integerEntry != 0 {
		out.integer |= entry.integer
	}
	if v.holdsInteger&integerEntry != 0 {
		out.holdsInteger |= entry.holdsInteger
	}
	return out
}

func (v value) expands() space {
	switch {
	case v.integer == integerYes:
		return spaceClean
	case v.space == 0:
		return spaceDirty
	}
	return v.space
}

type funcRef struct {
	decl  *syntax.FuncDecl
	maybe bool
}

type subshellKind uint8

const (
	notSubshell subshellKind = iota
	subshellGroup
	subshellPipeline
	subshellSubstitution
	subshellBackticks
	subshellBackground
	subshellCoproc
	subshellBats
	subshellProcess
	subshellScript
)

func (k subshellKind) label() string {
	switch k {
	case subshellGroup:
		return "( ) group"
	case subshellPipeline:
		return "pipeline"
	case subshellSubstitution:
		return "command substitution"
	case subshellBackticks:
		return "backtick expansion"
	case subshellBackground:
		return "background job"
	case subshellCoproc:
		return "coprocess"
	case subshellBats:
		return "Bats test"
	case subshellProcess:
		return "process substitution"
	case subshellScript:
		return "separate shell"
	case notSubshell:
	}
	panic(fmt.Sprintf("lint: subshell kind %d has no label", k))
}

func (k subshellKind) implicit() bool {
	return k == subshellSubstitution || k == subshellBackticks || k == subshellPipeline
}

type lostWrite struct {
	site parser.Span
	kind subshellKind
}

func (w lostWrite) later(o lostWrite) lostWrite {
	if o.site.Start > w.site.Start {
		return o
	}
	return w
}

type subshellWrite struct {
	name string
	site parser.Span
}

type state struct {
	live     bool
	symbolic bool
	vars     pmap[value]
	funcs    pmap[funcRef]
	status   statusSet
	pending  pmap[*callList]
	lost     pmap[lostWrite]
}

func newState(symbolic bool) *state {
	return &state{live: true, symbolic: symbolic}
}

func deadState(symbolic bool) *state {
	s := newState(symbolic)
	s.live = false
	return s
}

func (s *state) clone() *state {
	out := *s
	return &out
}

func (s *state) get(name string) value {
	if v, ok := s.vars.get(name); ok {
		return v
	}
	if s.symbolic {
		return value{space: spaceEntry, integer: integerEntry, holdsInteger: integerEntry}
	}
	return environment(name)
}

func (s *state) set(name string, v value) { s.vars = s.vars.set(name, v) }

func (s *state) lose(name string, w lostWrite) {
	if prev, ok := s.lost.get(name); ok {
		w = prev.later(w)
	}
	s.lost = s.lost.set(name, w)
}

func (s *state) forget(name string) { s.lost = s.lost.remove(name) }

func join(a, b *state) *state {
	switch {
	case !a.live:
		return b.clone()
	case !b.live:
		return a.clone()
	}
	out := a.clone()
	out.status = a.status.union(b.status)
	out.pending = unionPending(a.pending, b.pending)
	a.vars.diff(b.vars, func(name string) {
		out.set(name, a.get(name).join(b.get(name)))
	})
	a.lost.diff(b.lost, func(name string) {
		if w, ok := b.lost.get(name); ok {
			out.lose(name, w)
		}
	})
	a.funcs.diff(b.funcs, func(name string) {
		mine, inA := a.funcs.get(name)
		theirs, inB := b.funcs.get(name)
		switch {
		case !inB:
			out.funcs = out.funcs.set(name, funcRef{decl: mine.decl, maybe: true})
		case !inA || mine.decl != theirs.decl:
			out.funcs = out.funcs.set(name, funcRef{decl: theirs.decl, maybe: true})
		default:
			out.funcs = out.funcs.set(name, funcRef{decl: theirs.decl, maybe: mine.maybe || theirs.maybe})
		}
	})
	return out
}

func (s *state) equal(o *state) bool {
	if s.live != o.live || !slices.Equal(s.status, o.status) {
		return false
	}
	same := true
	s.pending.diff(o.pending, func(string) { same = false })
	s.vars.diff(o.vars, func(string) { same = false })
	s.funcs.diff(o.funcs, func(string) { same = false })
	s.lost.diff(o.lost, func(string) { same = false })
	return same
}

func environment(name string) value {
	if spacelessVariables[name] {
		return value{space: spaceClean, integer: integerNo}
	}
	return value{integer: integerNo}
}

var spacelessVariables = setOf(
	"$", "?", "!", "#", "-",
	"BASHPID", "BASH_ARGC", "BASH_LINENO", "BASH_SUBSHELL", "EUID", "EPOCHREALTIME", "EPOCHSECONDS",
	"LINENO", "OPTIND", "PPID", "RANDOM", "READLINE_ARGUMENT", "READLINE_MARK", "READLINE_POINT",
	"SECONDS", "SHELLOPTS", "SHLVL", "SRANDOM", "UID", "COLUMNS", "HISTFILESIZE", "HISTSIZE", "LINES",
	"BASH_MONOSECONDS", "BASH_TRAPSIG",
)

func setOf(names ...string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

type flowResult struct {
	expansions map[*syntax.ParamExp]space
	reached    map[*syntax.Stmt]bool
	called     map[*syntax.FuncDecl]bool
	lostReads  map[syntax.Node]lostWrite
	flowFacts
}

func (r *flowResult) expansion(pe *syntax.ParamExp) space {
	if r == nil {
		return spaceDirty
	}
	if s, ok := r.expansions[pe]; ok {
		return s
	}
	return spaceDirty
}

func (r *flowResult) lostRead(read syntax.Node) (lostWrite, bool) {
	if r == nil {
		return lostWrite{}, false
	}
	w, ok := r.lostReads[read]
	return w, ok
}

type loopFrame struct {
	breaks    *state
	continues *state
}

type funcFrame struct {
	locals  map[string]*value
	returns *state
}

type summary struct {
	exit *state
}

type interpreter struct {
	tree       *parser.Tree
	dialect    dialect.Dialect
	writes     nodeWrites
	imported   map[string]value
	result     *flowResult
	record     bool
	loops      []*loopFrame
	frames     []*funcFrame
	summaries  map[*syntax.FuncDecl]*summary
	computing  map[*syntax.FuncDecl]bool
	globals    pmap[value]
	staticFunc map[string]*syntax.FuncDecl
	final      *state
	bats       bool
	walk       []syntax.Node
	deferred   []syntax.Node
	eval       evaluation
	visitor    func(syntax.Node) bool
	lastPipe   bool
	subshells  [][]subshellWrite

	trackStatus   bool
	trackCalls    bool
	unreachable   bool
	definitions   map[*syntax.FuncDecl]bool
	subshellExits []pmap[*callList]
	subshellDepth int
	sourceReturns *state
}

func analyzeFlow(tree *parser.Tree, imports []parser.Import, bats bool) *flowResult {
	in := &interpreter{
		bats:       bats,
		tree:       tree,
		dialect:    tree.Dialect,
		writes:     indexWrites(tree),
		imported:   importedGlobals(imports),
		result:     &flowResult{expansions: map[*syntax.ParamExp]space{}, reached: make(map[*syntax.Stmt]bool, tree.Statements), called: map[*syntax.FuncDecl]bool{}, lostReads: map[syntax.Node]lostWrite{}, flowFacts: newFlowFacts()},
		summaries:  map[*syntax.FuncDecl]*summary{},
		computing:  map[*syntax.FuncDecl]bool{},
		staticFunc: map[string]*syntax.FuncDecl{},
		lastPipe:   runsLastPipeInShell(tree),
	}
	in.visitor = in.visit
	for _, fn := range tree.Functions {
		if _, seen := in.staticFunc[fn.Name]; !seen && fn.Decl != nil {
			in.staticFunc[fn.Name] = fn.Decl
		}
	}
	in.trackStatus = readsStatus(tree.Text())
	in.trackCalls = len(tree.Functions) > 0
	in.definitions = map[*syntax.FuncDecl]bool{}
	in.record = true
	in.sourceReturns = deadState(false)
	in.final = join(in.stmts(tree.File.Stmts, newState(false)), in.sourceReturns)
	if in.final.live {
		in.escape(in.final)
	}
	in.runFunctionBodies()
	return in.result
}

func (in *interpreter) runFunctionBodies() {
	done := make(map[*syntax.FuncDecl]bool, len(in.tree.Functions))
	for progress := true; progress; {
		progress = false
		for _, fn := range in.tree.Functions {
			if fn.Decl == nil || fn.Decl.Body == nil || done[fn.Decl] || !in.invokable(fn.Decl) {
				continue
			}
			done[fn.Decl] = true
			progress = true
			in.runBody(fn.Decl, in.entry())
		}
	}
	in.unreachable = true
	for _, fn := range in.tree.Functions {
		if fn.Decl == nil || fn.Decl.Body == nil || done[fn.Decl] {
			continue
		}
		in.result.deadFunctions[fn.Decl] = true
		in.runBody(fn.Decl, in.entry())
	}
	in.unreachable = false
}

func (in *interpreter) invokable(decl *syntax.FuncDecl) bool {
	if in.result.called[decl] {
		return true
	}
	if !in.final.live {
		return false
	}
	return slices.ContainsFunc(declaredNames(decl), func(name *syntax.Lit) bool {
		ref, ok := in.final.funcs.get(name.Value)
		return ok && (ref.decl == decl || (ref.maybe && in.definitions[decl]))
	})
}

func declaredNames(decl *syntax.FuncDecl) []*syntax.Lit {
	if decl.Name != nil {
		return []*syntax.Lit{decl.Name}
	}
	return decl.Names
}

func importedGlobals(imports []parser.Import) map[string]value {
	out := map[string]value{}
	for _, imp := range imports {
		for _, def := range imp.Tree.Definitions {
			if def.Local {
				continue
			}
			v := value{space: spaceDirty, integer: integerNo}
			if def.Value != nil {
				v = value{space: literalWordSpace(def.Value), integer: integerNo}
			}
			if prev, ok := out[def.Name]; ok {
				v = prev.join(v)
			}
			out[def.Name] = v
		}
	}
	return out
}

func literalWordSpace(w *syntax.Word) space {
	lit, ok := parser.Literal(w)
	if !ok {
		return spaceDirty
	}
	return literalSpace(lit)
}

func literalSpace(s string) space {
	switch {
	case s == "":
		return spaceEmpty
	case strings.ContainsAny(s, " \t\n*?["):
		return spaceDirty
	}
	return spaceClean
}

func runsLastPipeInShell(tree *parser.Tree) bool {
	if tree.Dialect.Features().LastPipeInShell {
		return true
	}
	return tree.Dialect == dialect.Bash && scanShellOptions(tree).lastpipe
}

func (in *interpreter) entry() *state {
	return &state{live: true, vars: in.globals, funcs: in.final.funcs}
}

func (in *interpreter) assign(s *state, name string, v value) {
	s.set(name, v)
	s.forget(name)
	if s.symbolic {
		return
	}
	if prev, ok := in.globals.get(name); ok {
		v = prev.join(v)
	}
	in.globals = in.globals.set(name, v)
}

func (in *interpreter) stmts(list []*syntax.Stmt, s *state) *state {
	for _, stmt := range list {
		s = in.stmt(stmt, s)
	}
	return s
}

func (in *interpreter) stmt(stmt *syntax.Stmt, s *state) *state {
	if !s.live {
		return s
	}
	if in.record && !s.symbolic && !in.unreachable {
		in.result.reached[stmt] = true
	}
	if stmt.Background || stmt.Coprocess {
		kind := subshellBackground
		if stmt.Coprocess {
			kind = subshellCoproc
		}
		in.subshell(kind, func(sub *state) *state { return in.command(stmt, sub) }, s)
		in.setStatus(s, stmt)
		return s
	}
	return in.command(stmt, s)
}

func (in *interpreter) subshell(kind subshellKind, run func(*state) *state, s *state) *state {
	saved := in.loops
	in.loops = nil
	in.subshellDepth++
	if in.trackCalls {
		in.subshellExits = append(in.subshellExits, pmap[*callList]{})
	}
	in.subshells = append(in.subshells, nil)
	end := run(s.clone())
	in.loseWrites(kind, s)
	in.subshellDepth--
	in.loops = saved
	if in.trackCalls {
		exits := in.subshellExits[len(in.subshellExits)-1]
		in.subshellExits = in.subshellExits[:len(in.subshellExits)-1]
		if end.live {
			exits = unionPending(exits, end.pending)
		}
		s.pending = unionPending(s.pending, exits)
	}
	return end
}

func (in *interpreter) loseWrites(kind subshellKind, s *state) {
	top := len(in.subshells) - 1
	if kind != subshellProcess {
		for _, w := range in.subshells[top] {
			s.lose(w.name, lostWrite{site: w.site, kind: kind})
		}
	}
	in.subshells = in.subshells[:top]
}

func (in *interpreter) noteWrite(name string, site parser.Span) {
	if top := len(in.subshells) - 1; top >= 0 && name != "IFS" {
		in.subshells[top] = append(in.subshells[top], subshellWrite{name: name, site: site})
	}
}

func writeSite(def *parser.Definition) parser.Span {
	switch n := def.Node.(type) {
	case *syntax.BinaryArithm:
		return parser.Span{Start: parser.Offset(n.OpPos), End: def.Span.End}
	case *syntax.UnaryArithm, *syntax.CallExpr:
		return def.NameSpan
	}
	return def.Span
}

func (in *interpreter) command(stmt *syntax.Stmt, s *state) *state {
	switch cmd := stmt.Cmd.(type) {
	case nil:
		s = in.evaluate(stmt, s)
		in.setStatus(s, stmt)
	case *syntax.CallExpr:
		s = in.call(stmt, cmd, s)
		in.setStatus(s, stmt)
	case *syntax.DeclClause:
		s = in.evaluate(stmt, s)
		in.setStatus(s, stmt)
	case *syntax.IfClause:
		s = in.ifClause(cmd, s)
		s = in.redirects(stmt, s)
	case *syntax.WhileClause:
		s = in.whileClause(cmd, s)
		s = in.redirects(stmt, s)
	case *syntax.ForClause:
		s = in.forClause(cmd, s)
		s = in.redirects(stmt, s)
	case *syntax.RepeatClause:
		s = in.repeatClause(cmd, s)
		s = in.redirects(stmt, s)
	case *syntax.TryClause:
		s = in.tryClause(cmd, s)
		s = in.redirects(stmt, s)
	case *syntax.NamespaceClause:
		s = in.stmts(cmd.Body.Stmts, s)
		s = in.redirects(stmt, s)
	case *syntax.CaseClause:
		s = in.caseClause(cmd, s)
		s = in.redirects(stmt, s)
	case *syntax.Block:
		s = in.stmts(cmd.Stmts, s)
		s = in.redirects(stmt, s)
	case *syntax.Subshell:
		kind := subshellGroup
		if in.tree.Synthetic(parser.Offset(cmd.Lparen)) {
			kind = subshellScript
		}
		if end := in.subshell(kind, func(sub *state) *state { return in.stmts(cmd.Stmts, sub) }, s); end.live {
			s.status = end.status
		}
		s = in.redirects(stmt, s)
	case *syntax.BinaryCmd:
		s = in.binary(cmd, s)
	case *syntax.FuncDecl:
		if len(declaredNames(cmd)) == 0 {
			s = in.anonymousFunction(stmt, cmd, s)
		} else {
			s = in.define(cmd, s)
		}
	case *syntax.TimeClause:
		if cmd.Stmt != nil {
			s = in.stmt(cmd.Stmt, s)
		}
	case *syntax.CoprocClause:
		in.subshell(subshellCoproc, func(sub *state) *state { return in.stmt(cmd.Stmt, sub) }, s)
		in.setStatus(s, stmt)
	case *syntax.TestDecl:
		s = in.testBody(cmd, s)
	default:
		s = in.evaluate(stmt, s)
		in.setStatus(s, stmt)
	}
	return s
}

func (in *interpreter) testBody(cmd *syntax.TestDecl, s *state) *state {
	saved := in.loops
	in.loops = nil
	in.subshells = append(in.subshells, nil)
	after := in.stmt(cmd.Body, s.clone())
	in.loops = saved
	if !after.live {
		after = s
	}
	in.loseWrites(subshellBats, after)
	return after
}

func (in *interpreter) redirects(stmt *syntax.Stmt, s *state) *state {
	for _, r := range stmt.Redirs {
		s = in.evaluate(r, s)
	}
	return s
}

type evaluation struct {
	s            *state
	base         int
	deferredBase int
}

func (in *interpreter) evaluate(node syntax.Node, s *state) *state {
	saved := in.eval
	in.eval = evaluation{s: s, base: len(in.walk), deferredBase: len(in.deferred)}
	parser.Walk(node, in.visitor)
	s = in.eval.s
	in.walk, in.deferred = in.walk[:in.eval.base], in.deferred[:in.eval.deferredBase]
	in.eval = saved
	return s
}

func (in *interpreter) visit(n syntax.Node) bool {
	e := &in.eval
	if n == nil {
		done := in.walk[len(in.walk)-1]
		in.walk = in.walk[:len(in.walk)-1]
		switch {
		case len(in.walk) > e.base && isDeclaration(in.walk[len(in.walk)-1]):
			in.deferred = append(in.deferred, done)
		case isDeclaration(done):
			for _, d := range in.deferred[e.deferredBase:] {
				in.applyWrites(d, e.s)
			}
			in.deferred = in.deferred[:e.deferredBase]
			in.applyWrites(done, e.s)
		default:
			in.applyWrites(done, e.s)
		}
		return true
	}
	switch n := n.(type) {
	case *syntax.CmdSubst:
		if n.TempFile || n.ReplyVar {
			e.s = in.stmts(n.Stmts, e.s)
			return false
		}
		kind := subshellSubstitution
		if n.Backquotes {
			kind = subshellBackticks
		}
		in.subshell(kind, func(sub *state) *state { return in.stmts(n.Stmts, sub) }, e.s)
		return false
	case *syntax.ProcSubst:
		in.subshell(subshellProcess, func(sub *state) *state { return in.stmts(n.Stmts, sub) }, e.s)
		return false
	case *syntax.ParamExp:
		in.recordExpansion(n, e.s)
		in.recordStatusRead(n, e.s)
		if n.Param != nil {
			in.recordRead(n.Param.Value, n, e.s)
		}
	case *syntax.Word:
		if len(in.walk) > e.base {
			if name, ok := parser.ArithmeticRead(in.walk[len(in.walk)-1], n); ok {
				in.recordRead(name, n, e.s)
			}
		}
	}
	in.walk = append(in.walk, n)
	return true
}

func isDeclaration(n syntax.Node) bool {
	_, ok := n.(*syntax.DeclClause)
	return ok
}

func (in *interpreter) recordExpansion(pe *syntax.ParamExp, s *state) {
	if !in.record || s.symbolic {
		return
	}
	got := in.readSpace(pe, s)
	if prev, ok := in.result.expansions[pe]; ok {
		got |= prev
	}
	in.result.expansions[pe] = got
	in.recordIntegerRead(pe, s)
}

func (in *interpreter) recordRead(name string, read syntax.Node, s *state) {
	if !in.record || s.symbolic {
		return
	}
	w, ok := s.lost.get(name)
	if !ok {
		return
	}
	if prev, seen := in.result.lostReads[read]; seen {
		w = prev.later(w)
	}
	in.result.lostReads[read] = w
}

type nodeWrites struct {
	starts []uint64
	defs   map[syntax.Node][]*parser.Definition
}

func indexWrites(tree *parser.Tree) nodeWrites {
	w := nodeWrites{starts: make([]uint64, len(tree.Text())/64+1), defs: make(map[syntax.Node][]*parser.Definition, len(tree.Definitions))}
	for i := range tree.Definitions {
		def := &tree.Definitions[i]
		if def.Node == nil {
			continue
		}
		off := def.Node.Pos().Offset()
		w.starts[off/64] |= 1 << (off % 64)
		w.defs[def.Node] = append(w.defs[def.Node], def)
	}
	return w
}

func (w nodeWrites) of(n syntax.Node) []*parser.Definition {
	off := n.Pos().Offset()
	if w.starts[off/64]&(1<<(off%64)) == 0 {
		return nil
	}
	return w.defs[n]
}

func (in *interpreter) applyWrites(n syntax.Node, s *state) {
	for _, def := range in.writes.of(n) {
		in.write(def, s)
	}
}

func (in *interpreter) write(def *parser.Definition, s *state) {
	switch def.Kind {
	case parser.DefCommandEnv, parser.DefLoop:
		return
	case parser.DefDeclare:
		if hasValue(def.Node) {
			in.noteWrite(def.Name, writeSite(def))
		}
	default:
		in.noteWrite(def.Name, writeSite(def))
	}
	switch def.Kind {
	case parser.DefArithmetic, parser.DefDescriptor:
		in.assignVar(def, s, value{space: spaceClean, integer: s.get(def.Name).integer, holdsInteger: integerYes})
		return
	case parser.DefBuiltin:
		in.assignVar(def, s, value{space: builtinSpace(def), integer: s.get(def.Name).integer})
		return
	}
	prev := s.get(def.Name)
	v := value{space: prev.space, integer: prev.integer}
	switch node := def.Node.(type) {
	case *syntax.Assign:
		v.space = in.assignSpace(node, prev, s)
		v.holdsInteger = in.assignedInteger(node, prev, s)
	case *syntax.Word:
		v.space = in.declarationWordSpace(node, s)
	}
	if def.Kind == parser.DefDeclare {
		v.integer = declaredInteger(def, prev.integer)
		if def.Local && !hasValue(def.Node) {
			v.space = spaceEmpty
		}
	}
	in.assignVar(def, s, v)
}

func hasValue(n syntax.Node) bool {
	switch n := n.(type) {
	case *syntax.Assign:
		return !n.Naked
	case *syntax.Word:
		lit, _ := parser.Literal(n)
		return strings.Contains(lit, "=")
	}
	return true
}

func declaredInteger(def *parser.Definition, prev integerAttr) integerAttr {
	switch {
	case def.Attrs.Has(parser.AttrInteger):
		return integerYes
	case def.Cleared.Has(parser.AttrInteger), def.Local:
		return integerNo
	}
	return prev
}

func builtinSpace(def *parser.Definition) space {
	if call, ok := def.Node.(*syntax.CallExpr); ok && len(call.Args) > 0 {
		switch call.Args[0].Lit() {
		case "getopts", "wait":
			return spaceClean
		}
	}
	return spaceDirty
}

func (in *interpreter) assignVar(def *parser.Definition, s *state, v value) {
	if def.Local {
		in.declareLocal(def.Name, s)
		s.set(def.Name, v)
		s.forget(def.Name)
		return
	}
	in.assign(s, def.Name, v)
}

func (in *interpreter) declareLocal(name string, s *state) {
	if len(in.frames) == 0 {
		return
	}
	frame := in.frames[len(in.frames)-1]
	if _, seen := frame.locals[name]; seen {
		return
	}
	if prev, ok := s.vars.get(name); ok {
		frame.locals[name] = &prev
		return
	}
	frame.locals[name] = nil
}

func (in *interpreter) assignSpace(a *syntax.Assign, prev value, s *state) space {
	var assigned space
	switch {
	case a.Array != nil || a.Index != nil:
		return spaceDirty
	case a.Naked:
		return prev.space
	case a.Value == nil:
		assigned = spaceEmpty
	default:
		assigned = in.wordSpace(a.Value, s)
	}
	if a.Append {
		return appendSpace(prev.expands(), assigned)
	}
	return assigned
}

func (in *interpreter) declarationWordSpace(w *syntax.Word, s *state) space {
	for i, part := range w.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			continue
		}
		_, after, found := strings.Cut(lit.Value, "=")
		if !found {
			continue
		}
		rest := &syntax.Word{Parts: append([]syntax.WordPart{&syntax.Lit{Value: after}}, w.Parts[i+1:]...)}
		return in.wordSpace(rest, s)
	}
	return spaceEmpty
}

func (in *interpreter) wordSpace(w *syntax.Word, s *state) space {
	var out space
	for _, part := range w.Parts {
		out = appendSpace(out, in.partSpace(part, s))
	}
	if out == 0 {
		return spaceEmpty
	}
	return out
}

func (in *interpreter) partSpace(part syntax.WordPart, s *state) space {
	switch p := part.(type) {
	case *syntax.Lit:
		if in.tree.Unexpandable(p) {
			return spaceDirty
		}
		return literalSpace(p.Value)
	case *syntax.SglQuoted:
		return literalSpace(p.Value)
	case *syntax.DblQuoted:
		var out space
		for _, inner := range p.Parts {
			out = appendSpace(out, in.partSpace(inner, s))
		}
		if out == 0 {
			return spaceEmpty
		}
		return out
	case *syntax.ParamExp:
		return in.expansionSpace(p, s)
	case *syntax.ArithmExp:
		return spaceClean
	case *syntax.ProcSubst:
		return spaceClean
	}
	return spaceDirty
}

func (in *interpreter) readSpace(pe *syntax.ParamExp, s *state) space {
	if pe.Param == nil {
		return spaceDirty
	}
	return in.variable(pe.Param.Value, s)
}

func (in *interpreter) expansionSpace(pe *syntax.ParamExp, s *state) space {
	switch {
	case pe.Length:
		return spaceClean
	case pe.Excl, pe.Names != 0, pe.Param == nil:
		return spaceDirty
	}
	if pe.Index != nil || pe.Repl != nil || pe.Slice != nil || len(pe.Modifiers) > 0 {
		return spaceDirty
	}
	base := in.variable(pe.Param.Value, s)
	if pe.Exp == nil {
		return base
	}
	alternative := spaceEmpty
	if pe.Exp.Word != nil {
		alternative = in.wordSpace(pe.Exp.Word, s)
	}
	switch pe.Exp.Op {
	case syntax.DefaultUnset, syntax.DefaultUnsetOrNull, syntax.AssignUnset, syntax.AssignUnsetOrNull:
		return (base | alternative).normalize()
	case syntax.AlternateUnset, syntax.AlternateUnsetOrNull, syntax.AssignAlways:
		return alternative
	case syntax.ErrorUnset, syntax.ErrorUnsetOrNull:
		return base
	}
	return spaceDirty
}

func (in *interpreter) variable(name string, s *state) space {
	switch {
	case isPositional(name):
		return spaceDirty
	case in.bats && name == batsStatus:
		return spaceClean
	}
	return s.get(name).expands()
}

func isPositional(name string) bool {
	if name == "@" || name == "*" {
		return true
	}
	return name != "" && strings.Trim(name, "0123456789") == ""
}

func (in *interpreter) call(stmt *syntax.Stmt, call *syntax.CallExpr, s *state) *state {
	s = in.evaluate(stmt, s)
	if len(call.Args) == 0 {
		return s
	}
	name, ok := parser.Literal(call.Args[0])
	if !ok {
		return s
	}
	in.callBeforeDefinition(call, name, s)
	if parser.IsSourceName(name) {
		for _, imported := range slices.Sorted(maps.Keys(in.imported)) {
			in.assign(s, imported, in.imported[imported])
		}
	}
	switch name {
	case "exit":
		in.escape(s)
		return deadState(s.symbolic)
	case "return":
		switch {
		case len(in.frames) > 0:
			frame := in.frames[len(in.frames)-1]
			frame.returns = join(frame.returns, s)
		case in.subshellDepth > 0:
			in.escape(s)
		default:
			in.sourceReturns = join(in.sourceReturns, s)
		}
		return deadState(s.symbolic)
	case "break", "continue":
		return in.jump(name, call, s)
	case "unset":
		for _, arg := range call.Args[1:] {
			if v, ok := parser.Literal(arg); ok && !strings.HasPrefix(v, "-") {
				in.assign(s, v, value{space: spaceEmpty, integer: integerNo})
			}
		}
	case "trap":
		in.trapped(call.Args[1:], s)
	}
	return in.invoke(name, s)
}

func (in *interpreter) trapped(args []*syntax.Word, s *state) {
	if len(args) > 0 && parser.LiteralIs(args[0], "--") {
		args = args[1:]
	}
	if len(args) < 2 {
		return
	}
	handler, ok := parser.Literal(args[0])
	if !ok {
		return
	}
	for _, c := range parser.Parse(handler, in.dialect).Commands {
		if ref, ok := s.funcs.get(c.Name); ok {
			in.result.called[ref.decl] = true
			continue
		}
		if decl, ok := in.staticFunc[c.Name]; ok {
			in.result.called[decl] = true
		}
	}
}

func (in *interpreter) jump(name string, call *syntax.CallExpr, s *state) *state {
	if len(in.loops) == 0 {
		return deadState(s.symbolic)
	}
	depth := 1
	if len(call.Args) > 1 {
		if lit, ok := parser.Literal(call.Args[1]); ok && len(lit) == 1 && lit[0] >= '1' && lit[0] <= '9' {
			depth = int(lit[0] - '0')
		}
	}
	frame := in.loops[max(len(in.loops)-depth, 0)]
	if name == "break" {
		frame.breaks = join(frame.breaks, s)
	} else {
		frame.continues = join(frame.continues, s)
	}
	return s
}

func (in *interpreter) invoke(name string, s *state) *state {
	ref, ok := s.funcs.get(name)
	if !ok {
		decl, static := in.staticFunc[name]
		if !static {
			return s
		}
		ref = funcRef{decl: decl, maybe: s.symbolic}
	}
	if ok || s.symbolic {
		in.result.called[ref.decl] = true
	}
	after := in.apply(in.summary(ref.decl), s)
	if ref.maybe {
		return join(after, s)
	}
	return after
}

func (in *interpreter) apply(sum *summary, s *state) *state {
	if !sum.exit.live {
		in.escape(s)
		return deadState(s.symbolic)
	}
	out := s.clone()
	sum.exit.vars.each(func(name string, v value) { in.assign(out, name, v.substitute(s.get(name))) })
	sum.exit.funcs.each(func(name string, ref funcRef) { out.funcs = out.funcs.set(name, ref) })
	sum.exit.lost.each(func(name string, w lostWrite) { out.lost = out.lost.set(name, w) })
	return out
}

func (in *interpreter) summary(decl *syntax.FuncDecl) *summary {
	if sum, ok := in.summaries[decl]; ok {
		return sum
	}
	if in.computing[decl] || decl.Body == nil {
		return &summary{exit: newState(true)}
	}
	in.computing[decl] = true
	saved := in.record
	in.record = false
	exit := in.runBody(decl, newState(true))
	in.record = saved
	delete(in.computing, decl)
	sum := &summary{exit: exit}
	in.summaries[decl] = sum
	return sum
}

func (in *interpreter) runBody(decl *syntax.FuncDecl, entry *state) *state {
	frame := &funcFrame{locals: map[string]*value{}, returns: deadState(entry.symbolic)}
	in.frames = append(in.frames, frame)
	savedLoops, savedSubshells := in.loops, in.subshells
	in.loops, in.subshells = nil, nil
	exit := in.stmt(decl.Body, entry)
	in.loops, in.subshells = savedLoops, savedSubshells
	in.frames = in.frames[:len(in.frames)-1]
	exit = join(exit, frame.returns)
	if !exit.live {
		return exit
	}
	for name, prev := range frame.locals {
		exit.forget(name)
		if prev == nil {
			exit.vars = exit.vars.remove(name)
			continue
		}
		exit.set(name, *prev)
	}
	return exit
}

func (in *interpreter) define(decl *syntax.FuncDecl, s *state) *state {
	for _, name := range declaredNames(decl) {
		s.funcs = s.funcs.set(name.Value, funcRef{decl: decl})
		s.pending = s.pending.remove(name.Value)
	}
	if in.record && !s.symbolic && !in.unreachable {
		in.definitions[decl] = true
	}
	return s
}

func (in *interpreter) anonymousFunction(stmt *syntax.Stmt, decl *syntax.FuncDecl, s *state) *state {
	for _, arg := range decl.Args {
		s = in.evaluate(arg, s)
	}
	s = in.runBody(decl, s)
	return in.redirects(stmt, s)
}

func (in *interpreter) binary(cmd *syntax.BinaryCmd, s *state) *state {
	switch cmd.Op {
	case syntax.AndStmt, syntax.OrStmt:
		in.markConditional(cmd.X, cmd.Y.Pos(), cmd.Y.End())
		left := in.stmt(cmd.X, s)
		right := in.stmt(cmd.Y, left.clone())
		return join(left, right)
	}
	in.subshell(subshellPipeline, func(sub *state) *state { return in.stmt(cmd.X, sub) }, s)
	if in.lastPipe {
		return in.stmt(cmd.Y, s)
	}
	if end := in.subshell(subshellPipeline, func(sub *state) *state { return in.stmt(cmd.Y, sub) }, s); end.live {
		s.status = end.status
	}
	return s
}

func (in *interpreter) ifClause(cmd *syntax.IfClause, s *state) *state {
	in.markConditionList(cmd.Cond, cmd.End())
	cond := in.stmts(cmd.Cond, s)
	then := in.stmts(cmd.Then, cond.clone())
	switch {
	case cmd.Else == nil:
		return join(then, cond)
	case len(cmd.Else.Cond) == 0:
		return join(then, in.stmts(cmd.Else.Then, cond.clone()))
	}
	return join(then, in.ifClause(cmd.Else, cond.clone()))
}

func (in *interpreter) loop(s *state, round func(head *state) *state, exitFrom func(head *state) *state) *state {
	frame := &loopFrame{breaks: deadState(s.symbolic), continues: deadState(s.symbolic)}
	in.loops = append(in.loops, frame)
	head := s
	for range maxLoopRounds {
		next := join(head, join(round(head.clone()), frame.continues))
		if next.equal(head) {
			break
		}
		head = next
	}
	exit := join(exitFrom(head.clone()), frame.breaks)
	in.loops = in.loops[:len(in.loops)-1]
	return exit
}

func (in *interpreter) whileClause(cmd *syntax.WhileClause, s *state) *state {
	in.markConditionList(cmd.Cond, cmd.End())
	return in.loop(s,
		func(head *state) *state { return in.stmts(cmd.Do, in.stmts(cmd.Cond, head)) },
		func(head *state) *state { return in.stmts(cmd.Cond, head) })
}

func (in *interpreter) forClause(cmd *syntax.ForClause, s *state) *state {
	switch loop := cmd.Loop.(type) {
	case *syntax.WordIter:
		items := spaceDirty
		var holdsInteger integerAttr
		if parser.ListsWords(loop) {
			items = 0
			for _, w := range loop.Items {
				s = in.evaluate(w, s)
				items |= literalWordSpace(w)
				if in.numericWord(w, s) {
					holdsInteger = integerYes
				}
			}
		}
		site, located := in.tree.NodeSpan(cmd)
		assign := func(head *state) *state {
			if items == 0 {
				return head
			}
			for _, name := range parser.LoopNames(loop) {
				if name == nil {
					continue
				}
				in.assign(head, name.Value, value{space: items.normalize(), integer: integerNo, holdsInteger: holdsInteger})
				if located {
					in.noteWrite(name.Value, site)
				}
			}
			return head
		}
		return in.loop(s,
			func(head *state) *state { return in.stmts(cmd.Do, assign(head)) },
			func(head *state) *state { return head })
	case *syntax.CStyleLoop:
		for _, expr := range []syntax.ArithmExpr{loop.Init} {
			if expr != nil {
				s = in.evaluate(expr, s)
			}
		}
		return in.loop(s,
			func(head *state) *state {
				if loop.Cond != nil {
					head = in.evaluate(loop.Cond, head)
				}
				head = in.stmts(cmd.Do, head)
				if loop.Post != nil && head.live {
					head = in.evaluate(loop.Post, head)
				}
				return head
			},
			func(head *state) *state {
				if loop.Cond != nil {
					head = in.evaluate(loop.Cond, head)
				}
				return head
			})
	}
	return s
}

func (in *interpreter) repeatClause(cmd *syntax.RepeatClause, s *state) *state {
	s = in.evaluate(cmd.Count, s)
	return in.loop(s,
		func(head *state) *state { return in.stmts(cmd.Do, head) },
		func(head *state) *state { return head })
}

func (in *interpreter) tryClause(cmd *syntax.TryClause, s *state) *state {
	entry := s.clone()
	tried := in.stmts(cmd.Try.Stmts, s)
	finished := in.stmts(cmd.Always.Stmts, join(tried, entry))
	if !tried.live {
		return deadState(finished.symbolic)
	}
	return finished
}

func (in *interpreter) caseClause(cmd *syntax.CaseClause, s *state) *state {
	s = in.evaluate(cmd.Word, s)
	exit := deadState(s.symbolic)
	carry := deadState(s.symbolic)
	catchAll := false
	for _, item := range cmd.Items {
		entry := s.clone()
		for _, pattern := range item.Patterns {
			entry = in.evaluate(pattern, entry)
			if lit, ok := parser.Literal(pattern); ok && lit == "*" {
				catchAll = true
			}
		}
		body := in.stmts(item.Stmts, join(entry, carry))
		switch item.Op {
		case syntax.Fallthrough, syntax.Resume, syntax.ResumeKorn:
			carry = body
		default:
			carry = deadState(s.symbolic)
			exit = join(exit, body)
		}
	}
	exit = join(exit, carry)
	if !catchAll {
		exit = join(exit, s)
	}
	return exit
}
