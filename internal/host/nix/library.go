package nix

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/stubbedev/shell-lsp/internal/host/nix/builtins"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/nixeval"
)

const (
	libraryEntry    = "lib/default.nix"
	libraryMaxSteps = 4096
	byNameShard     = 2
	metaAttr        = "meta"
	descriptionAttr = "description"
)

var attrPreserving = []string{"mapAttrs", "recurseIntoAttrs", "dontRecurseIntoAttrs"}

var packageSetFiles = []string{"pkgs/top-level/all-packages.nix", "pkgs/build-support/trivial-builders/default.nix"}

var libraryFixers = []string{"makeExtensible", "makeExtensible'", "makeScope", "fix", "fix'", "extends"}

type LibraryEntry struct {
	Declaration nixeval.Declaration
	Doc         string
	Builtin     string
}

type libraryFile struct {
	path  string
	tree  *syntax.File
	table *scope.Table
}

type libraryValue struct {
	file *libraryFile
	expr syntax.Expr
}

type librarySet struct {
	file *libraryFile
	set  *syntax.AttrSet
}

type libraryHit struct {
	file    *libraryFile
	at      syntax.Span
	comment int
	value   libraryValue
	builtin string
}

type Library struct {
	mu     sync.Mutex
	dir    string
	read   func(path string) (string, bool)
	files  map[string]*libraryFile
	selves map[*scope.Def]libraryValue
	steps  int
}

func NewLibrary(nixpkgsDir string, read func(path string) (string, bool)) *Library {
	return &Library{dir: nixpkgsDir, read: read, files: map[string]*libraryFile{}, selves: map[*scope.Def]libraryValue{}}
}

func (l *Library) load(file string) (*libraryFile, bool) {
	if f, ok := l.files[file]; ok {
		return f, f != nil
	}
	text, ok := l.read(file)
	if !ok {
		l.files[file] = nil
		return nil, false
	}
	tree := syntax.Parse(text)
	f := &libraryFile{path: file, tree: tree, table: scope.Resolve(tree)}
	l.files[file] = f
	return f, true
}

func (l *Library) root() []librarySet {
	f, ok := l.load(filepath.Join(l.dir, filepath.FromSlash(libraryEntry)))
	if !ok {
		return nil
	}
	return l.sets(libraryValue{file: f, expr: f.tree.Root})
}

func (l *Library) spend() bool {
	l.steps++
	return l.steps <= libraryMaxSteps
}

func (l *Library) sets(v libraryValue) []librarySet {
	if v.expr == nil || !l.spend() {
		return nil
	}
	switch e := syntax.Unparenthesized(v.expr).(type) {
	case *syntax.AttrSet:
		return []librarySet{{file: v.file, set: e}}
	case *syntax.Lambda:
		return l.sets(libraryValue{file: v.file, expr: e.Body})
	case *syntax.Let:
		return l.sets(libraryValue{file: v.file, expr: e.Body})
	case *syntax.With:
		return l.sets(libraryValue{file: v.file, expr: e.Body})
	case *syntax.Assert:
		return l.sets(libraryValue{file: v.file, expr: e.Body})
	case *syntax.Binary:
		if e.Op != syntax.OpUpdate {
			return nil
		}
		return slices.Concat(l.sets(libraryValue{file: v.file, expr: e.Right}), l.sets(libraryValue{file: v.file, expr: e.Left}))
	case *syntax.Apply:
		return l.applied(v.file, e)
	case *syntax.Ident:
		return l.named(v.file, e)
	case *syntax.Select:
		return l.selected(v.file, e)
	}
	return nil
}

