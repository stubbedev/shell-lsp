package syntax

func Inspect(n Node, visit func(Node) bool) {
	if !visit(n) {
		return
	}
	each := func(children ...Node) {
		for _, c := range children {
			Inspect(c, visit)
		}
	}
	switch n := n.(type) {
	case *String:
		eachOf(n.Parts, each)
	case *Interpolation:
		each(n.Value)
	case *Path:
		eachOf(n.Parts, each)
	case *Paren:
		each(n.Inner)
	case *List:
		eachOf(n.Items, each)
	case *AttrSet:
		eachOf(n.Bindings, each)
	case *Let:
		eachOf(n.Bindings, each)
		each(n.Body)
	case *LegacyLet:
		each(n.Set)
	case *With:
		each(n.Scope, n.Body)
	case *Assert:
		each(n.Cond, n.Body)
	case *If:
		each(n.Cond, n.Then, n.Else)
	case *Lambda:
		each(n.Param, n.Body)
	case *Apply:
		each(n.Fn)
		eachOf(n.Args, each)
	case *Select:
		each(n.Target)
		eachOf(n.Path, each)
		if n.Default != nil {
			each(n.Default)
		}
	case *HasAttr:
		each(n.Target)
		eachOf(n.Path, each)
	case *Binary:
		each(n.Left, n.Right)
	case *Unary:
		each(n.Operand)
	case *StringAttr:
		each(n.Value)
	case *DynamicAttr:
		each(n.Value)
	case *Assign:
		eachOf(n.Path, each)
		each(n.Value)
	case *Inherit:
		if n.From != nil {
			each(n.From)
		}
		eachOf(n.Attrs, each)
	case *IdentParam:
		each(n.Name)
	case *PatternParam:
		if n.Alias != nil && n.AliasFirst {
			each(n.Alias)
		}
		eachOf(n.Formals, each)
		if n.Alias != nil && !n.AliasFirst {
			each(n.Alias)
		}
	case *Formal:
		each(n.Name)
		if n.Default != nil {
			each(n.Default)
		}
	}
}

func eachOf[T Node](nodes []T, each func(...Node)) {
	for _, n := range nodes {
		each(n)
	}
}
