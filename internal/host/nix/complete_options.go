package nix

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/nixeval"
)

const (
	pkgsName        = "pkgs"
	libName         = "lib"
	anyAttrName     = "<name>"
	maxRootHops     = 8
	sourceSeparator = ", "
)

var moduleWrappers = []string{"mkIf", "mkDefault", "mkForce", "mkOverride", "mkBefore", "mkAfter", "mkOrder", "optionalAttrs"}

type OptionClass uint8

const (
	ClassUnknown OptionClass = iota
	ClassNixOS
	ClassHomeManager
	ClassFlakeParts
	ClassPerSystem
)

func optionClassNamed(name string) OptionClass {
	switch name {
	case classNixos:
		return ClassNixOS
	case classHome:
		return ClassHomeManager
	case classFlake:
		return ClassFlakeParts
	case classPerSystem:
		return ClassPerSystem
	}
	return ClassUnknown
}

type OptionSite struct {
	Class OptionClass
	Path  []string
}

type OptionChild struct {
	Name    string
	Sources []string
	Option  *nixeval.Option
}

type SourcedOption struct {
	Option  nixeval.Option
	Sources []string
}

type Evaluated interface {
	OptionChildren(site OptionSite) []OptionChild
	OptionAt(site OptionSite) (SourcedOption, bool)
	NixpkgsChildren(path []string, prefix string) []string
	NixpkgsPackage(path []string) (*nixeval.Package, bool)
}

type noEvaluation struct{}

func (noEvaluation) OptionChildren(OptionSite) []OptionChild { return nil }

func (noEvaluation) OptionAt(OptionSite) (SourcedOption, bool) { return SourcedOption{}, false }

func (noEvaluation) NixpkgsChildren([]string, string) []string { return nil }

func (noEvaluation) NixpkgsPackage([]string) (*nixeval.Package, bool) { return nil, false }

var NoEvaluation Evaluated = noEvaluation{}

func (p *Project) OptionClasses(path string) []OptionClass {
	p.mu.Lock()
	defer p.mu.Unlock()
	file, ok := p.byPath[path]
	if !ok {
		return nil
	}
	var out []OptionClass
	for _, name := range sortedNames(p.classes) {
		if slices.ContainsFunc(p.classes[name].root.members, func(h model.Home) bool { return h.File == file }) {
			out = append(out, optionClassNamed(name))
		}
	}
	if slices.Contains(out, ClassFlakeParts) {
		return []OptionClass{ClassFlakeParts}
	}
	return out
}

type walkMode uint8

const (
	walkNone walkMode = iota
	walkFlakeFile
	walkFlakeOutputs
	walkModule
	walkModuleList
	walkMergedList
	walkCallArgs
)

type optionWalk struct {
	mode  walkMode
	class OptionClass
	path  []string
	top   bool
}

func optionWalkTo(chain []syntax.Node, end int) optionWalk {
	w := optionWalk{mode: walkModule, top: true}
	if set, ok := chain[0].(*syntax.AttrSet); ok && hasHead(set, outputsAttr) {
		w.mode = walkFlakeFile
	}
	for i := range end {
		w.step(chain[i], chain[i+1])
	}
	return w
}

func optionSiteAt(chain []syntax.Node, end int) (OptionSite, bool) {
	w := optionWalkTo(chain, end)
	return OptionSite{Class: w.class, Path: w.path}, w.mode == walkModule
}

func (w *optionWalk) keepIf(ok bool) {
	if !ok {
		w.mode = walkNone
	}
}

func (w *optionWalk) step(node, child syntax.Node) {
	switch n := node.(type) {
	case *syntax.Paren, *syntax.AttrSet:
	case *syntax.Lambda:
		w.keepIf(n.Body == child)
	case *syntax.Let:
		w.keepIf(n.Body == child)
	case *syntax.With:
		w.keepIf(n.Body == child)
	case *syntax.Assert:
		w.keepIf(n.Body == child)
	case *syntax.If:
		w.keepIf(n.Cond != child)
	case *syntax.Binary:
		w.keepIf(n.Op == syntax.OpUpdate && w.mode == walkModule || n.Op == syntax.OpConcat && (w.mode == walkModuleList || w.mode == walkMergedList))
	case *syntax.Apply:
		w.apply(n, child)
	case *syntax.List:
		w.list()
	case *syntax.Assign:
		for _, a := range n.Path {
			if a == child {
				return
			}
			w.enter(a)
		}
	default:
		w.mode = walkNone
	}
}

