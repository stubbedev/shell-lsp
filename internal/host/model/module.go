package model

import (
	"fmt"
	"slices"
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
		return Resolution{Targets: module.lookup(-1, ref.Name, NamespaceModules, Global)}
	}
	ns := ref.Kind.Namespace()
	scope := ref.Scope
	from := file
	if ref.Qualified() {
		scope, from = Global, -1
	}
	if targets := module.lookup(from, ref.Name, ns, scope); len(targets) > 0 {
		return Resolution{Targets: targets}
	}
	if b, ok := m.Builtins.Lookup(ref.Kind, ref.Name); ok {
		return Resolution{Builtin: &b}
	}
	return Resolution{}
}

func (m *Module) lookup(file int, name string, ns Namespace, scope Scope) []Target {
	if file >= 0 && scope != Global {
		var local []Target
		for i, s := range m.Files[file].Symbols {
			if s.Name == name && s.Scope == scope && s.Kind.Namespace() == ns {
				local = append(local, Target{Module: m, File: file, Symbol: i})
			}
		}
		if len(local) > 0 {
			return local
		}
	}
	var out []Target
	for f, ix := range m.Files {
		for i, s := range ix.Symbols {
			if s.Name == name && s.Scope == Global && s.Kind.Namespace() == ns {
				out = append(out, Target{Module: m, File: f, Symbol: i})
			}
		}
	}
	return out
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

func (m *Module) Broken() bool {
	return slices.ContainsFunc(m.Files, func(ix *Index) bool { return ix.Broken })
}

func (m *Module) Findings(file int, checks Checks, used Usage) []lint.Finding {
	ix := m.Files[file]
	certain := m.Complete && !m.Broken()
	var out []lint.Finding
	for _, r := range ix.Refs {
		res := m.Resolve(file, r)
		switch {
		case res.Missing || r.Soft:
		case !res.Found():
			if code, ok := checks.Unknown[r.Kind.Namespace()]; ok && (certain || checks.Always[r.Kind.Namespace()]) {
				out = append(out, lint.HostFinding(code, r.Span, fmt.Sprintf("unknown %s `%s`", r.Kind, qualified(r))))
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
	out = append(out, m.cycles(file, checks)...)
	for i, s := range ix.Symbols {
		code, ok := checks.Unused[s.Kind]
		if ok && s.Unused && certain && !used[Target{Module: m, File: file, Symbol: i}] {
			out = append(out, lint.HostFinding(code, s.Selection, fmt.Sprintf("%s `%s` is never used", s.Kind, s.Name)))
		}
	}
	return out
}

func qualified(r Ref) string {
	return strings.Join(append(slices.Clone(r.Qualifier), r.Name), "::")
}

func arity(checks Checks, r Ref, kind Kind, a Arity) []lint.Finding {
	code, ok := checks.Arity[kind]
	if !ok || r.Args == NoArgs || a.Accepts(r.Args) {
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

func (m *Module) edges(n node) []edge {
	var out []edge
	for _, r := range m.Files[n.file].Refs {
		if !r.Edge || r.Owner != n.symbol || r.Qualified() {
			continue
		}
		for _, t := range m.Resolve(n.file, r).Targets {
			if t.Module == m {
				out = append(out, edge{node{t.File, t.Symbol}, r})
			}
		}
	}
	return out
}

func (m *Module) cycles(file int, checks Checks) []lint.Finding {
	var out []lint.Finding
	for i, s := range m.Files[file].Symbols {
		code, ok := checks.Circular[s.Kind]
		if !ok {
			continue
		}
		start := node{file, i}
		for _, e := range m.edges(start) {
			path, found := m.pathBack(e.to, start, map[node]bool{})
			if !found {
				continue
			}
			names := []string{s.Name}
			for _, n := range path {
				names = append(names, m.Files[n.file].Symbols[n.symbol].Name)
			}
			message := fmt.Sprintf("%s `%s` depends on itself", s.Kind, s.Name)
			if len(names) > 2 {
				message = fmt.Sprintf("%s `%s` is circular: `%s`", s.Kind, s.Name, strings.Join(names, " -> "))
			}
			out = append(out, lint.HostFinding(code, e.ref.Span, message))
			break
		}
	}
	return out
}

func (m *Module) pathBack(from, goal node, seen map[node]bool) ([]node, bool) {
	if from == goal {
		return []node{goal}, true
	}
	if seen[from] {
		return nil, false
	}
	seen[from] = true
	for _, e := range m.edges(from) {
		if rest, ok := m.pathBack(e.to, goal, seen); ok {
			return append([]node{from}, rest...), true
		}
	}
	return nil, false
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
