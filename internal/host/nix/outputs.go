package nix

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

const (
	perSystemAttr  = "perSystem"
	flakeAttr      = "flake"
	mkFlakeName    = "mkFlake"
	checksCategory = "checks"
	flakeRefPrefix = ".#"
)

type outputCategory struct {
	attr            string
	argv            []string
	unattended      []string
	unattendedTitle string
	bare            bool
}

var outputCategories = []outputCategory{
	{attr: "packages", argv: []string{"nix", "build"}},
	{attr: "apps", argv: []string{"nix", "run"}},
	{attr: "devShells", argv: []string{"nix", "develop"}, unattended: []string{"--command", "true"}, unattendedTitle: "Build dev shell"},
	{attr: checksCategory, argv: []string{"nix", "flake", "check"}},
	{attr: inputsAttr, argv: []string{"nix", "flake", "update"}, bare: true},
}

var (
	systemEaches   = []string{"eachDefaultSystem", "eachSystem"}
	systemSuffixes = []string{"-linux", "-darwin", "-freebsd", "-netbsd", "-openbsd", "-windows", "-cygwin"}
)

const (
	inputRunPrefix = inputsAttr + "."
)

func InputRun(input string) string { return inputRunPrefix + input }

func (d *Document) FlakeInputAt(offset int) (string, bool) {
	var found string
	eachInputLeaf(d.tree.Root, func(path syntax.AttrPath, _ syntax.Expr) {
		if name, static := syntax.AttrName(path[0]); static && path[0].Span().Touches(offset) {
			found = name
		}
	})
	return found, found != ""
}

func (d *Document) DeclaresInput(input string) bool {
	declared, _ := flakeInputsOf(d.tree.Root)
	return slices.ContainsFunc(declared, func(in flakeInput) bool { return in.name == input })
}

func categoryOf(name string) (outputCategory, string, bool) {
	attr, output, hasOutput := strings.Cut(name, ".")
	i := slices.IndexFunc(outputCategories, func(c outputCategory) bool { return c.attr == attr })
	if i < 0 {
		panic("nix: " + name + " is not a flake output this file offers")
	}
	return outputCategories[i], output, hasOutput
}

func TerminalInvocation(name string) []string {
	category, output, hasOutput := categoryOf(name)
	argv := slices.Clone(category.argv)
	switch {
	case hasOutput && category.bare:
		argv = append(argv, output)
	case hasOutput:
		argv = append(argv, flakeRefPrefix+output)
	}
	return argv
}

func Invocation(name string) []string {
	category, _, _ := categoryOf(name)
	return append(TerminalInvocation(name), category.unattended...)
}

func NeedsTerminal(name string) bool {
	category, _, _ := categoryOf(name)
	return category.unattended != nil
}

func UnattendedTitle(name string) string {
	category, output, hasOutput := categoryOf(name)
	if category.unattendedTitle == "" {
		return strings.Join(Invocation(name), " ")
	}
	if !hasOutput {
		return category.unattendedTitle
	}
	return category.unattendedTitle + " " + flakeRefPrefix + output
}

type outputCollector struct {
	table *scope.Table
	out   []model.Runnable
	seen  map[string]bool
}

func outputs(tree *syntax.File, table *scope.Table) []model.Runnable {
	c := &outputCollector{table: table, seen: map[string]bool{}}
	if lambda := flakeOutputs(tree.Root); lambda != nil {
		c.flake(lambda.Body)
		return c.out
	}
	if set, ok := reduced(tree.Root).(*syntax.AttrSet); ok && hasHead(set, perSystemAttr) {
		c.module(set)
	}
	return c.out
}

func reduced(e syntax.Expr) syntax.Expr {
	for {
		switch n := e.(type) {
		case *syntax.Paren:
			e = n.Inner
		case *syntax.Let:
			e = n.Body
		case *syntax.With:
			e = n.Body
		case *syntax.Assert:
			e = n.Body
		case *syntax.Lambda:
			e = n.Body
		default:
			return e
		}
	}
}

func hasHead(set *syntax.AttrSet, name string) bool {
	return slices.ContainsFunc(set.Bindings, func(b syntax.Binding) bool {
		a, ok := b.(*syntax.Assign)
		if !ok {
			return false
		}
		head, static := syntax.AttrName(a.Path[0])
		return static && head == name
	})
}