func (w *optionWalk) list() {
	switch w.mode {
	case walkModuleList:
		w.mode, w.top = walkModule, true
	case walkMergedList:
		w.mode = walkModule
	case walkNone, walkFlakeFile, walkFlakeOutputs, walkModule, walkCallArgs:
		w.mode = walkNone
	}
}

func (w *optionWalk) apply(n *syntax.Apply, child syntax.Node) {
	if n.Args[len(n.Args)-1] != child {
		w.mode = walkNone
		return
	}
	name := calleeName(syntax.Unparenthesized(n.Fn))
	if class, ok := rootClasses[name]; ok {
		w.mode, w.class, w.path = walkCallArgs, optionClassNamed(class), nil
		return
	}
	switch {
	case name == mkFlakeName:
		w.into(ClassFlakeParts)
	case name == mkMergeName && w.mode == walkModule:
		w.mode = walkMergedList
	case slices.Contains(moduleWrappers, name) && w.mode == walkModule:
	default:
		w.mode = walkNone
	}
}

func (w *optionWalk) into(class OptionClass) {
	w.mode, w.class, w.path, w.top = walkModule, class, nil, true
}

func (w *optionWalk) enter(a syntax.Attr) {
	name, ok := syntax.AttrName(a)
	if !ok {
		name = anyAttrName
	}
	switch w.mode {
	case walkFlakeFile:
		w.mode = walkNone
		if name == outputsAttr {
			w.mode = walkFlakeOutputs
		}
	case walkFlakeOutputs:
		w.path = append(slices.Clip(w.path), name)
		if class, ok := moduleOutputClass(w.path); ok {
			w.into(class)
		}
	case walkCallArgs:
		w.mode = walkNone
		if name == modulesAttr {
			w.mode = walkModuleList
		}
	case walkModule:
		w.enterModule(name)
	case walkNone, walkModuleList, walkMergedList:
		w.mode = walkNone
	}
}

func (w *optionWalk) enterModule(name string) {
	if w.top {
		w.top = false
		switch {
		case name == configFormal:
			return
		case name == importsAttr:
			w.mode = walkModuleList
			return
		case slices.Contains(moduleKeys, name):
			w.mode = walkNone
			return
		}
	}
	w.path = append(slices.Clip(w.path), name)
	if w.class == ClassUnknown || w.class == ClassFlakeParts {
		w.flakePartsTransition()
	}
	if w.class == ClassUnknown || w.class == ClassNixOS {
		w.homeManagerTransition()
	}
}

func (w *optionWalk) flakePartsTransition() {
	path := w.path
	switch {
	case len(path) == 1 && path[0] == perSystemAttr:
		w.class, w.top = ClassFlakeParts, true
	case path[0] != flakeAttr:
	case len(path) == 1:
		w.class = ClassFlakeParts
	case len(path) == 4 && path[1] == modulesAttr:
		w.into(optionClassNamed(path[2]))
	default:
		if class, ok := moduleOutputClass(path[1:]); ok {
			w.into(class)
		}
	}
}

func (w *optionWalk) homeManagerTransition() {
	path := w.path
	switch {
	case len(path) == 3 && path[0] == homeManager && path[1] == usersAttr:
		w.into(ClassHomeManager)
	case len(path) == 2 && path[0] == homeManager && path[1] == sharedAttr:
		w.into(ClassHomeManager)
		w.mode = walkModuleList
	}
}

func moduleOutputClass(path []string) (OptionClass, bool) {
	for _, o := range moduleOutputs {
		if o.name == path[0] && (o.single && len(path) == 1 || !o.single && len(path) == 2) {
			return optionClassNamed(o.class), true
		}
	}
	return ClassUnknown, false
}

type evaluatedRoot struct {
	nixpkgs bool
	site    OptionSite
	path    []string
}

func (r *reading) rootOf(chain []syntax.Node, e syntax.Expr, hops int) (evaluatedRoot, bool) {
	if hops == 0 {
		return evaluatedRoot{}, false
	}
	switch e := syntax.Unparenthesized(e).(type) {
	case *syntax.Select:
		root, ok := r.rootOf(chain, e.Target, hops-1)
		names, static := attrNames(e.Path)
		if !ok || !static {
			return evaluatedRoot{}, false
		}
		root.path = slices.Concat(root.path, names)
		return root, true
	case *syntax.Ident:
		return r.identRoot(chain, e, hops)
	}
	return evaluatedRoot{}, false
}

