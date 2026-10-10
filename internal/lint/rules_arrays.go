package lint

import (
	"cmp"
	"slices"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var arraysFamily = family{
	codes: map[Code]meta{
		2128: {severity: SeverityWarning},
		2178: {severity: SeverityWarning},
		2179: {severity: SeverityWarning},
	},
	rules: []rule{{finish: checkArrayUse}},
}

var shellArrays = []string{
	"BASH_ALIASES", "BASH_ARGC", "BASH_ARGV", "BASH_CMDS", "BASH_LINENO", "BASH_REMATCH", "BASH_SOURCE",
	"BASH_VERSINFO", "COMP_WORDS", "COMPREPLY", "COPROC", "DIRSTACK", "FUNCNAME", "GROUPS", "MAPFILE", "PIPESTATUS",
}

type arrayEvent struct {
	at     int
	def    *parser.Definition
	read   parser.Span
	plain  bool
	tested bool
	name   string
}

func checkArrayUse(p *pass) {
	if p.dialect == dialect.Zsh {
		return
	}
	arrays := setOf(shellArrays...)
	if p.bats {
		arrays["lines"] = true
	}
	for _, e := range p.arrayEvents() {
		switch {
		case e.def != nil:
			p.arrayWrite(arrays, e.def)
		case e.plain && arrays[e.name]:
			p.report(2128, e.read, "Expanding the array "+e.name+" without an index gives only its first element; use \"${"+e.name+"[@]}\" for all elements or \"${"+e.name+"[0]}\" for the first.")
		}
		if e.tested {
			delete(arrays, e.name)
		}
	}
}

func (p *pass) arrayEvents() []arrayEvent {
	events := make([]arrayEvent, 0, len(p.tree.Definitions)+len(p.tree.References))
	for i := range p.tree.Definitions {
		def := &p.tree.Definitions[i]
		events = append(events, arrayEvent{at: p.writtenAt(def), def: def})
	}
	for _, ref := range p.tree.References {
		span, plain := p.plainExpansion(ref.Span)
		events = append(events, arrayEvent{at: ref.Span.Start, read: span, plain: plain, tested: p.emptinessTests[ref.Span.Start], name: ref.Name})
	}
	slices.SortStableFunc(events, func(a, b arrayEvent) int { return cmp.Compare(a.at, b.at) })
	return events
}

func (p *pass) writtenAt(def *parser.Definition) int {
	span, ok := p.span(def.Node)
	if !ok {
		return def.Span.End
	}
	if _, isCall := def.Node.(*syntax.CallExpr); isCall {
		if i, found := slices.BinarySearchFunc(p.stmts, span.Start, func(s parser.Span, at int) int { return cmp.Compare(s.Start, at) }); found {
			for i+1 < len(p.stmts) && p.stmts[i+1].Start == span.Start {
				i++
			}
			return max(span.End, p.stmts[i].End)
		}
	}
	return span.End
}

func (p *pass) recordEmptinessTest(w *syntax.Word) {
	for part := range unquotedParts(w) {
		if pe, ok := part.(*syntax.ParamExp); ok && pe.Param != nil {
			if p.emptinessTests == nil {
				p.emptinessTests = map[int]bool{}
			}
			p.emptinessTests[parser.Offset(pe.Param.Pos())] = true
		}
	}
}

func (p *pass) plainExpansion(name parser.Span) (parser.Span, bool) {
	switch {
	case name.Start >= 2 && p.text[name.Start-2:name.Start] == "${" && name.End < len(p.text) && p.text[name.End] == '}':
		return parser.Span{Start: name.Start - 2, End: name.End + 1}, true
	case name.Start >= 1 && p.text[name.Start-1] == '$':
		return parser.Span{Start: name.Start - 1, End: name.End}, true
	}
	return parser.Span{}, false
}

func (p *pass) arrayWrite(arrays map[string]bool, def *parser.Definition) {
	assign, isAssign := def.Node.(*syntax.Assign)
	stringValue := isAssign && !assign.Naked && assign.Value != nil && assign.Index == nil
	if stringValue && arrays[def.Name] {
		p.reportStringToArray(def.Name, assign)
	}
	switch {
	case def.Attrs.Has(parser.AttrArray), def.Attrs.Has(parser.AttrAssoc), isAssign && assign.Index != nil:
		arrays[def.Name] = true
	case !stringValue:
		delete(arrays, def.Name)
	}
}

func (p *pass) reportStringToArray(name string, assign *syntax.Assign) {
	if assign.Append {
		p.reportNode(2179, assign, "Appending a string to the array "+name+" changes its first element; use "+name+"+=(value) to add an element.")
		return
	}
	p.reportNode(2178, assign, "The array "+name+" is assigned a plain string, which only replaces its first element; assign an array with ( ) or use a different variable.")
}