func (l *Library) applied(f *libraryFile, call *syntax.Apply) []librarySet {
	if p, hasPath := loadedPath(call); hasPath {
		loaded, ok := l.loadAt(f, p)
		if !ok {
			return nil
		}
		return l.sets(libraryValue{file: loaded, expr: loaded.tree.Root})
	}
	if slices.Contains(attrPreserving, calleeName(syntax.Unparenthesized(call.Fn))) {
		return l.sets(libraryValue{file: f, expr: call.Args[len(call.Args)-1]})
	}
	if !fixesItsArgument(f.table, call) {
		return nil
	}
	fn, isLambda := syntax.Unparenthesized(call.Args[len(call.Args)-1]).(*syntax.Lambda)
	if !isLambda {
		return nil
	}
	if param, named := fn.Param.(*syntax.IdentParam); named {
		if d, bound := f.table.Binder(param.Name); bound {
			l.selves[d] = libraryValue{file: f, expr: fn.Body}
		}
	}
	return slices.Concat(l.fixerAttrs(f, call), l.sets(libraryValue{file: f, expr: fn.Body}))
}

func loadedPath(call *syntax.Apply) (*syntax.Path, bool) {
	for _, arg := range call.Args {
		if p, isPath := syntax.Unparenthesized(arg).(*syntax.Path); isPath && loadable(p) {
			return p, true
		}
	}
	return nil, false
}

func (l *Library) loadAt(f *libraryFile, p *syntax.Path) (*libraryFile, bool) {
	span := p.Span()
	target := filepath.Join(filepath.Dir(f.path), filepath.FromSlash(f.tree.Text[span.Start:span.End]))
	if loaded, ok := l.load(target); ok {
		return loaded, true
	}
	return l.load(filepath.Join(target, DirectoryEntry))
}

func (l *Library) fixerAttrs(f *libraryFile, call *syntax.Apply) []librarySet {
	id, plain := syntax.Unparenthesized(call.Fn).(*syntax.Ident)
	if !plain {
		return nil
	}
	value, bound := localValue(f.table, id)
	if !bound {
		return nil
	}
	return l.sets(libraryValue{file: f, expr: value})
}

func fixesItsArgument(table *scope.Table, call *syntax.Apply) bool {
	fn := syntax.Unparenthesized(call.Fn)
	if !slices.Contains(libraryFixers, calleeName(fn)) {
		return false
	}
	if !definedHere(table, fn) {
		return true
	}
	id, plain := fn.(*syntax.Ident)
	if !plain {
		return false
	}
	value, bound := localValue(table, id)
	return bound && appliesParameter(table, value, len(call.Args))
}

func appliesParameter(table *scope.Table, value syntax.Expr, position int) bool {
	var lambda *syntax.Lambda
	for range position {
		next, isLambda := syntax.Unparenthesized(value).(*syntax.Lambda)
		if !isLambda {
			return false
		}
		lambda, value = next, next.Body
	}
	param, named := lambda.Param.(*syntax.IdentParam)
	if !named {
		return false
	}
	own, bound := table.Binder(param.Name)
	if !bound {
		return false
	}
	found := false
	syntax.Inspect(lambda.Body, func(n syntax.Node) bool {
		call, isCall := n.(*syntax.Apply)
		if !isCall {
			return !found
		}
		if id, plain := syntax.Unparenthesized(call.Fn).(*syntax.Ident); plain {
			ref, ok := table.Of(id)
			found = found || ok && ref.Kind == scope.Definition && ref.Def == own
		}
		return !found
	})
	return found
}

func (l *Library) named(f *libraryFile, id *syntax.Ident) []librarySet {
	ref, ok := f.table.Of(id)
	if !ok || ref.Kind != scope.Definition {
		return nil
	}
	if self, known := l.selves[ref.Def]; known {
		return l.sets(self)
	}
	if ref.Def.Name == LibName && isFileFormal(f, ref.Def) {
		return l.root()
	}
	if value, bound := singleValue(ref.Def); bound {
		return l.sets(libraryValue{file: f, expr: value})
	}
	if len(ref.Def.Bindings) == 1 {
		if stmt, inherited := ref.Def.Bindings[0].(*syntax.Inherit); inherited {
			return l.follow(l.inheritedHit(f, stmt, ref.Def.Name))
		}
	}
	return nil
}

func isFileFormal(f *libraryFile, d *scope.Def) bool {
	lambda, isLambda := syntax.Unparenthesized(f.tree.Root).(*syntax.Lambda)
	if !isLambda {
		return false
	}
	pattern, patterned := lambda.Param.(*syntax.PatternParam)
	return patterned && slices.ContainsFunc(pattern.Formals, func(formal *syntax.Formal) bool {
		own, bound := f.table.Binder(formal.Name)
		return bound && own == d
	})
}