func (r *reading) identRoot(chain []syntax.Node, id *syntax.Ident, hops int) (evaluatedRoot, bool) {
	if ref, ok := r.table.Of(id); ok && ref.Kind == scope.Definition {
		if root, ok := r.boundRoot(chain, ref.Def, hops); ok {
			return root, true
		}
	}
	switch id.Name {
	case pkgsName:
		return evaluatedRoot{nixpkgs: true}, true
	case libName:
		return evaluatedRoot{nixpkgs: true, path: []string{libName}}, true
	}
	return evaluatedRoot{}, false
}

func (r *reading) boundRoot(chain []syntax.Node, d *scope.Def, hops int) (evaluatedRoot, bool) {
	switch d.Kind {
	case scope.FormalDef:
		if d.Name != configFormal && d.Name != optionsFormal {
			return evaluatedRoot{}, false
		}
		return formalRoot(chain, d)
	case scope.LetDef, scope.RecDef, scope.InheritDef:
		origins := r.originsOf(d)
		if len(origins) != 1 {
			return evaluatedRoot{}, false
		}
		return r.originRoot(chain, origins[0], hops-1)
	case scope.ArgDef, scope.AliasDef:
	}
	return evaluatedRoot{}, false
}

func (r *reading) originRoot(chain []syntax.Node, o origin, hops int) (evaluatedRoot, bool) {
	switch {
	case o.assign != nil && o.depth == len(o.assign.Path):
		return r.rootOf(chain, o.assign.Value, hops)
	case o.inherit != nil && o.inherit.From != nil:
		root, ok := r.rootOf(chain, o.inherit.From.Inner, hops)
		name, static := syntax.AttrName(o.attr)
		if !ok || !static {
			return evaluatedRoot{}, false
		}
		root.path = slices.Concat(root.path, []string{name})
		return root, true
	}
	return evaluatedRoot{}, false
}

func formalRoot(chain []syntax.Node, d *scope.Def) (evaluatedRoot, bool) {
	for i, n := range chain {
		lambda, ok := n.(*syntax.Lambda)
		if !ok {
			continue
		}
		pattern, ok := lambda.Param.(*syntax.PatternParam)
		if !ok || !slices.ContainsFunc(pattern.Formals, func(f *syntax.Formal) bool { return syntax.Node(f.Name) == d.Binders[0] }) {
			continue
		}
		site, inModule := optionSiteAt(chain, i)
		return evaluatedRoot{site: site}, inModule
	}
	return evaluatedRoot{}, false
}

func attrNames(path syntax.AttrPath) ([]string, bool) {
	out := make([]string, len(path))
	for i, a := range path {
		name, ok := syntax.AttrName(a)
		if !ok {
			return nil, false
		}
		out[i] = name
	}
	return out, true
}

func (c *cursor) evaluatedMembers(target syntax.Expr, path []string) []Item {
	root, ok := c.rootOf(c.chain, target, maxRootHops)
	if !ok {
		return nil
	}
	full := slices.Concat(root.path, path)
	if root.nixpkgs {
		return c.nixpkgsItems(full)
	}
	return optionItems(c.known.OptionChildren(OptionSite{Class: root.site.Class, Path: slices.Concat(root.site.Path, full)}))
}

func (c *cursor) nixpkgsItems(parent []string) []Item {
	names := c.known.NixpkgsChildren(parent, c.prefix)
	out := make([]Item, len(names))
	for i, name := range names {
		path := append(slices.Clip(parent), name)
		out[i] = Item{Label: attrLabel(name), Kind: model.KindAttribute, Detail: pkgsName + "." + strings.Join(path, "."), packagePath: path}
	}
	return out
}

func (c *cursor) optionKeys() []Item {
	site, ok := optionSiteAt(c.chain, len(c.chain)-1)
	if !ok {
		return nil
	}
	return optionItems(c.known.OptionChildren(site))
}

func optionItems(children []OptionChild) []Item {
	out := make([]Item, len(children))
	for i, child := range children {
		sources := strings.Join(child.Sources, sourceSeparator)
		out[i] = Item{Label: attrLabel(child.Name), Kind: model.KindAttribute, Detail: sources}
		if child.Option != nil {
			out[i].Detail = child.Option.Type + " (" + sources + ")"
			out[i].Doc = optionMarkdown(SourcedOption{Option: *child.Option, Sources: child.Sources})
		}
	}
	return out
}

func attrLabel(name string) string {
	if ValidName(model.KindAttribute, name) {
		return name
	}
	return `"` + name + `"`
}

func (d *Document) evaluatedHover(span syntax.Span, known Evaluated) (Hover, bool) {
	chain := chainTo(d.tree, span)
	if len(chain) < 2 {
		return Hover{}, false
	}
	markdown, ok := newReading(d, chain, NoFiles).evaluatedDoc(chain, known)
	if !ok {
		return Hover{}, false
	}
	return Hover{Markdown: markdown, Span: span}, true
}

