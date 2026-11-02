package nix

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

func (p *Project) incomplete() string {
	switch {
	case p.failure != "":
		return p.failure
	case p.truncated:
		return fmt.Sprintf("the project has more than %d files, so not every reader is known", maxProjectFiles)
	}
	return ""
}

func (p *Project) readEscapes() {
	if p.escapesRead {
		return
	}
	p.escapesRead = true
	for _, u := range p.units {
		for _, chain := range u.chains {
			values := p.eval(u, chain.target)
			for _, name := range chain.path {
				values = p.selectAll(values, name)
			}
			for _, v := range values {
				switch v := v.(type) {
				case flowSpace:
					v.space.escaped = v.space.escaped || !chain.dynamic
					v.space.dynamic = v.space.dynamic || chain.dynamic
				case flowSet:
					p.escapedSets[v.set] = true
				}
			}
		}
	}
}

func (p *Project) Refusal(file, symbol int) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	u := p.units[file]
	sym := u.ix.Symbols[symbol]
	p.readEscapes()
	for _, m := range p.memberships[model.Home{File: file, Scope: sym.Scope}] {
		if m.applies(sym.Name) {
			return p.spaceRefusal(m.space, sym.Name), true
		}
	}
	o := u.owners[symbol]
	if o.set != nil && o.set.lambda != nil {
		return p.formalRefusal(flowLambda{unit: u, fn: o.set.lambda}, sym.Name)
	}
	return p.fileAttrRefusal(file, o)
}

func (p *Project) spaceRefusal(s *space, name string) string {
	if reason := p.incomplete(); reason != "" {
		return fmt.Sprintf("`%s` is shared between files, and %s", name, reason)
	}
	switch s.role {
	case inputsRole:
		if s.parent == nil && name == selfFormal {
			return "`self` is the flake itself and has no other name"
		}
	case argsRole:
		if slices.Contains(givenArgs, name) {
			return fmt.Sprintf("the module system provides `%s` under this name", name)
		}
	case optionRole:
		if !declaredWithin(s, name) {
			return fmt.Sprintf("`%s` is an option declared outside this project, so the name is not this project's to change", name)
		}
	}
	for at := s; at != nil; at = at.parent {
		if at.escaped {
			return fmt.Sprintf("the set that holds `%s` is passed on as a value, so other code may read it by its name", name)
		}
		if at.dynamic {
			return fmt.Sprintf("the set that holds `%s` is read with a computed name, so other code may read it by its name", name)
		}
	}
	return ""
}

func declaredWithin(s *space, name string) bool {
	if kid := s.kids[name]; kid != nil && kid.declared {
		return true
	}
	for at := s; at != nil; at = at.parent {
		if at.declared {
			return true
		}
	}
	return false
}

func (p *Project) formalRefusal(fn flowLambda, name string) (string, bool) {
	if _, pattern := fn.fn.Param.(*syntax.PatternParam); !pattern {
		return "", false
	}
	if fn.unit == p.flakeUnit && fn.fn == p.outputs {
		return fmt.Sprintf("the flake receives its input `%s` under this name", name), true
	}
	if p.modular[fn] {
		return fmt.Sprintf("the module system passes `%s` by this name", name), true
	}
	callers := p.callers[fn]
	if len(callers) == 0 {
		return "", false
	}
	if reason := p.incomplete(); reason != "" {
		return fmt.Sprintf("the callers of this function are not all known: %s", reason), true
	}
	for _, c := range callers {
		if c.call.kind == callAuto && !passes(c, name) {
			return fmt.Sprintf("`callPackage` fills `%s` by name unless a caller passes it", name), true
		}
	}
	return "", false
}

func passes(c flowCaller, name string) bool {
	at := c.call.path + 1
	if at >= len(c.call.call.Args) {
		return false
	}
	set, literal := syntax.Unparenthesized(c.call.call.Args[at]).(*syntax.AttrSet)
	if !literal {
		return false
	}
	own := c.unit.sets[set]
	if own == nil {
		return false
	}
	_, named := own.names[name]
	return named
}

