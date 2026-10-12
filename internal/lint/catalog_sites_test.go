package lint

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"strings"
	"testing"
)

func TestEveryCodeInTheSourceIsInTheCatalog(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	conf := types.Config{
		Importer: importerFunc(func(p string) (*types.Package, error) { return types.NewPackage(p, path.Base(p)), nil }),
		Error:    func(error) {},
	}
	pkg, _ := conf.Check("lint", fset, files, info)
	codeType := pkg.Scope().Lookup("Code").Type()
	codeValue := func(expr ast.Expr) (constant.Value, bool) {
		tv, ok := info.Types[expr]
		return tv.Value, ok && tv.Value != nil && types.Identical(tv.Type, codeType)
	}
	sites := 0
	var visit func(ast.Node) bool
	visit = func(n ast.Node) bool {
		if expr, ok := n.(ast.Expr); ok {
			if value, ok := codeValue(expr); ok {
				number, exact := constant.Uint64Val(value)
				switch {
				case !exact:
					t.Errorf("%s: code %s is not a whole number", fset.Position(expr.Pos()), value)
				case number == 0:
				default:
					sites++
					if _, ok := catalog[Code(number)]; !ok {
						t.Errorf("%s: %s is used but missing from the catalog", fset.Position(expr.Pos()), Code(number))
					}
				}
				return false
			}
		}
		switch n := n.(type) {
		case *ast.GenDecl:
			return n.Tok != token.CONST
		case *ast.CallExpr:
			return !info.Types[n.Fun].IsType()
		case *ast.CaseClause:
			for _, stmt := range n.Body {
				ast.Inspect(stmt, visit)
			}
			return false
		case *ast.BinaryExpr:
			for _, operand := range []ast.Expr{n.X, n.Y} {
				if _, constant := codeValue(operand); !constant {
					ast.Inspect(operand, visit)
				}
			}
			return false
		}
		return true
	}
	for _, f := range files {
		ast.Inspect(f, visit)
	}
	if sites < len(catalog) {
		t.Errorf("found %d code sites, fewer than the %d catalog entries; the scan no longer sees the source", sites, len(catalog))
	}
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }
