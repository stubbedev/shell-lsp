package lint

import (
	"slices"
	"strconv"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const codeReassignedGlobal = nativeBase + 2015

var globalReassignmentFamily = family{
	codes: map[Code]meta{
		codeReassignedGlobal: {severity: SeverityStyle, optional: optionalGlobalReassignment},
	},
	nodes:  []nodeRule{onNode(recordAssignmentBranches)},
	finish: []func(*pass){checkGlobalReassignments},
}

const conditionArm = 0

type branchArm struct {
	container syntax.Node
	arm       int
}

func recordAssignmentBranches(p *pass, a *syntax.Assign) {
	if !p.active(codeReassignedGlobal) {
		return
	}
	var arms []branchArm
	for i, n := range p.stack {
		child := syntax.Node(a)
		if i+1 < len(p.stack) {
			child = p.stack[i+1]
		}
		switch n := n.(type) {
		case *syntax.IfClause:
			arms = append(arms, branchArm{container: n, arm: ifArm(n, child)})
		case *syntax.CaseClause:
			arms = append(arms, branchArm{container: n, arm: 1 + slices.IndexFunc(n.Items, func(item *syntax.CaseItem) bool { return syntax.Node(item) == child })})
		}
	}
	if p.assignmentArms == nil {
		p.assignmentArms = map[*syntax.Assign][]branchArm{}
	}
	p.assignmentArms[a] = arms
}

func ifArm(n *syntax.IfClause, child syntax.Node) int {
	switch {
	case n.Else != nil && syntax.Node(n.Else) == child:
		return 2
	case slices.ContainsFunc(n.Then, func(s *syntax.Stmt) bool { return syntax.Node(s) == child }):
		return 1
	}
	return conditionArm
}

func (p *pass) exclusiveBranches(a, b syntax.Node) bool {
	first, okFirst := a.(*syntax.Assign)
	second, okSecond := b.(*syntax.Assign)
	if !okFirst || !okSecond {
		return false
	}
	for _, x := range p.assignmentArms[first] {
		for _, y := range p.assignmentArms[second] {
			if x.container == y.container && x.arm != y.arm && x.arm != conditionArm && y.arm != conditionArm {
				return true
			}
		}
	}
	return false
}

func overwritesGlobal(def *parser.Definition) bool {
	switch def.Kind {
	case parser.DefAssign, parser.DefArithmetic:
		return true
	case parser.DefDeclare:
		a, ok := def.Node.(*syntax.Assign)
		return ok && !a.Naked
	case parser.DefCommandEnv, parser.DefLoop, parser.DefBuiltin, parser.DefDescriptor:
	}
	return false
}

func updatesItself(def *parser.Definition) bool {
	switch n := def.Node.(type) {
	case *syntax.Assign:
		return n.Append || n.Index != nil || n.Value != nil && readsVariable(n.Value, def.Name) || n.Array != nil && readsVariable(n.Array, def.Name)
	case *syntax.UnaryArithm:
		return true
	case *syntax.BinaryArithm:
		return n.Op != syntax.Assgn || readsVariable(n.Y, def.Name)
	}
	return false
}

func readsVariable(root syntax.Node, name string) bool {
	return parser.Any(root, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.ParamExp:
			return n.Param != nil && n.Param.Value == name
		case *syntax.Word:
			found, ok := parser.ArithmeticName(n)
			return ok && found == name
		}
		return false
	})
}

func checkGlobalReassignments(p *pass) {
	if !p.active(codeReassignedGlobal) {
		return
	}
	topLevel := map[string][]*parser.Definition{}
	for i := range p.tree.Definitions {
		def := &p.tree.Definitions[i]
		if def.Within == parser.FileScope && def.Kind != parser.DefCommandEnv && !p.parameters.Has(def.Name) {
			topLevel[def.Name] = append(topLevel[def.Name], def)
		}
	}
	reported := map[string]map[parser.ScopeID]bool{}
	for i := range p.tree.Definitions {
		def := &p.tree.Definitions[i]
		writes := topLevel[def.Name]
		if len(writes) == 0 {
			continue
		}
		frozen := slices.IndexFunc(writes, func(w *parser.Definition) bool {
			return w.Kind == parser.DefDeclare && w.Attrs.Has(parser.AttrReadonly)
		})
		if def.Within == parser.FileScope {
			if frozen < 0 || writes[frozen].Span.Start <= def.Span.Start {
				p.checkTopLevelReassignment(def, writes)
			}
			continue
		}
		if frozen >= 0 || def.Local || !writesVariable(def.Kind) && !overwritesGlobal(def) || reported[def.Name][def.Within] || p.localBefore(*def) {
			continue
		}
		addScope(reported, def.Name, def.Within)
		fn, _ := p.tree.EnclosingFunction(def.Within)
		p.report(codeReassignedGlobal, def.NameSpan, fn.Name+" changes the global "+def.Name+", which the top level sets on line "+strconv.Itoa(p.tree.Line(writes[0].Span.Start))+"; pass values in as arguments and out on standard output, or keep the global constant.")
	}
}

func (p *pass) checkTopLevelReassignment(def *parser.Definition, writes []*parser.Definition) {
	if !overwritesGlobal(def) || updatesItself(def) {
		return
	}
	for _, earlier := range writes {
		if earlier == def {
			return
		}
		if overwritesGlobal(earlier) && !p.exclusiveBranches(earlier.Node, def.Node) {
			p.report(codeReassignedGlobal, def.NameSpan, def.Name+" is set again after line "+strconv.Itoa(p.tree.Line(earlier.Span.Start))+"; a global that keeps one value can be made readonly, so give the new value its own name.")
			return
		}
	}
}