func (p *Project) fileAttrRefusal(file int, o owner) (string, bool) {
	if o.def != nil || o.set == nil {
		return "", false
	}
	top := o.set
	for top.parent != nil {
		top = top.parent
	}
	u := p.units[file]
	if !slices.Contains(p.fileValue(p.paths[file]), flowValue(flowSet{unit: u, set: top})) {
		return "", false
	}
	sites := p.sites[file]
	if len(sites) == 0 || p.incomplete() != "" || slices.Contains(sites, false) {
		return "", false
	}
	for at := o.set; at != nil; at = at.parent {
		if p.escapedSets[at] {
			return "", false
		}
	}
	return "", true
}

func (p *Project) Conflict(file, symbol int, newName string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	u := p.units[file]
	sym := u.ix.Symbols[symbol]
	home := model.Home{File: file, Scope: sym.Scope}
	if taken, shared := p.homes(file, sym.Scope, sym.Name); shared {
		for _, at := range taken {
			if reason := p.homeConflict(at, sym.Name, newName); reason != "" {
				return reason
			}
		}
	} else if reason := p.homeConflict(home, sym.Name, newName); reason != "" {
		return reason
	}
	if o := u.owners[symbol]; o.set != nil && o.set.lambda != nil {
		for _, c := range p.callers[flowLambda{unit: u, fn: o.set.lambda}] {
			if passes(c, newName) {
				return fmt.Sprintf("a call in `%s` already passes `%s` next to `%s`", filepath.Base(p.paths[p.files[c.unit]]), newName, sym.Name)
			}
		}
	}
	return p.readConflict(file, sym, newName)
}

func (p *Project) homeConflict(at model.Home, name, newName string) string {
	u := p.units[at.File]
	for i, s := range u.ix.Symbols {
		if s.Scope != at.Scope {
			continue
		}
		if s.Name == newName {
			return fmt.Sprintf("`%s` is already bound next to `%s` in `%s`", newName, name, filepath.Base(p.paths[at.File]))
		}
		if s.Name != name {
			continue
		}
		if reason := u.ix.Renames.Conflict(i, newName); reason != "" {
			return reason
		}
	}
	return ""
}

func (p *Project) readConflict(file int, sym model.Symbol, newName string) string {
	homes, shared := p.homes(file, sym.Scope, sym.Name)
	if !shared {
		homes = []model.Home{{File: file, Scope: sym.Scope}}
	}
	for f, u := range p.units {
		for _, r := range u.ix.Refs {
			if r.Name != newName {
				continue
			}
			slot, opened := u.slots[r.Scope]
			if !opened {
				continue
			}
			for _, at := range p.homesOfSlot(model.Home{File: f, Scope: r.Scope}, slot) {
				if slices.Contains(homes, at) {
					return fmt.Sprintf("`%s` already reads an attribute `%s` of the set that holds `%s`, which would then find the renamed one", filepath.Base(p.paths[f]), newName, sym.Name)
				}
			}
		}
	}
	return ""
}

func (p *Project) Sources(file, symbol int) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	sym := p.units[file].ix.Symbols[symbol]
	shared := false
	for _, m := range p.memberships[model.Home{File: file, Scope: sym.Scope}] {
		shared = shared || m.space == p.inputs && m.applies(sym.Name)
	}
	if !shared || p.flakeUnit == nil {
		return nil
	}
	lock, ok := p.flakeLockFile()
	if !ok {
		return nil
	}
	source, ok := lock.sourceOf(sym.Name)
	if !ok {
		return nil
	}
	return []string{filepath.Join(source, FlakeFile), filepath.Join(source, DirectoryEntry)}
}

func (p *Project) flakeLockFile() (flakeLock, bool) {
	if !p.lockRead {
		p.lockRead = true
		if data, err := p.source.Read(filepath.Join(p.flakeDir, lockFile)); err == nil {
			if lock, ok := parseFlakeLock(data); ok {
				p.lock = &lock
			}
		}
	}
	if p.lock == nil {
		return flakeLock{}, false
	}
	return *p.lock, true
}
