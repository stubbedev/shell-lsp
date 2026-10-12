package lint

import (
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type nameReads struct {
	names    map[string]bool
	prefixes []string
	anyName  bool
}

func (r *nameReads) reads(name string) bool {
	if r.anyName || r.names[name] {
		return true
	}
	for _, prefix := range r.prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func (p *pass) namesReadByValue() *nameReads {
	if p.nameReads == nil {
		trees := make([]*parser.Tree, 0, 1+len(p.imports))
		trees = append(trees, p.tree)
		for _, imp := range p.imports {
			trees = append(trees, imp.Tree)
		}
		p.nameReads = analyzeNameReads(trees)
	}
	return p.nameReads
}

type heldName struct {
	tree *parser.Tree
	name scopedName
}

type nameFlow struct {
	current *parser.Tree
	reads   nameReads
	params  map[string]map[int]bool
	shifts  map[string]bool
	anyFunc map[string]bool
	held    map[heldName]bool
	changed bool
}

func analyzeNameReads(trees []*parser.Tree) *nameReads {
	f := &nameFlow{
		current: trees[0],
		reads:   nameReads{names: map[string]bool{}},
		params:  map[string]map[int]bool{},
		shifts:  map[string]bool{},
		anyFunc: map[string]bool{},
		held:    map[heldName]bool{},
	}
	for _, tree := range trees {
		f.seed(tree)
	}
	for f.changed = true; f.changed; {
		f.changed = false
		for _, tree := range trees {
			f.calls(tree)
		}
	}
	return &f.reads
}

func (f *nameFlow) seed(tree *parser.Tree) {
	for _, cmd := range tree.Commands {
		if fn, ok := tree.EnclosingFunction(cmd.Within); ok && cmd.Name == "shift" {
			f.shifts[fn.Name] = true
		}
	}
	for i := range tree.Definitions {
		if def := &tree.Definitions[i]; def.Attrs.Has(parser.AttrNameref) && def.Value != nil {
			f.need(tree, def.Value, def.Within)
		}
	}
	parser.Walk(tree.File, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.ParamExp:
			f.indirect(tree, n)
		case *syntax.DeclClause:
			f.printed(tree, n)
		}
		return true
	})
}

func (f *nameFlow) indirect(tree *parser.Tree, pe *syntax.ParamExp) {
	if !pe.Excl || pe.Param == nil || pe.Index != nil || tree.Dialect != dialect.Bash {
		return
	}
	if pe.Names != 0 {
		f.reads.prefixes = append(f.reads.prefixes, pe.Param.Value)
		return
	}
	f.param(tree, pe.Param.Value, tree.ScopeAt(parser.Offset(pe.Pos())))
}

func (f *nameFlow) printed(tree *parser.Tree, d *syntax.DeclClause) {
	operands, printing := printedOperands(d)
	if !printing {
		return
	}
	scope := tree.ScopeAt(parser.Offset(d.Pos()))
	if len(operands) == 0 {
		f.unknown(tree, scope)
	}
	for _, arg := range operands {
		if arg.Name == nil && arg.Value != nil {
			f.need(tree, arg.Value, scope)
		}
	}
}

func (f *nameFlow) calls(tree *parser.Tree) {
	for _, cmd := range tree.Commands {
		if f.anyFunc[cmd.Name] {
			f.unknown(tree, cmd.Within)
		}
		for position := range f.params[cmd.Name] {
			for _, arg := range f.arguments(cmd, position) {
				f.need(tree, arg, cmd.Within)
			}
		}
	}
}

func (f *nameFlow) arguments(cmd parser.Command, position int) []*syntax.Word {
	switch {
	case position == everyPosition:
		return cmd.Args
	case position > len(cmd.Args):
		return nil
	case f.shifts[cmd.Name]:
		return cmd.Args[position-1:]
	}
	return cmd.Args[position-1 : position]
}

func (f *nameFlow) need(tree *parser.Tree, w *syntax.Word, scope parser.ScopeID) {
	if lit, ok := parser.Literal(w); ok {
		if name, _, _ := strings.Cut(lit, "["); parser.IsName(name) {
			f.reads.names[name] = true
		}
		return
	}
	if pe, ok := solePart(w).(*syntax.ParamExp); ok && plainOrElement(pe) {
		f.param(tree, pe.Param.Value, scope)
		return
	}
	prefix := leadingLiteral(w)
	if prefix == "" {
		f.unknown(tree, scope)
		return
	}
	base, _, subscripted := strings.Cut(prefix, "[")
	switch {
	case subscripted && parser.IsName(base):
		f.reads.names[base] = true
	case !subscripted && leadingName(prefix) == prefix:
		f.reads.prefixes = append(f.reads.prefixes, prefix)
	}
}

func plainOrElement(pe *syntax.ParamExp) bool {
	whole := *pe
	whole.Index = nil
	return parser.IsPlainParam(&whole)
}

func (f *nameFlow) param(tree *parser.Tree, name string, scope parser.ScopeID) {
	switch {
	case name == "@" || name == "*":
		f.positional(tree, everyPosition, scope)
	case name != "0" && isPositional(name):
		n, err := strconv.Atoi(name)
		if err != nil {
			f.unknown(tree, scope)
			return
		}
		f.positional(tree, n, scope)
	case parser.IsName(name):
		f.hold(tree, name, scope)
	default:
		f.unknown(tree, scope)
	}
}

func (f *nameFlow) positional(tree *parser.Tree, n int, scope parser.ScopeID) {
	fn, ok := tree.EnclosingFunction(scope)
	if !ok {
		f.unknown(tree, scope)
		return
	}
	if f.params[fn.Name] == nil {
		f.params[fn.Name] = map[int]bool{}
	}
	if !f.params[fn.Name][n] {
		f.params[fn.Name][n] = true
		f.changed = true
	}
}

func (f *nameFlow) unknown(tree *parser.Tree, scope parser.ScopeID) {
	fn, ok := tree.EnclosingFunction(scope)
	switch {
	case ok && !f.anyFunc[fn.Name]:
		f.anyFunc[fn.Name] = true
		f.changed = true
	case !ok && tree == f.current:
		f.reads.anyName = true
	}
}

func (f *nameFlow) hold(tree *parser.Tree, name string, scope parser.ScopeID) {
	key := heldName{tree: tree, name: bindingKey(tree, name, scope)}
	if f.held[key] {
		return
	}
	f.held[key] = true
	defs := tree.Visible(name, scope)
	if defs.Len() == 0 {
		f.unknown(tree, scope)
		return
	}
	for def := range defs.All() {
		f.defined(tree, def)
	}
}

func (f *nameFlow) defined(tree *parser.Tree, def *parser.Definition) {
	switch def.Kind {
	case parser.DefAssign, parser.DefDeclare, parser.DefCommandEnv:
		if assign, ok := def.Node.(*syntax.Assign); ok && assign.Array != nil {
			for _, elem := range assign.Array.Elems {
				if elem.Value != nil {
					f.need(tree, elem.Value, def.Within)
				}
			}
			return
		}
		if def.Value != nil {
			f.need(tree, def.Value, def.Within)
		}
	case parser.DefLoop:
		iter, ok := def.Node.(*syntax.WordIter)
		switch {
		case !ok:
			f.unknown(tree, def.Within)
		case !parser.ListsWords(iter):
			f.positional(tree, everyPosition, def.Within)
		default:
			for _, item := range iter.Items {
				f.need(tree, item, def.Within)
			}
		}
	case parser.DefArithmetic:
	default:
		f.unknown(tree, def.Within)
	}
}
