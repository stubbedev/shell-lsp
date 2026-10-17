package model

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

type Module struct {
	Files      []*Index
	Submodules map[string]*Module
	Complete   bool
	Builtins   Builtins
}

type Target struct {
	Module *Module
	File   int
	Symbol int
}

func (t Target) Info() Symbol { return t.Module.Files[t.File].Symbols[t.Symbol] }

type Resolution struct {
	Targets []Target
	Builtin *Builtin
	Missing bool
}

func (r Resolution) Found() bool { return len(r.Targets) > 0 || r.Builtin != nil }

func (m *Module) Resolve(file int, ref Ref) Resolution {
	module := m
	for _, name := range ref.Qualifier {
		sub := module.Submodules[name]
		if sub == nil {
			return Resolution{Missing: true}
		}
		module = sub
	}
	if ref.Kind == KindModule {
		sub, ok := module.Submodules[ref.Name]
		if ok && sub == nil {
			return Resolution{Missing: true}
		}
		return Resolution{Targets: module.lookup(-1, ref.Name, NamespaceModules, Global, 0)}
	}
	ns := ref.Kind.Namespace()
	scope := ref.Scope
	from := file
	if ref.Qualified() {
		scope, from = Global, -1
	}
	if targets := module.lookup(from, ref.Name, ns, scope, ref.Horizon); len(targets) > 0 {
		return Resolution{Targets: targets}
	}
	if b, ok := m.Builtins.Lookup(ref.Kind, ref.Name); ok {
		return Resolution{Builtin: &b}
	}
	return Resolution{}
}

func (m *Module) lookup(file int, name string, ns Namespace, scope Scope, horizon int) []Target {
	if file >= 0 && scope != Global {
		var local []Target
		for _, i := range m.Files[file].named(ns, name) {
			if s := m.Files[file].Symbols[i]; s.Scope == scope && !s.Inert && (horizon == 0 || s.Span.End <= horizon) {
				local = append(local, Target{Module: m, File: file, Symbol: i})
			}
		}
		if len(local) > 0 {
			return local
		}
		if inherited := m.inherited(file, name, ns, scope); len(inherited) > 0 {
			return inherited
		}
	}
	var out []Target
	for f, ix := range m.Files {
		for _, i := range ix.named(ns, name) {
			if ix.Symbols[i].Scope == Global {
				out = append(out, Target{Module: m, File: f, Symbol: i})
			}
		}
	}
	return out
}

func (m *Module) inherited(file int, name string, ns Namespace, scope Scope) []Target {
	ix := m.Files[file]
	seen := map[Scope]bool{scope: true}
	parents := slices.Clone(ix.Extends[scope])
	for len(parents) > 0 {
		parent := parents[0]
		parents = parents[1:]
		if seen[parent] {
			continue
		}
		seen[parent] = true
		var found []Target
		for _, i := range ix.named(ns, name) {
			if s := ix.Symbols[i]; s.Scope == parent && !s.Local && !s.Inert {
				found = append(found, Target{Module: m, File: file, Symbol: i})
			}
		}
		if len(found) > 0 {
			return found
		}
		parents = append(parents, ix.Extends[parent]...)
	}
	return nil
}

type Usage map[Target]bool

func (m *Module) Usage() Usage {
	used := Usage{}
	for f, ix := range m.Files {
		for _, r := range ix.Refs {
			for _, t := range m.Resolve(f, r).Targets {
				if !r.Edge || r.Owner != t.Symbol || t.File != f {
					used[t] = true
				}
			}
		}
	}
	return used
}

type moment struct {
	file, offset int
}

var whenever = moment{file: -1}

func (t Target) before(at moment) bool {
	return at == whenever || t.File != at.file || t.Info().Span.End <= at.offset
}

func effective(targets []Target, at moment) []Target {
	var out []Target
	byFile := map[int][]Target{}
	var files []int
	for _, t := range targets {
		if !t.before(at) || t.Info().Inert {
			continue
		}
		if _, ok := byFile[t.File]; !ok {
			files = append(files, t.File)
		}
		byFile[t.File] = append(byFile[t.File], t)
	}
	for _, f := range files {
		visible := byFile[f]
		slices.SortFunc(visible, func(a, b Target) int { return a.Info().Span.Start - b.Info().Span.Start })
		last := len(visible) - 1
		for last > 0 && visible[last].Info().Appends {
			last--
		}
		out = append(out, visible[last:]...)
	}
	return out
}