func (l *Library) follow(hit libraryHit, ok bool) []librarySet {
	if !ok {
		return nil
	}
	return l.sets(hit.value)
}

func (l *Library) selected(f *libraryFile, e *syntax.Select) []librarySet {
	current := l.sets(libraryValue{file: f, expr: e.Target})
	for _, attr := range e.Path {
		name, static := syntax.AttrName(attr)
		if !static {
			return nil
		}
		hit, ok := l.lookup(current, name)
		if !ok {
			return nil
		}
		current = l.sets(hit.value)
	}
	return current
}

func (l *Library) inheritedHit(f *libraryFile, stmt *syntax.Inherit, name string) (libraryHit, bool) {
	if stmt.From != nil {
		if isGlobalBuiltins(f.table, stmt.From.Inner) {
			return libraryHit{builtin: name}, true
		}
		return l.lookup(l.sets(libraryValue{file: f, expr: stmt.From.Inner}), name)
	}
	for _, attr := range stmt.Attrs {
		if own, static := syntax.AttrName(attr); !static || own != name {
			continue
		}
		ref, ok := f.table.Inherited(attr)
		if !ok {
			return libraryHit{}, false
		}
		switch ref.Kind {
		case scope.Global:
			return libraryHit{builtin: name}, true
		case scope.Definition:
			return l.definedHit(f, ref.Def)
		case scope.Undefined, scope.Dynamic:
		}
	}
	return libraryHit{}, false
}

func (l *Library) definedHit(f *libraryFile, d *scope.Def) (libraryHit, bool) {
	if len(d.Binders) == 0 || len(d.Bindings) != 1 {
		return libraryHit{}, false
	}
	if stmt, inherited := d.Bindings[0].(*syntax.Inherit); inherited {
		return l.inheritedHit(f, stmt, d.Name)
	}
	hit := libraryHit{file: f, at: d.Binders[0].Span(), comment: d.Bindings[0].Span().Start}
	if value, bound := singleValue(d); bound {
		hit.value = libraryValue{file: f, expr: value}
	}
	return hit, true
}

func (l *Library) lookup(sets []librarySet, name string) (libraryHit, bool) {
	if !l.spend() {
		return libraryHit{}, false
	}
	for _, s := range sets {
		for _, b := range s.set.Bindings {
			switch b := b.(type) {
			case *syntax.Assign:
				if own, static := syntax.AttrName(b.Path[0]); static && own == name {
					hit := libraryHit{file: s.file, at: nameSpan(b.Path[0]), comment: b.Span().Start}
					if len(b.Path) == 1 {
						hit.value = libraryValue{file: s.file, expr: b.Value}
					}
					return hit, true
				}
			case *syntax.Inherit:
				if slices.ContainsFunc(b.Attrs, func(a syntax.Attr) bool {
					own, static := syntax.AttrName(a)
					return static && own == name
				}) {
					return l.inheritedHit(s.file, b, name)
				}
			}
		}
	}
	return libraryHit{}, false
}

func (l *Library) Lookup(names []string) (LibraryEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.steps = 0
	hit, ok := l.walk(l.root(), names)
	if !ok {
		return LibraryEntry{}, false
	}
	return hit.entry()
}

func (l *Library) walk(current []librarySet, names []string) (libraryHit, bool) {
	var hit libraryHit
	for _, name := range names {
		found, ok := l.lookup(current, name)
		if !ok {
			return libraryHit{}, false
		}
		hit = found
		current = l.sets(hit.value)
	}
	return hit, hit.builtin != "" || hit.file != nil
}

func (hit libraryHit) entry() (LibraryEntry, bool) {
	if hit.builtin != "" {
		return LibraryEntry{Builtin: hit.builtin}, true
	}
	if hit.file == nil {
		return LibraryEntry{}, false
	}
	return LibraryEntry{Declaration: hit.file.declarationAt(hit.at.Start), Doc: cleanDocComment(commentAbove(hit.file.tree, hit.comment))}, true
}