func (r *reading) evaluatedDoc(chain []syntax.Node, known Evaluated) (string, bool) {
	switch leaf := chain[len(chain)-1].(type) {
	case *syntax.Ident:
		ref, ok := r.table.Of(leaf)
		if !ok || ref.Kind != scope.Dynamic {
			return "", false
		}
		for _, with := range r.table.Withs(ref.With) {
			if root, ok := r.rootOf(chain, with.Scope, maxRootHops); ok && root.nixpkgs {
				return packageDoc(known, slices.Concat(root.path, []string{leaf.Name}))
			}
		}
	case *syntax.IdentAttr:
		switch parent := chain[len(chain)-2].(type) {
		case *syntax.Assign:
			site, ok := optionSiteAt(chain, len(chain)-1)
			if !ok {
				return "", false
			}
			return optionDoc(known, OptionSite{Class: site.Class, Path: append(slices.Clip(site.Path), leaf.Name)})
		case *syntax.Select:
			return r.selectedDoc(chain, known, parent.Target, parent.Path, leaf)
		case *syntax.HasAttr:
			return r.selectedDoc(chain, known, parent.Target, parent.Path, leaf)
		case *syntax.Inherit:
			if parent.From != nil {
				return r.selectedDoc(chain, known, parent.From.Inner, syntax.AttrPath{leaf}, leaf)
			}
		}
	}
	return "", false
}

func (r *reading) selectedDoc(chain []syntax.Node, known Evaluated, target syntax.Expr, path syntax.AttrPath, leaf syntax.Attr) (string, bool) {
	root, ok := r.rootOf(chain, target, maxRootHops)
	if !ok {
		return "", false
	}
	i := slices.IndexFunc(path, func(a syntax.Attr) bool { return a == leaf })
	names, static := attrNames(path[:i+1])
	if !static {
		return "", false
	}
	full := slices.Concat(root.path, names)
	if root.nixpkgs {
		return packageDoc(known, full)
	}
	return optionDoc(known, OptionSite{Class: root.site.Class, Path: slices.Concat(root.site.Path, full)})
}

func optionDoc(known Evaluated, site OptionSite) (string, bool) {
	option, ok := known.OptionAt(site)
	if !ok {
		return "", false
	}
	return optionMarkdown(option), true
}

func packageDoc(known Evaluated, path []string) (string, bool) {
	pkg, ok := known.NixpkgsPackage(path)
	if !ok {
		return "", false
	}
	return packageMarkdown(path, pkg), true
}

func fenced(code string) string { return hoverFence + code + "\n```" }

func optionMarkdown(o SourcedOption) string {
	heading := strings.Join(o.Option.Path, ".")
	if o.Option.Type != "" {
		heading += " : " + o.Option.Type
	}
	parts := []string{fenced(heading)}
	if o.Option.Description != "" {
		parts = append(parts, o.Option.Description)
	}
	parts = append(parts, valueParts("Default", o.Option.Default)...)
	parts = append(parts, valueParts("Example", o.Option.Example)...)
	for _, d := range o.Option.Declarations {
		parts = append(parts, "Declared in "+declarationText(d))
	}
	if len(o.Sources) > 0 {
		parts = append(parts, "From "+strings.Join(o.Sources, sourceSeparator))
	}
	return strings.Join(parts, "\n\n")
}

func valueParts(label, value string) []string {
	switch {
	case value == "":
		return nil
	case strings.ContainsAny(value, "\n`"):
		return []string{label + ":", fenced(value)}
	}
	return []string{label + ": `" + value + "`"}
}

func declarationText(d nixeval.Declaration) string {
	if d.Line == 0 {
		return "`" + d.File + "`"
	}
	return fmt.Sprintf("`%s:%d`", d.File, d.Line)
}

func packageMarkdown(path []string, pkg *nixeval.Package) string {
	parts := []string{fenced(pkgsName + "." + strings.Join(path, "."))}
	switch {
	case pkg.Name != "" && pkg.Version != "" && !strings.HasSuffix(pkg.Name, pkg.Version):
		parts = append(parts, pkg.Name+" "+pkg.Version)
	case pkg.Name != "":
		parts = append(parts, pkg.Name)
	}
	if pkg.Description != "" {
		parts = append(parts, pkg.Description)
	}
	if pkg.Declaration.File != "" {
		parts = append(parts, "Declared in "+declarationText(pkg.Declaration))
	}
	return strings.Join(parts, "\n\n")
}
