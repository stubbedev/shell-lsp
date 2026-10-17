package justfile

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

var unixSystems = []string{"android", "dragonfly", "freebsd", "linux", "macos", "netbsd", "openbsd"}

type checker struct {
	f        *File
	findings []lint.Finding
	lists    bool
	unstable bool
}

func (f *File) check() []lint.Finding {
	c := &checker{f: f, lists: f.Enabled("lists"), unstable: f.Enabled("unstable")}
	for _, e := range f.Errors {
		c.report(lint.CodeJustSyntax, e.Span, "%s", e.Message)
	}
	c.duplicates()
	if c.lists && !c.unstable {
		s, _ := f.setting("lists")
		c.report(lint.CodeJustNeedsSetting, s.Name.Span, "`set lists` is unstable and needs `set unstable`")
	}
	for _, it := range f.Items {
		switch it.Kind {
		case ItemSetting:
			c.settingValue(it.Setting)
		case ItemRecipe:
			c.recipe(it.Recipe)
		case ItemFunction:
			if !c.unstable {
				c.report(lint.CodeJustNeedsSetting, it.Function.Name.Span, "user-defined functions are unstable and need `set unstable`")
			}
		case ItemAssignment, ItemUnexport, ItemAlias, ItemImport, ItemModule:
		}
		c.features(it)
	}
	return c.findings
}

func (c *checker) report(code lint.Code, span Span, format string, args ...any) {
	c.findings = append(c.findings, lint.HostFinding(code, span, fmt.Sprintf(format, args...)))
}

func (c *checker) line(offset int) int { return strings.Count(c.f.Text[:offset], "\n") + 1 }

func systems(attrs []Attribute) []string {
	var out []string
	for _, a := range attrs {
		switch {
		case a.Name.Text == "unix":
			out = append(out, unixSystems...)
		case a.Name.Text == "windows" || slices.Contains(unixSystems, a.Name.Text):
			out = append(out, a.Name.Text)
		}
	}
	return out
}

func overlap(a, b []string) bool {
	return len(a) == 0 || len(b) == 0 || slices.ContainsFunc(a, func(s string) bool { return slices.Contains(b, s) })
}

type definition struct {
	kind    string
	name    Name
	systems []string
}

func (c *checker) duplicates() {
	allowRecipes, allowVariables := c.f.Enabled("allow-duplicate-recipes"), c.f.Enabled("allow-duplicate-variables")
	var recipes, aliases, variables, settings []definition
	for _, it := range c.f.Items {
		switch it.Kind {
		case ItemRecipe:
			d := definition{"recipe", it.Recipe.Name, systems(it.Recipe.Attributes)}
			if prior, ok := clash(aliases, d); ok {
				c.report(lint.CodeJustDuplicateAlias, d.name.Span, "alias `%s` defined on line %d is redefined as a recipe on line %d", d.name.Text, c.line(prior.name.Span.Start), c.line(d.name.Span.Start))
			}
			if prior, ok := clash(recipes, d); ok && !allowRecipes {
				c.report(lint.CodeJustDuplicateRecipe, d.name.Span, "recipe `%s` first defined on line %d is redefined on line %d", d.name.Text, c.line(prior.name.Span.Start), c.line(d.name.Span.Start))
			}
			recipes = append(recipes, d)
		case ItemAlias:
			d := definition{"alias", it.Alias.Name, systems(it.Alias.Attributes)}
			if prior, ok := clash(aliases, d); ok {
				c.report(lint.CodeJustDuplicateAlias, d.name.Span, "alias `%s` first defined on line %d is redefined on line %d", d.name.Text, c.line(prior.name.Span.Start), c.line(d.name.Span.Start))
			}
			if prior, ok := clash(recipes, d); ok {
				c.report(lint.CodeJustDuplicateAlias, d.name.Span, "alias `%s` has the same name as the recipe on line %d", d.name.Text, c.line(prior.name.Span.Start))
			}
			aliases = append(aliases, d)
		case ItemAssignment:
			d := definition{"variable", it.Assignment.Name, systems(it.Assignment.Attributes)}
			if _, ok := clash(variables, d); ok && !allowVariables {
				c.report(lint.CodeJustDuplicateVariable, d.name.Span, "variable `%s` has multiple definitions", d.name.Text)
			}
			variables = append(variables, d)
		case ItemSetting:
			d := definition{"setting", it.Setting.Name, systems(it.Setting.Attributes)}
			if prior, ok := clash(settings, d); ok {
				c.report(lint.CodeJustDuplicateSetting, d.name.Span, "setting `%s` first set on line %d is set again on line %d", d.name.Text, c.line(prior.name.Span.Start), c.line(d.name.Span.Start))
			}
			settings = append(settings, d)
		case ItemUnexport, ItemImport, ItemModule, ItemFunction:
		}
	}
}

func clash(prior []definition, d definition) (definition, bool) {
	for _, p := range prior {
		if p.name.Text == d.name.Text && overlap(p.systems, d.systems) {
			return p, true
		}
	}
	return definition{}, false
}

func (c *checker) settingValue(s *Setting) {
	kind, known := settingTypes[s.Name.Text]
	if !known {
		return
	}
	v := s.Value
	switch kind {
	case settingBool:
		if v != nil && (v.Kind != ExprVariable || v.Name.Text != "true" && v.Name.Text != "false") {
			c.report(lint.CodeJustSettingValue, v.Span, "setting `%s` takes `true` or `false`", s.Name.Text)
		}
	case settingString:
		switch {
		case v == nil:
			c.report(lint.CodeJustSettingValue, s.Name.Span, "setting `%s` needs a value: `set %s := '...'`", s.Name.Text, s.Name.Text)
		case v.Kind == ExprList && !c.lists:
			c.report(lint.CodeJustSettingValue, v.Span, "setting `%s` takes a string, not a list", s.Name.Text)
		}
	case settingList:
		if v == nil || v.Kind != ExprList {
			span := s.Name.Span
			if v != nil {
				span = v.Span
			}
			c.report(lint.CodeJustSettingValue, span, "setting `%s` takes a list such as `['bash', '-c']`", s.Name.Text)
			return
		}
		if len(v.Args) == 0 {
			c.report(lint.CodeJustSettingValue, v.Span, "setting `%s` needs at least the command", s.Name.Text)
		}
	}
}