func (f *libraryFile) declarationAt(at int) nixeval.Declaration {
	text := f.tree.Text
	return nixeval.Declaration{File: f.path, Line: strings.Count(text[:at], "\n") + 1, Column: at - (strings.LastIndexByte(text[:at], '\n') + 1) + 1}
}

func (l *Library) Package(names []string) (LibraryEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.steps = 0
	for _, set := range packageSetFiles {
		f, ok := l.load(filepath.Join(l.dir, filepath.FromSlash(set)))
		if !ok {
			continue
		}
		if hit, found := l.walk(l.sets(libraryValue{file: f, expr: f.tree.Root}), names); found {
			return l.packageEntry(hit)
		}
	}
	if len(names) != 1 || len(names[0]) < byNameShard {
		return LibraryEntry{}, false
	}
	f, ok := l.load(filepath.Join(l.dir, "pkgs", "by-name", names[0][:byNameShard], names[0], "package.nix"))
	if !ok {
		return LibraryEntry{}, false
	}
	return packageFileEntry(f), true
}

func (l *Library) packageEntry(hit libraryHit) (LibraryEntry, bool) {
	if call, isCall := syntax.Unparenthesized(hit.value.expr).(*syntax.Apply); isCall && strings.HasPrefix(calleeName(syntax.Unparenthesized(call.Fn)), callPackageName) {
		if p, hasPath := loadedPath(call); hasPath {
			if target, ok := l.loadAt(hit.value.file, p); ok {
				return packageFileEntry(target), true
			}
		}
	}
	return hit.entry()
}

func packageFileEntry(f *libraryFile) LibraryEntry {
	return LibraryEntry{Declaration: nixeval.Declaration{File: f.path, Line: 1, Column: 1}, Doc: metaDescription(f.tree)}
}

func metaDescription(tree *syntax.File) string {
	found := ""
	syntax.Inspect(tree.Root, func(n syntax.Node) bool {
		assign, isAssign := n.(*syntax.Assign)
		if found != "" || !isAssign {
			return found == ""
		}
		path, static := attrNames(assign.Path)
		switch {
		case !static:
		case slices.Equal(path, []string{metaAttr, descriptionAttr}):
			found = stringValue(assign.Value)
		case slices.Equal(path, []string{metaAttr}):
			if set, isSet := syntax.Unparenthesized(assign.Value).(*syntax.AttrSet); isSet {
				found = describedIn(set)
			}
		}
		return found == ""
	})
	return found
}

func describedIn(set *syntax.AttrSet) string {
	for _, binding := range set.Bindings {
		assign, isAssign := binding.(*syntax.Assign)
		if !isAssign {
			continue
		}
		if path, static := attrNames(assign.Path); static && slices.Equal(path, []string{descriptionAttr}) {
			return stringValue(assign.Value)
		}
	}
	return ""
}

func stringValue(e syntax.Expr) string {
	str, literal := syntax.Unparenthesized(e).(*syntax.String)
	if !literal {
		return ""
	}
	return literalOf(str)
}