type context struct {
	at    moment
	chain string
}

func chainOf(scopes []Scope) string {
	var sb strings.Builder
	for _, s := range scopes {
		sb.WriteString(strconv.Itoa(int(s)) + ",")
	}
	return sb.String()
}

func (c context) scopes() []Scope {
	var out []Scope
	for part := range strings.SplitSeq(strings.TrimSuffix(c.chain, ","), ",") {
		if n, err := strconv.Atoi(part); err == nil {
			out = append(out, Scope(n))
		}
	}
	return out
}

const maxChains = 32

func (m *Module) chains(file int, scope Scope) []string {
	if scope == Global {
		return []string{""}
	}
	inherits := m.Files[file].Inherits
	var out []string
	var walk func(path []Scope)
	walk = func(path []Scope) {
		if len(out) >= maxChains {
			return
		}
		out = append(out, chainOf(path))
		for _, parent := range inherits[path[len(path)-1]] {
			if !slices.Contains(path, parent) {
				walk(append(slices.Clone(path), parent))
			}
		}
	}
	walk([]Scope{scope})
	return out
}

func (m *Module) resolveIn(file int, r Ref, c context) []Target {
	var scopes []Scope
	switch {
	case r.Base:
	case r.Scope != Global:
		scopes = append(scopes, r.Scope)
		fallthrough
	default:
		for _, s := range c.scopes() {
			if !slices.Contains(scopes, s) {
				scopes = append(scopes, s)
			}
		}
	}
	for _, scope := range append(scopes, Global) {
		r.Scope = scope
		targets := slices.DeleteFunc(effective(m.Resolve(file, r).Targets, c.at), func(t Target) bool { return t.Info().Scope != scope })
		if len(targets) > 0 {
			return targets
		}
	}
	return nil
}

func (m *Module) expansions() map[Target][]context {
	out := map[Target][]context{}
	type key struct {
		target Target
		chain  string
	}
	seen := map[key]moment{}
	type visit struct {
		target Target
		c      context
	}
	var queue []visit
	mark := func(t Target, c context) {
		if t.Module != m || !t.before(c.at) {
			return
		}
		k := key{t, c.chain}
		if prior, ok := seen[k]; ok && (prior == whenever || c.at != whenever && prior.offset >= c.at.offset) {
			return
		}
		seen[k], out[t] = c.at, append(out[t], c)
		queue = append(queue, visit{t, c})
	}
	for f, ix := range m.Files {
		for _, r := range ix.Refs {
			if r.Edge || r.Inert || r.Declares {
				continue
			}
			at := moment{file: f, offset: r.Span.Start}
			if r.Late {
				at = whenever
			}
			for _, chain := range m.chains(f, r.Scope) {
				c := context{at: at, chain: chain}
				for _, t := range m.resolveIn(f, r, c) {
					mark(t, c)
				}
			}
		}
	}
	if slices.ContainsFunc(m.Files, func(ix *Index) bool { return ix.Recipes }) {
		for f, ix := range m.Files {
			for i, sym := range ix.Symbols {
				if !sym.Inert && (sym.Exported || ix.ExportAll && sym.Kind == KindVariable) {
					mark(Target{Module: m, File: f, Symbol: i}, context{at: whenever})
				}
			}
		}
	}
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		ix := m.Files[v.target.File]
		for _, i := range ix.edgesOf(v.target.Symbol) {
			if ix.Refs[i].Inert {
				continue
			}
			for _, next := range m.resolveIn(v.target.File, ix.Refs[i], v.c) {
				mark(next, v.c)
			}
		}
	}
	return out
}

func (m *Module) required(declared Ref) bool {
	return slices.ContainsFunc(m.Files, func(ix *Index) bool {
		return slices.ContainsFunc(ix.Refs, func(r Ref) bool {
			return !r.Declares && r.Name == declared.Name && r.Kind.Namespace() == declared.Kind.Namespace()
		})
	})
}

func (m *Module) Broken() bool {
	return slices.ContainsFunc(m.Files, func(ix *Index) bool { return ix.Broken })
}

func (m *Module) opaque() bool {
	return slices.ContainsFunc(m.Files, func(ix *Index) bool { return ix.Opaque })
}