func (c *checker) features(it Item) {
	visit := func(e *Expr) {
		Walk(e, func(x *Expr) {
			switch {
			case c.lists:
			case x.Kind == ExprNot:
				c.report(lint.CodeJustNeedsSetting, Span{Start: x.Span.Start, End: x.Span.Start + 1}, "`!` needs `set lists`")
			case x.Kind == ExprBinary && (x.Op == "&&" || x.Op == "||" || x.Op == "++"):
				c.report(lint.CodeJustNeedsSetting, x.OpSpan, "`%s` needs `set lists`", x.Op)
			case x.Kind == ExprList && !commandSetting(it):
				c.report(lint.CodeJustNeedsSetting, x.Span, "list literals need `set lists`")
			}
		})
	}
	for _, e := range itemExprs(it) {
		visit(e)
	}
}

func itemExprs(it Item) []*Expr {
	var out []*Expr
	attrs := func(list []Attribute) {
		for _, a := range list {
			for _, arg := range a.Args {
				out = append(out, arg.Value)
			}
		}
	}
	switch it.Kind {
	case ItemSetting:
		attrs(it.Setting.Attributes)
		out = append(out, it.Setting.Value)
	case ItemAssignment:
		attrs(it.Assignment.Attributes)
		out = append(out, it.Assignment.Value)
	case ItemAlias:
		attrs(it.Alias.Attributes)
	case ItemImport:
		out = append(out, it.Import.Path)
	case ItemModule:
		attrs(it.Module.Attributes)
		out = append(out, it.Module.Path)
	case ItemFunction:
		attrs(it.Function.Attributes)
		out = append(out, it.Function.Body)
	case ItemRecipe:
		r := it.Recipe
		attrs(r.Attributes)
		for _, p := range r.Params {
			out = append(out, p.Default)
		}
		for _, d := range slices.Concat(r.Deps, r.Subsequent) {
			out = append(out, d.Args...)
		}
		for _, l := range r.Body {
			for _, frag := range l.Fragments {
				out = append(out, frag.Interp)
			}
		}
	case ItemUnexport:
	}
	return out
}

func (c *checker) recipe(r *Recipe) {
	c.parameters(r)
	for _, d := range slices.Concat(r.Deps, r.Subsequent) {
		if !d.Star {
			continue
		}
		if !c.lists {
			c.report(lint.CodeJustNeedsSetting, Span{Start: d.Span.Start, End: d.Span.Start + 1}, "mapped dependencies need `set lists`")
		}
		if !slices.ContainsFunc(d.Args, func(a *Expr) bool { return a.Starred }) {
			c.report(lint.CodeJustDependencyArguments, d.Span, "a mapped dependency needs a starred argument such as `*items`")
		}
	}
	c.indentation(r)
}

func (c *checker) parameters(r *Recipe) {
	seen := map[string]bool{}
	defaulted := false
	for i, p := range r.Params {
		if seen[p.Name.Text] {
			c.report(lint.CodeJustParameterOrder, p.Name.Span, "recipe `%s` has duplicate parameter `%s`", r.Name.Text, p.Name.Text)
		}
		seen[p.Name.Text] = true
		if i > 0 && r.Params[i-1].Variadic != Singular {
			c.report(lint.CodeJustParameterOrder, p.Name.Span, "parameter `%s` follows variadic parameter", p.Name.Text)
		}
		switch {
		case p.Default != nil:
			defaulted = true
		case defaulted && p.Variadic == Singular:
			c.report(lint.CodeJustParameterOrder, p.Name.Span, "non-default parameter `%s` follows default parameter", p.Name.Text)
		}
	}
}

func (c *checker) indentation(r *Recipe) {
	if len(r.Body) == 0 {
		return
	}
	_, script := attributeNamed(r.Attributes, "script")
	linewise := !r.Shebang && !script && !c.f.Enabled("default-script")
	continued := false
	for _, l := range r.Body {
		indent := c.f.Text[l.Indent.Start:l.Indent.End]
		if len(l.Fragments) == 0 {
			continue
		}
		switch {
		case !strings.HasPrefix(indent, r.Indent):
			c.report(lint.CodeJustIndentation, l.Indent, "recipe line has inconsistent leading whitespace: it started with %s but this line has %s", whitespace(r.Indent), whitespace(indent))
		case linewise && !continued && len(indent) > len(r.Indent):
			c.report(lint.CodeJustIndentation, Span{Start: l.Indent.Start, End: l.Span.End}, "recipe line has extra leading whitespace; join it to the line above with a trailing `\\` or start the recipe with a shebang")
		}
		continued = l.Continued
	}
}

func whitespace(s string) string {
	var sb strings.Builder
	sb.WriteByte('`')
	for _, c := range s {
		switch c {
		case ' ':
			sb.WriteString("␠")
		case '\t':
			sb.WriteString("␉")
		}
	}
	sb.WriteByte('`')
	return sb.String()
}

func commandSetting(it Item) bool {
	return it.Kind == ItemSetting && slices.Contains([]string{"shell", "windows-shell", "script-interpreter"}, it.Setting.Name.Text)
}