func (l *Library) Members(names []string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.steps = 0
	current := l.root()
	for _, name := range names {
		found, ok := l.lookup(current, name)
		if !ok {
			return nil
		}
		current = l.sets(found.value)
	}
	var out []string
	for _, s := range current {
		for _, b := range s.set.Bindings {
			switch b := b.(type) {
			case *syntax.Assign:
				if own, static := syntax.AttrName(b.Path[0]); static {
					out = append(out, own)
				}
			case *syntax.Inherit:
				for _, a := range b.Attrs {
					if own, static := syntax.AttrName(a); static {
						out = append(out, own)
					}
				}
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func PackageMarkdown(names []string, entry LibraryEntry) string {
	parts := []string{fenced(pkgsName + "." + strings.Join(names, "."))}
	if entry.Doc != "" {
		parts = append(parts, entry.Doc)
	}
	return strings.Join(append(parts, "Defined in "+declarationText(entry.Declaration)), "\n\n")
}

func cleanDocComment(doc string) string {
	doc = strings.TrimPrefix(doc, "*")
	lines := strings.Split(doc, "\n")
	indent := -1
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) != "" {
			n := leadingSpaces(line)
			if indent < 0 || n < indent {
				indent = n
			}
		}
	}
	for i, line := range lines {
		if i > 0 && indent > 0 && len(line) >= indent {
			line = line[indent:]
		}
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (d *Document) NixpkgsPathAt(offset int) ([]string, syntax.Span, bool) {
	token, ok := d.tokenAt(offset)
	if !ok {
		return nil, syntax.Span{}, false
	}
	names, found := d.nixpkgsPath(token.Span)
	return names, token.Span, found
}

func (d *Document) nixpkgsPath(span syntax.Span) ([]string, bool) {
	chain := chainTo(d.tree, span)
	if len(chain) < 2 {
		return nil, false
	}
	r := newReading(d, chain, NoFiles)
	switch leaf := chain[len(chain)-1].(type) {
	case *syntax.Ident:
		ref, ok := r.table.Of(leaf)
		if !ok || ref.Kind != scope.Dynamic {
			return nil, false
		}
		for _, with := range r.table.Withs(ref.With) {
			if root, known := r.rootOf(chain, with.Scope, maxRootHops); known && root.nixpkgs {
				return slices.Concat(root.path, []string{leaf.Name}), true
			}
		}
	case *syntax.IdentAttr:
		switch parent := chain[len(chain)-2].(type) {
		case *syntax.Select:
			return r.nixpkgsSelected(chain, parent.Target, parent.Path, leaf)
		case *syntax.Inherit:
			if parent.From != nil {
				return r.nixpkgsSelected(chain, parent.From.Inner, syntax.AttrPath{leaf}, leaf)
			}
		}
	}
	return nil, false
}

func (r *reading) nixpkgsSelected(chain []syntax.Node, target syntax.Expr, attrs syntax.AttrPath, leaf syntax.Attr) ([]string, bool) {
	root, ok := r.rootOf(chain, target, maxRootHops)
	if !ok || !root.nixpkgs {
		return nil, false
	}
	i := slices.IndexFunc(attrs, func(a syntax.Attr) bool { return a == leaf })
	names, static := attrNames(attrs[:i+1])
	if !static {
		return nil, false
	}
	return slices.Concat(root.path, names), true
}

func (d *Document) OptionDeclarations(offset int, known Evaluated) []nixeval.Declaration {
	token, ok := d.tokenAt(offset)
	if !ok {
		return nil
	}
	chain := chainTo(d.tree, token.Span)
	if len(chain) < 2 {
		return nil
	}
	leaf, isAttr := chain[len(chain)-1].(*syntax.IdentAttr)
	if !isAttr {
		return nil
	}
	if _, assigned := chain[len(chain)-2].(*syntax.Assign); !assigned {
		return nil
	}
	r := newReading(d, chain, NoFiles)
	var sites []OptionSite
	if site, inModule := optionSiteAt(d.table, chain, len(chain)-1); inModule {
		sites = append(sites, site)
	}
	sites = append(sites, r.moduleSites(chain, len(chain)-1, known)...)
	for _, site := range sites {
		if option, found := known.OptionAt(OptionSite{Class: site.Class, Path: append(slices.Clip(site.Path), leaf.Name)}); found {
			return option.Option.Declarations
		}
	}
	return nil
}

func LibraryMarkdown(names []string, entry LibraryEntry) (string, bool) {
	if entry.Builtin != "" {
		b, known := builtins.Lookup(entry.Builtin)
		if !known {
			return "", false
		}
		return hoverFence + builtinSignature(b) + "\n```\n\n" + b.Doc + "\n\n" + arity(b), true
	}
	parts := []string{fenced(LibName + "." + strings.Join(names, "."))}
	if entry.Doc != "" {
		parts = append(parts, entry.Doc)
	}
	return strings.Join(append(parts, "Defined in "+declarationText(entry.Declaration)), "\n\n"), true
}
