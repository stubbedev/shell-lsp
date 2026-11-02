package syntax

import "strings"

func render(e Expr) string {
	switch e := e.(type) {
	case *Ident:
		return e.Name
	case *Int:
		if digits := strings.TrimLeft(e.Text, "0"); digits != "" {
			return digits
		}
		return "0"
	case *URI:
		return `"` + e.Text + `"`
	case *String:
		return `"` + literalText(e.Parts) + `"`
	case *Path:
		return `(__findFile __nixPath "` + literalText(e.Parts) + `")`
	case *Paren:
		return render(e.Inner)
	case *List:
		return renderList(e)
	case *AttrSet:
		return renderSet(e)
	case *Let:
		return "(let " + renderBindings(e.Bindings, " ") + "in " + render(e.Body) + ")"
	case *LegacyLet:
		return "(rec " + strings.TrimPrefix(renderSet(e.Set), "rec ") + ").body"
	case *With:
		return "(with " + render(e.Scope) + "; " + render(e.Body) + ")"
	case *Assert:
		return "assert " + render(e.Cond) + "; " + render(e.Body)
	case *If:
		return "(if " + render(e.Cond) + " then " + render(e.Then) + " else " + render(e.Else) + ")"
	case *Lambda:
		return "(" + renderParam(e.Param) + ": " + render(e.Body) + ")"
	case *Apply:
		return "(" + strings.Join(callParts(e), " ") + ")"
	case *Select:
		out := "(" + render(e.Target) + ")." + renderPath(e.Path)
		if e.Default != nil {
			out += " or (" + render(e.Default) + ")"
		}
		return out
	case *HasAttr:
		return "((" + render(e.Target) + ") ? " + renderPath(e.Path) + ")"
	case *Binary:
		return renderBinary(e)
	case *Unary:
		if e.Op == OpNot {
			return "(! " + render(e.Operand) + ")"
		}
		return "(__sub 0 " + render(e.Operand) + ")"
	}
	return "<unrenderable>"
}

func literalText(parts []Part) string {
	var sb strings.Builder
	for _, p := range parts {
		if l, ok := p.(*Literal); ok {
			sb.WriteString(l.Text)
		}
	}
	return sb.String()
}

func renderList(e *List) string {
	var sb strings.Builder
	sb.WriteString("[ ")
	for _, item := range e.Items {
		sb.WriteString("(" + render(item) + ") ")
	}
	sb.WriteString("]")
	return sb.String()
}

func renderSet(e *AttrSet) string {
	prefix := ""
	if e.Rec {
		prefix = "rec "
	}
	return prefix + "{ " + renderBindings(e.Bindings, " ") + "}"
}

func renderBindings(bindings []Binding, after string) string {
	var sb strings.Builder
	for _, b := range bindings {
		switch b := b.(type) {
		case *Assign:
			sb.WriteString(renderAssign(b.Path, b.Value) + after)
		case *Inherit:
			if len(b.Attrs) == 0 {
				continue
			}
			sb.WriteString("inherit ")
			if b.From != nil {
				sb.WriteString("(" + render(b.From) + ") ")
			}
			names := make([]string, len(b.Attrs))
			for i, a := range b.Attrs {
				names[i] = attrName(a)
			}
			sb.WriteString(strings.Join(names, " ") + ";" + after)
		}
	}
	return sb.String()
}

func renderAssign(path AttrPath, value Expr) string {
	if len(path) == 1 {
		return attrName(path[0]) + " = " + render(value) + ";"
	}
	return attrName(path[0]) + " = { " + renderAssign(path[1:], value) + " };"
}

func attrName(a Attr) string {
	if d, ok := a.(*DynamicAttr); ok {
		return `"${` + render(d.Value) + `}"`
	}
	name, _, _ := StaticName(a)
	return name
}

func renderPath(path AttrPath) string {
	names := make([]string, len(path))
	for i, a := range path {
		names[i] = attrName(a)
	}
	return strings.Join(names, ".")
}

func renderParam(p Param) string {
	switch p := p.(type) {
	case *IdentParam:
		return p.Name.Name
	case *PatternParam:
		items := make([]string, 0, len(p.Formals)+1)
		for _, f := range p.Formals {
			item := f.Name.Name
			if f.Default != nil {
				item += " ? " + render(f.Default)
			}
			items = append(items, item)
		}
		if p.Ellipsis {
			items = append(items, "...")
		}
		out := "{ " + strings.Join(items, ", ") + " }"
		if p.Alias != nil {
			out += " @ " + p.Alias.Name
		}
		return out
	}
	return "<unrenderable>"
}

func callParts(e Expr) []string {
	switch e := e.(type) {
	case *Paren:
		return callParts(e.Inner)
	case *Apply:
		parts := callParts(e.Fn)
		for _, a := range e.Args {
			parts = append(parts, render(a))
		}
		return parts
	case *Binary:
		switch e.Op {
		case OpPipeInto:
			return append(callParts(e.Right), render(e.Left))
		case OpPipeFrom:
			return append(callParts(e.Left), render(e.Right))
		}
	}
	return []string{render(e)}
}

func renderBinary(e *Binary) string {
	l, r := render(e.Left), render(e.Right)
	switch e.Op {
	case OpPipeInto, OpPipeFrom:
		return "(" + strings.Join(callParts(e), " ") + ")"
	case OpSub:
		return "(__sub " + l + " " + r + ")"
	case OpMul:
		return "(__mul " + l + " " + r + ")"
	case OpDiv:
		return "(__div " + l + " " + r + ")"
	case OpLt:
		return "(__lessThan " + l + " " + r + ")"
	case OpGt:
		return "(__lessThan " + r + " " + l + ")"
	case OpLe:
		return "(! (__lessThan " + r + " " + l + "))"
	case OpGe:
		return "(! (__lessThan " + l + " " + r + "))"
	}
	return "(" + l + " " + e.Op.String() + " " + r + ")"
}
