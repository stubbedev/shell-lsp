package syntax

func Inspect(n Node, visit func(Node) bool) {
	if !visit(n) {
		return
	}
	switch n := n.(type) {
	case *String:
		inspectAll(n.Parts, visit)
	case *Interpolation:
		Inspect(n.Value, visit)
	case *Path:
		inspectAll(n.Parts, visit)
	case *Paren:
		Inspect(n.Inner, visit)
	case *List:
		inspectAll(n.Items, visit)
	case *AttrSet:
		inspectAll(n.Bindings, visit)
	case *Let:
		inspectAll(n.Bindings, visit)
		Inspect(n.Body, visit)
	case *LegacyLet:
		Inspect(n.Set, visit)
	case *With:
		Inspect(n.Scope, visit)
		Inspect(n.Body, visit)
	case *Assert:
		Inspect(n.Cond, visit)
		Inspect(n.Body, visit)
	case *If:
		Inspect(n.Cond, visit)
		Inspect(n.Then, visit)
		Inspect(n.Else, visit)
	case *Lambda:
		Inspect(n.Param, visit)
		Inspect(n.Body, visit)
	case *Apply:
		Inspect(n.Fn, visit)
		inspectAll(n.Args, visit)
	case *Select:
		Inspect(n.Target, visit)
		inspectAll(n.Path, visit)
		if n.Default != nil {
			Inspect(n.Default, visit)
		}
	case *HasAttr:
		Inspect(n.Target, visit)
		inspectAll(n.Path, visit)
	case *Binary:
		Inspect(n.Left, visit)
		Inspect(n.Right, visit)
	case *Unary:
		Inspect(n.Operand, visit)
	case *StringAttr:
		Inspect(n.Value, visit)
	case *DynamicAttr:
		Inspect(n.Value, visit)
	case *Assign:
		inspectAll(n.Path, visit)
		Inspect(n.Value, visit)
	case *Inherit:
		if n.From != nil {
			Inspect(n.From, visit)
		}
		inspectAll(n.Attrs, visit)
	case *IdentParam:
		Inspect(n.Name, visit)
	case *PatternParam:
		if n.Alias != nil && n.AliasFirst {
			Inspect(n.Alias, visit)
		}
		inspectAll(n.Formals, visit)
		if n.Alias != nil && !n.AliasFirst {
			Inspect(n.Alias, visit)
		}
	case *Formal:
		Inspect(n.Name, visit)
		if n.Default != nil {
			Inspect(n.Default, visit)
		}
	}
}

func inspectAll[T Node](nodes []T, visit func(Node) bool) {
	for _, n := range nodes {
		Inspect(n, visit)
	}
}