func (m *Module) Findings(file int, checks Checks, used Usage) []lint.Finding {
	ix := m.Files[file]
	certain := m.Complete && !m.Broken() && !m.opaque()
	expansions := m.expansions()
	expanded := Usage{}
	for t := range expansions {
		expanded[t] = true
	}
	dormant := func(r Ref) bool {
		if !r.Edge {
			return false
		}
		if r.Owner < 0 {
			return true
		}
		owner := ix.Symbols[r.Owner]
		return checks.Of(owner.Kind).Expanded == OnUse && !expanded[Target{Module: m, File: file, Symbol: r.Owner}]
	}
	var out []lint.Finding
	for _, r := range ix.Refs {
		res := m.Resolve(file, r)
		switch {
		case res.Missing || r.Soft || r.Inert || dormant(r) || r.Declares && m.required(r):
		case !res.Found():
			if rules := checks.Of(r.Kind); rules.Unknown != 0 && (certain || rules.UnknownWhen == Regardless) {
				out = append(out, lint.HostFinding(rules.Unknown, r.Span, fmt.Sprintf("unknown %s `%s`", r.Kind, qualified(r))))
			}
		case res.Builtin != nil:
			out = append(out, arity(checks, r, res.Builtin.Kind, res.Builtin.Arity)...)
			if res.Builtin.Deprecated != "" && checks.Deprecated != 0 {
				out = append(out, lint.HostFinding(checks.Deprecated, r.Span, fmt.Sprintf("%s `%s` is deprecated: %s", r.Kind, r.Name, res.Builtin.Deprecated)))
			}
		default:
			s := res.Targets[0].Info()
			out = append(out, arity(checks, r, s.Kind, s.Arity)...)
		}
	}
	out = append(out, m.cycles(file, checks, expansions)...)
	if checks.Absent != 0 {
		out = append(out, m.absent(file, checks.Absent)...)
	}
	if checks.NotPhony != 0 && certain {
		out = append(out, m.notPhony(file, checks.NotPhony)...)
	}
	for _, d := range ix.Deferred {
		code := d.Unused
		contexts := expansions[Target{Module: m, File: file, Symbol: d.Symbol}]
		if len(contexts) > 0 && (d.Until == 0 || slices.ContainsFunc(contexts, func(c context) bool {
			return c.at != whenever && c.at.file == file && c.at.offset < d.Until
		})) {
			code = d.Used
		}
		out = append(out, lint.HostFinding(code, d.Span, d.Message))
	}
	for i, s := range ix.Symbols {
		code := checks.Of(s.Kind).Unused
		if code != 0 && s.Unused && certain && !used[Target{Module: m, File: file, Symbol: i}] {
			out = append(out, lint.HostFinding(code, s.Selection, fmt.Sprintf("%s `%s` is never used", s.Kind, s.Name)))
		}
	}
	return out
}

func qualified(r Ref) string {
	return strings.Join(append(slices.Clone(r.Qualifier), r.Name), "::")
}

func arity(checks Checks, r Ref, kind Kind, a Arity) []lint.Finding {
	code := checks.Of(kind).Arity
	if code == 0 || r.Args == NoArgs || a.Accepts(r.Args) {
		return nil
	}
	return []lint.Finding{lint.HostFinding(code, r.Span, fmt.Sprintf("%s `%s` takes %s but is given %d", kind, qualified(r), a, r.Args))}
}

func (a Arity) String() string {
	plural := func(n int) string {
		if n == 1 {
			return "1 argument"
		}
		return fmt.Sprintf("%d arguments", n)
	}
	switch {
	case a.Max == Variadic:
		return "at least " + plural(a.Min)
	case a.Min == a.Max:
		return plural(a.Min)
	}
	return fmt.Sprintf("%d to %s", a.Min, plural(a.Max))
}

type node struct {
	file, symbol int
}

type edge struct {
	to  node
	ref Ref
}

func (m *Module) edges(n node, c context) []edge {
	var out []edge
	ix := m.Files[n.file]
	for _, i := range ix.edgesOf(n.symbol) {
		r := ix.Refs[i]
		if r.Qualified() || r.Inert {
			continue
		}
		for _, t := range m.resolveIn(n.file, r, c) {
			if t.Module == m && (t.File != n.file || !ix.Branches.Exclusive(r.Span.Start, ix.Symbols[t.Symbol].Span.Start)) {
				out = append(out, edge{node{t.File, t.Symbol}, r})
			}
		}
	}
	return out
}

