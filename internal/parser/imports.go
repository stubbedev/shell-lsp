package parser

type Import struct {
	Name string
	Tree *Tree
}

func ImportedFunction(imports []Import, name string) (Import, Function, bool) {
	for _, imp := range imports {
		if fn, ok := imp.Tree.FunctionNamed(name); ok {
			return imp, fn, true
		}
	}
	return Import{}, Function{}, false
}

func ImportedGlobal(imports []Import, name string) (Import, Definitions, bool) {
	for _, imp := range imports {
		if defs := imp.Tree.Visible(name, FileScope); defs.Len() > 0 {
			return imp, defs, true
		}
	}
	return Import{}, Definitions{}, false
}
