package lint

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type statusSet []*syntax.Stmt

func compareStmts(a, b *syntax.Stmt) int {
	return cmp.Or(cmp.Compare(a.Pos().Offset(), b.Pos().Offset()), cmp.Compare(a.End().Offset(), b.End().Offset()))
}

func (s statusSet) union(o statusSet) statusSet {
	switch {
	case len(o) == 0 || slices.Equal(s, o):
		return s
	case len(s) == 0:
		return o
	}
	out := make(statusSet, 0, len(s)+len(o))
	i, j := 0, 0
	for i < len(s) || j < len(o) {
		switch {
		case j == len(o):
			out = append(out, s[i])
			i++
		case i == len(s):
			out = append(out, o[j])
			j++
		case s[i] == o[j]:
			out = append(out, s[i])
			i++
			j++
		case compareStmts(s[i], o[j]) <= 0:
			out = append(out, s[i])
			i++
		default:
			out = append(out, o[j])
			j++
		}
	}
	return out
}

type callList struct {
	call *syntax.CallExpr
	next *callList
}

func (l *callList) contains(call *syntax.CallExpr) bool {
	for ; l != nil; l = l.next {
		if l.call == call {
			return true
		}
	}
	return false
}

func (l *callList) union(o *callList) *callList {
	out := l
	for ; o != nil; o = o.next {
		if !out.contains(o.call) {
			out = &callList{call: o.call, next: out}
		}
	}
	return out
}

func unionPending(a, b pmap[*callList]) pmap[*callList] {
	out := a
	a.diff(b, func(name string) {
		theirs, ok := b.get(name)
		if !ok {
			return
		}
		mine, _ := a.get(name)
		out = out.set(name, mine.union(theirs))
	})
	return out
}

type appendFact struct {
	numeric bool
	integer bool
}

type flowFacts struct {
	statusReads   map[*syntax.ParamExp]statusSet
	conditional   map[*syntax.Stmt]parser.Span
	forwardCalls  map[*syntax.CallExpr]bool
	escaped       map[*syntax.CallExpr]bool
	appends       map[*syntax.Assign]appendFact
	deadFunctions map[*syntax.FuncDecl]bool
	integerReads  map[*syntax.ParamExp]bool
}

func newFlowFacts() flowFacts {
	return flowFacts{
		statusReads:   map[*syntax.ParamExp]statusSet{},
		conditional:   map[*syntax.Stmt]parser.Span{},
		forwardCalls:  map[*syntax.CallExpr]bool{},
		escaped:       map[*syntax.CallExpr]bool{},
		appends:       map[*syntax.Assign]appendFact{},
		deadFunctions: map[*syntax.FuncDecl]bool{},
		integerReads:  map[*syntax.ParamExp]bool{},
	}
}

func readsStatus(text string) bool {
	return strings.Contains(text, "$?") || strings.Contains(text, "${?")
}

func (in *interpreter) setStatus(s *state, stmt *syntax.Stmt) {
	if !in.trackStatus || !s.live {
		return
	}
	set, ok := in.singletons[stmt]
	if !ok {
		set = statusSet{stmt}
		in.singletons[stmt] = set
	}
	s.status = set
}

func (in *interpreter) markConditional(stmt *syntax.Stmt, from, to syntax.Pos) {
	if !in.trackStatus || !in.record {
		return
	}
	setter := rightmost(stmt)
	region := parser.Span{Start: parser.Offset(from), End: parser.Offset(to)}
	if prev, ok := in.result.conditional[setter]; ok {
		region = parser.Span{Start: min(prev.Start, region.Start), End: max(prev.End, region.End)}
	}
	in.result.conditional[setter] = region
}

func (in *interpreter) markConditionList(list []*syntax.Stmt, to syntax.Pos) {
	if len(list) > 0 {
		last := list[len(list)-1]
		in.markConditional(last, last.End(), to)
	}
}

func (in *interpreter) recordStatusRead(pe *syntax.ParamExp, s *state) {
	if !in.trackStatus || !in.record || s.symbolic || pe.Param == nil || pe.Param.Value != "?" {
		return
	}
	in.result.statusReads[pe] = in.result.statusReads[pe].union(s.status)
}

func (in *interpreter) callBeforeDefinition(call *syntax.CallExpr, name string, s *state) {
	if !in.trackCalls || !in.record || s.symbolic || len(in.frames) > 0 {
		return
	}
	if _, static := in.staticFunc[name]; !static {
		return
	}
	if ref, ok := s.funcs.get(name); ok && !ref.maybe {
		return
	}
	in.result.forwardCalls[call] = true
	pending, _ := s.pending.get(name)
	if !pending.contains(call) {
		s.pending = s.pending.set(name, &callList{call: call, next: pending})
	}
}

func (in *interpreter) escape(s *state) {
	if !in.trackCalls || s.pending.root == nil {
		return
	}
	if n := len(in.subshellExits); n > 0 {
		in.subshellExits[n-1] = unionPending(in.subshellExits[n-1], s.pending)
		return
	}
	s.pending.each(func(_ string, calls *callList) {
		for ; calls != nil; calls = calls.next {
			in.result.escaped[calls.call] = true
		}
	})
}

func (in *interpreter) numericWord(w *syntax.Word, s *state) bool {
	if w == nil || len(w.Parts) != 1 {
		return false
	}
	part := w.Parts[0]
	if dq, ok := part.(*syntax.DblQuoted); ok && len(dq.Parts) == 1 {
		part = dq.Parts[0]
	}
	switch part := part.(type) {
	case *syntax.Lit:
		return parser.IsDigits(part.Value)
	case *syntax.ArithmExp:
		return true
	case *syntax.ParamExp:
		return parser.IsPlainParam(part) && s.get(part.Param.Value).holdsInteger&integerYes != 0
	}
	return false
}

func (in *interpreter) assignedInteger(a *syntax.Assign, prev value, s *state) integerAttr {
	if a.Append {
		in.recordAppend(a, prev, s)
	}
	switch {
	case a.Naked:
		return prev.holdsInteger
	case a.Array != nil || a.Index != nil:
		return 0
	case a.Append:
		if prev.holdsInteger&integerYes != 0 && in.numericWord(a.Value, s) {
			return integerYes
		}
		return 0
	case in.numericWord(a.Value, s):
		return integerYes
	}
	return 0
}

func (in *interpreter) recordAppend(a *syntax.Assign, target value, s *state) {
	if !in.record || s.symbolic || a.Value == nil || len(a.Value.Parts) != 1 {
		return
	}
	var numeric bool
	switch part := a.Value.Parts[0].(type) {
	case *syntax.Lit:
		numeric = parser.IsDigits(part.Value) || (parser.IsName(part.Value) && s.get(part.Value).holdsInteger&integerYes != 0)
	case *syntax.ParamExp:
		numeric = parser.IsPlainParam(part) && s.get(part.Param.Value).holdsInteger&integerYes != 0
	}
	prev := in.result.appends[a]
	in.result.appends[a] = appendFact{numeric: prev.numeric || numeric, integer: prev.integer || target.integer&integerYes != 0}
}

func (r *flowResult) forwardCall(call *syntax.CallExpr) bool {
	return r != nil && r.forwardCalls[call] && !r.escaped[call]
}

func (in *interpreter) recordIntegerRead(pe *syntax.ParamExp, s *state) {
	if pe.Param != nil && s.get(pe.Param.Value).holdsInteger&integerYes != 0 {
		in.result.integerReads[pe] = true
	}
}

func (r *flowResult) mayHoldInteger(pe *syntax.ParamExp) bool {
	return r != nil && r.integerReads[pe]
}