func (m *Module) cycles(file int, checks Checks, expansions map[Target][]context) []lint.Finding {
	var out []lint.Finding
	for i, s := range m.Files[file].Symbols {
		rules := checks.Of(s.Kind)
		code := rules.Circular
		contexts := []context{{at: whenever}}
		if rules.Expanded == OnUse {
			contexts = expansions[Target{Module: m, File: file, Symbol: i}]
		}
		if code == 0 {
			continue
		}
		start := node{file, i}
		if e, path, found := m.cycle(start, contexts); found {
			names := []string{s.Name}
			for _, n := range path {
				names = append(names, m.Files[n.file].Symbols[n.symbol].Name)
			}
			message := fmt.Sprintf("%s `%s` depends on itself", s.Kind, s.Name)
			if len(names) > 2 {
				message = fmt.Sprintf("%s `%s` is circular: `%s`", s.Kind, s.Name, strings.Join(names, " -> "))
			}
			out = append(out, lint.HostFinding(code, e.ref.Span, message))
		}
	}
	return out
}

func (m *Module) cycle(start node, contexts []context) (edge, []node, bool) {
	for _, c := range contexts {
		for _, e := range m.edges(start, c) {
			if path, found := m.pathBack(e.to, start, map[node]bool{}, c); found {
				return e, path, true
			}
		}
	}
	return edge{}, nil, false
}

func (m *Module) pathBack(from, goal node, seen map[node]bool, c context) ([]node, bool) {
	if from == goal {
		return []node{goal}, true
	}
	if seen[from] {
		return nil, false
	}
	seen[from] = true
	for _, e := range m.edges(from, c) {
		if rest, ok := m.pathBack(e.to, goal, seen, c); ok {
			return append([]node{from}, rest...), true
		}
	}
	return nil, false
}

func (m *Module) absent(file int, code lint.Code) []lint.Finding {
	type blocker struct {
		via    string
		module string
	}
	blocked := map[node]blocker{}
	for changed := true; changed; {
		changed = false
		for f, ix := range m.Files {
			for _, r := range ix.Refs {
				owner := node{f, r.Owner}
				if !r.Edge || r.Owner < 0 || ix.Symbols[r.Owner].Kind.Namespace() != r.Kind.Namespace() {
					continue
				}
				if _, done := blocked[owner]; done {
					continue
				}
				res := m.Resolve(f, r)
				if sub, declared := m.Submodules[r.FirstQualifier()]; res.Missing && declared && sub == nil {
					blocked[owner], changed = blocker{via: qualified(r), module: r.Qualifier[0]}, true
					continue
				}
				for _, t := range res.Targets {
					if b, ok := blocked[node{t.File, t.Symbol}]; ok && t.Module == m {
						blocked[owner], changed = blocker{via: t.Info().Name, module: b.module}, true
						break
					}
				}
			}
		}
	}
	var out []lint.Finding
	ix := m.Files[file]
	for i, s := range ix.Symbols {
		b, ok := blocked[node{file, i}]
		if !ok {
			continue
		}
		message := fmt.Sprintf("%s `%s` cannot run: it depends on `%s` of the absent module `%s`", s.Kind, s.Name, b.via, b.module)
		if !strings.Contains(b.via, "::") {
			message = fmt.Sprintf("%s `%s` cannot run: it depends on `%s`, which needs the absent module `%s`", s.Kind, s.Name, b.via, b.module)
		}
		out = append(out, lint.HostFinding(code, s.Selection, message))
	}
	return out
}

func (m *Module) notPhony(file int, code lint.Code) []lint.Finding {
	phony := map[string]bool{}
	for _, ix := range m.Files {
		for _, name := range ix.Phony {
			phony[name] = true
		}
	}
	var out []lint.Finding
	ix := m.Files[file]
	for _, i := range ix.Fileless {
		if s := ix.Symbols[i]; !phony[s.Name] {
			phony[s.Name] = true
			out = append(out, lint.HostFinding(code, s.Selection, fmt.Sprintf("`%s` creates no file of that name; list it in .PHONY so a file called `%s` cannot keep it from running", s.Name, s.Name)))
		}
	}
	return out
}

func MissingInclude(checks Checks, inc Include) lint.Finding {
	if inc.Module != "" {
		return lint.HostFinding(checks.Missing, inc.Span, fmt.Sprintf("no source file for module `%s`; looked for %s", inc.Module, strings.Join(quoted(inc.Candidates), ", ")))
	}
	return lint.HostFinding(checks.Missing, inc.Span, fmt.Sprintf("`%s` is not found", inc.Path))
}

func quoted(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "`" + n + "`"
	}
	return out
}