func declaresOutputs(root syntax.Expr) bool {
	set, ok := syntax.Unparenthesized(root).(*syntax.AttrSet)
	return ok && hasHead(set, outputsAttr)
}

func (d *Document) IsFlake() bool { return declaresOutputs(d.tree.Root) }

func (c *outputCollector) flake(e syntax.Expr) {
	switch e := reduced(e).(type) {
	case *syntax.AttrSet:
		c.outputSet(e, true)
	case *syntax.Binary:
		if e.Op == syntax.OpUpdate {
			c.flake(e.Left)
			c.flake(e.Right)
		}
	case *syntax.Apply:
		last := e.Args[len(e.Args)-1]
		name := callName(c.table, e.Fn)
		switch {
		case name == mkFlakeName:
			if set, ok := reduced(last).(*syntax.AttrSet); ok {
				c.module(set)
			}
		case slices.Contains(systemEaches, name):
			if set, ok := reduced(last).(*syntax.AttrSet); ok {
				c.outputSet(set, false)
			}
		}
	}
}

func (c *outputCollector) module(set *syntax.AttrSet) {
	for _, b := range set.Bindings {
		a, ok := b.(*syntax.Assign)
		if !ok {
			continue
		}
		head, static := syntax.AttrName(a.Path[0])
		if !static {
			continue
		}
		switch head {
		case perSystemAttr:
			c.nested(a, false)
		case flakeAttr:
			c.nested(a, true)
		}
	}
}

func (c *outputCollector) nested(a *syntax.Assign, layered bool) {
	if len(a.Path) > 1 {
		c.category(a.Path[1:], a.Value, layered)
		return
	}
	if inner, ok := reduced(a.Value).(*syntax.AttrSet); ok {
		c.outputSet(inner, layered)
	}
}

func (c *outputCollector) outputSet(set *syntax.AttrSet, layered bool) {
	for _, b := range set.Bindings {
		if a, ok := b.(*syntax.Assign); ok {
			c.category(a.Path, a.Value, layered)
		}
	}
}

func (c *outputCollector) category(path syntax.AttrPath, value syntax.Expr, layered bool) {
	name, static := syntax.AttrName(path[0])
	if !static {
		return
	}
	i := slices.IndexFunc(outputCategories, func(o outputCategory) bool { return o.attr == name })
	if i < 0 {
		return
	}
	category := outputCategories[i].attr
	if category == checksCategory {
		c.add(category, nameSpan(path[0]))
		return
	}
	c.entries(category, path[1:], value, layered)
}

func (c *outputCollector) entries(category string, path syntax.AttrPath, value syntax.Expr, layered bool) {
	if len(path) > 0 {
		switch {
		case !layered:
			c.output(category, path[0])
		case systemAttr(path[0]):
			c.entries(category, path[1:], value, false)
		}
		return
	}
	switch v := reduced(value).(type) {
	case *syntax.AttrSet:
		for _, b := range v.Bindings {
			switch b := b.(type) {
			case *syntax.Assign:
				c.entries(category, b.Path, b.Value, layered)
			case *syntax.Inherit:
				if !layered {
					for _, attr := range b.Attrs {
						c.output(category, attr)
					}
				}
			}
		}
	case *syntax.Binary:
		if v.Op == syntax.OpUpdate {
			c.entries(category, nil, v.Left, layered)
			c.entries(category, nil, v.Right, layered)
		}
	case *syntax.Apply:
		if !layered {
			return
		}
		last := v.Args[len(v.Args)-1]
		if _, isLambda := syntax.Unparenthesized(last).(*syntax.Lambda); isLambda {
			c.entries(category, nil, last, false)
		}
	}
}

func systemAttr(a syntax.Attr) bool {
	name, static := syntax.AttrName(a)
	if !static {
		_, dynamic := a.(*syntax.DynamicAttr)
		return dynamic
	}
	return slices.ContainsFunc(systemSuffixes, func(suffix string) bool { return strings.HasSuffix(name, suffix) })
}

func (c *outputCollector) output(category string, attr syntax.Attr) {
	name, span, static := syntax.StaticName(attr)
	if static && ValidName(model.KindAttribute, name) {
		c.add(category+"."+name, span)
	}
}

func (c *outputCollector) add(name string, span model.Span) {
	if c.seen[name] {
		return
	}
	c.seen[name] = true
	c.out = append(c.out, model.Runnable{Name: name, Span: span})
}
