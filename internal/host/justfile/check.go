package justfile

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

type checker struct {
	f              *File
	findings       []lint.Finding
	lists          bool
	unstable       bool
	constVariables map[string]bool
}

func (f *File) check() []lint.Finding {
	c := &checker{f: f, lists: f.Enabled("lists"), unstable: f.Enabled("unstable"), constVariables: map[string]bool{}}
	for _, e := range f.Errors {
		c.report(e.Code, e.Span, "%s", e.Message)
	}
	c.duplicates()
	for _, stray := range f.Stray {
		args := c.attributeSet(stray.Attributes)
		if stray.Recipe != nil {
			c.argAttributes(*stray.Recipe, args)
		}
		if stray.Function != nil {
			c.placement(ItemFunction, "function", *stray.Function, stray.Attributes)
		}
	}
	if c.lists && !c.unstable {
		s, _ := f.setting("lists")
		c.report(lint.CodeJustNeedsSetting, s.Name.Span, "`set lists` is unstable and needs `set unstable`")
	}
	for _, it := range f.Items {
		args := c.attributes(it)
		switch it.Kind {
		case ItemSetting:
			c.settingValue(it.Setting)
		case ItemRecipe:
			c.recipe(it.Recipe, args)
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

func EnabledOn(attrs []Attribute, system string) bool {
	s := systems(attrs)
	return len(s) == 0 || slices.Contains(s, system)
}

func (it Item) EnabledOn(system string) bool {
	return EnabledOn(attributesOf(it), system)
}

func systems(attrs []Attribute) []string {
	var out []string
	for _, a := range attrs {
		switch {
		case a.Name.Text == "unix":
			out = append(out, unixSystems...)
		case enabler(a.Name.Text):
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
	var recipes, aliases, variables, settings, functions []definition
	for _, it := range c.f.Items {
		switch it.Kind {
		case ItemRecipe:
			d := definition{"recipe", it.Recipe.Name, systems(it.Recipe.Attributes)}
			if prior, ok := clash(aliases, d); ok {
				c.report(lint.CodeJustDuplicateAlias, d.name.Span, "alias `%s` defined on line %d is redefined as a recipe on line %d", d.name.Text, c.f.line(prior.name.Span.Start), c.f.line(d.name.Span.Start))
			}
			if prior, ok := clash(recipes, d); ok && !allowRecipes {
				c.report(lint.CodeJustDuplicateRecipe, d.name.Span, "recipe `%s` first defined on line %d is redefined on line %d", d.name.Text, c.f.line(prior.name.Span.Start), c.f.line(d.name.Span.Start))
			}
			recipes = append(recipes, d)
		case ItemAlias:
			d := definition{"alias", it.Alias.Name, systems(it.Alias.Attributes)}
			if prior, ok := clash(aliases, d); ok {
				c.report(lint.CodeJustDuplicateAlias, d.name.Span, "alias `%s` first defined on line %d is redefined on line %d", d.name.Text, c.f.line(prior.name.Span.Start), c.f.line(d.name.Span.Start))
			}
			if prior, ok := clash(recipes, d); ok {
				c.report(lint.CodeJustDuplicateAlias, d.name.Span, "alias `%s` has the same name as the recipe on line %d", d.name.Text, c.f.line(prior.name.Span.Start))
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
				c.report(lint.CodeJustDuplicateSetting, d.name.Span, "setting `%s` first set on line %d is set again on line %d", d.name.Text, c.f.line(prior.name.Span.Start), c.f.line(d.name.Span.Start))
			}
			settings = append(settings, d)
		case ItemFunction:
			d := definition{"function", it.Function.Name, systems(it.Function.Attributes)}
			if prior, ok := clash(functions, d); ok {
				c.report(lint.CodeJustDuplicateFunction, d.name.Span, "function `%s` first defined on line %d is redefined on line %d", d.name.Text, c.f.line(prior.name.Span.Start), c.f.line(d.name.Span.Start))
			}
			functions = append(functions, d)
		case ItemUnexport, ItemImport, ItemModule:
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

func (c *checker) indentationSetting(v *Expr) {
	values, ok := c.f.Constant(v)
	if !ok || len(values) != 1 {
		return
	}
	switch value := values[0]; {
	case value == "":
		c.report(lint.CodeJustSettingValue, v.Span, "the indentation must not be empty")
	case strings.Trim(value, " \t") != "":
		c.report(lint.CodeJustSettingValue, v.Span, "the indentation must be spaces or tabs")
	case strings.Contains(value, " ") && strings.Contains(value, "\t"):
		c.report(lint.CodeJustSettingValue, v.Span, "the indentation must be all spaces or all tabs")
	}
}

func (c *checker) settingValue(s *Setting) {
	kind, known := settingTypes[s.Name.Text]
	if !known {
		return
	}
	if slices.Contains(constSettings, s.Name.Text) {
		c.constant(lint.CodeJustSettingValue, s.Value)
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
		case s.Name.Text == "indentation":
			c.indentationSetting(v)
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

func condition(e *Expr) (*Expr, bool) {
	switch {
	case e.Kind == ExprIf, e.Kind == ExprCall && e.Name.Text == "assert" && len(e.Args) > 0:
		return e.Args[0], true
	}
	return nil, false
}

func isComparison(e *Expr) bool { return e.Kind == ExprBinary && operators[e.Op] == comparison }

func (c *checker) features(it Item) {
	visit := func(e *Expr) {
		conditions := map[*Expr]bool{}
		Walk(e, func(x *Expr) {
			if cond, ok := condition(x); ok {
				conditions[cond] = true
			}
		})
		Walk(e, func(x *Expr) {
			cond, conditional := condition(x)
			switch {
			case c.lists:
			case conditional && !isComparison(cond):
				c.report(lint.CodeJustNeedsSetting, cond.Span, "an `if` or `assert` condition other than a comparison needs `set lists`")
			case isComparison(x) && !conditions[x]:
				c.report(lint.CodeJustNeedsSetting, x.OpSpan, "`%s` outside the condition of an `if` needs `set lists`", x.Op)
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
		attrs(it.Unexport.Attributes)
	}
	return out
}

func (c *checker) script(r *Recipe) bool {
	_, shell := attributeNamed(r.Attributes, "shell")
	_, script := attributeNamed(r.Attributes, "script")
	return !shell && (r.Shebang || script || c.f.Enabled("default-script"))
}

func (c *checker) scriptAttributes(r *Recipe) {
	if c.script(r) {
		return
	}
	for _, name := range scriptAttributes {
		if _, ok := attributeNamed(r.Attributes, name); ok {
			c.report(lint.CodeJustAttributeArguments, r.Name.Span, "shell recipe `%s` has script recipe attribute `%s`", r.Name.Text, name)
		}
	}
}

func (c *checker) recipe(r *Recipe, args []argAttribute) {
	c.parameters(r)
	c.constAttributes(r)
	c.scriptAttributes(r)
	c.argAttributes(RecipeHeader{Name: r.Name, Params: r.Params, Colon: true}, args)
	c.listArguments(args)
	for _, d := range slices.Concat(r.Deps, r.Subsequent) {
		if !d.Star {
			continue
		}
		if !c.lists {
			c.report(lint.CodeJustNeedsSetting, Span{Start: d.Span.Start, End: d.Span.Start + 1}, "mapped dependencies need `set lists`")
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
	linewise := !c.script(r)
	continued := false
	for _, l := range r.Body {
		indent := c.f.Text[l.Indent.Start:l.Indent.End]
		if len(l.Fragments) == 0 {
			continued = false
			continue
		}
		if linewise && !continued && len(indent) > len(r.Indent) && strings.HasPrefix(indent, r.Indent) {
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

func (c *checker) itemToken(it Item) (string, Name) {
	switch it.Kind {
	case ItemSetting:
		return "setting", it.Setting.Name
	case ItemAssignment:
		return "assignment", it.Assignment.Name
	case ItemUnexport:
		return "unexport", it.Unexport.Name
	case ItemAlias:
		return "alias", it.Alias.Name
	case ItemImport:
		return "import", Name{Text: c.f.Text[it.Import.Path.Span.Start:it.Import.Path.Span.End], Span: it.Import.Path.Span}
	case ItemModule:
		return "module", it.Module.Name
	case ItemFunction:
		return "function", it.Function.Name
	case ItemRecipe:
		return "recipe", it.Recipe.Name
	}
	panic(fmt.Sprintf("justfile: item kind %d has no name", it.Kind))
}

func allowedOn(kind ItemKind, attribute string) bool {
	return kind == ItemRecipe || enabler(attribute) || slices.Contains(itemAttributes[kind], attribute)
}

func knownAttribute(a Attribute) bool {
	_, known := Builtins.Lookup(model.KindAttribute, a.Name.Text)
	return known && !a.Partial
}

func (c *checker) attributes(it Item) []argAttribute {
	attrs := attributesOf(it)
	args := c.attributeSet(attrs)
	label, name := c.itemToken(it)
	c.placement(it.Kind, label, name, attrs)
	return args
}

func (c *checker) placement(kind ItemKind, label string, name Name, attrs []Attribute) {
	for _, a := range attrs {
		if knownAttribute(a) && !allowedOn(kind, a.Name.Text) {
			c.report(lint.CodeJustAttributeArguments, name.Span, "%s `%s` has invalid attribute `%s`", label, name.Text, a.Name.Text)
		}
	}
}

func (c *checker) attributeSet(attrs []Attribute) []argAttribute {
	var args []argAttribute
	firsts := map[string]Attribute{}
	groups := map[string]Attribute{}
	for _, a := range attrs {
		if !knownAttribute(a) {
			continue
		}
		if arg, ok := c.attribute(a); ok {
			args = c.distinctArgument(args, arg)
		}
		c.repeated(firsts, a.Name.Text, a, "attribute `"+a.Name.Text+"`", repeatableAttributes)
		if group, ok := cooked(a.positionalArg().Value); ok && a.Name.Text == "group" {
			c.repeated(groups, group, a, "group `"+group+"`", nil)
		}
	}
	return args
}

func (c *checker) attribute(a Attribute) (argAttribute, bool) {
	before := len(c.findings)
	c.attributeArguments(a)
	switch a.Name.Text {
	case "cache":
		if !c.unstable {
			c.report(lint.CodeJustNeedsSetting, a.Name.Span, "cached recipes are unstable and need `set unstable`")
		}
	case "continue":
		c.continueSignals(a)
	case "arg":
		arg, ok := c.argAttribute(a)
		_, broken := nonLiteralArgument(a)
		return arg, ok && !broken && len(c.findings) == before && a.positional() == 1
	}
	return argAttribute{}, false
}

func (c *checker) repeated(firsts map[string]Attribute, key string, a Attribute, label string, repeatable []string) {
	prior, ok := firsts[key]
	switch {
	case !ok:
		firsts[key] = a
	case !slices.Contains(repeatable, key):
		c.report(lint.CodeJustAttributeArguments, a.Name.Span, "%s first used on line %d is duplicated on line %d", label, c.f.line(prior.Name.Span.Start), c.f.line(a.Name.Span.Start))
	}
}

func (c *checker) distinctArgument(args []argAttribute, arg argAttribute) []argAttribute {
	i := slices.IndexFunc(args, func(prior argAttribute) bool { return prior.parameter == arg.parameter })
	if i < 0 {
		return append(args, arg)
	}
	c.report(lint.CodeJustAttributeArguments, arg.attribute.Name.Span, "attribute for argument `%s` first used on line %d is duplicated on line %d", arg.parameter, c.f.line(args[i].attribute.Name.Span.Start), c.f.line(arg.attribute.Name.Span.Start))
	return args
}

func (c *checker) attributeArguments(a Attribute) {
	keys := attributeKeys[a.Name.Text]
	seen := map[string]bool{}
	for _, arg := range a.Args {
		if arg.Key.Text == "" || seen[arg.Key.Text] {
			continue
		}
		i := keyIndex(a.Name.Text, arg.Key.Text)
		switch {
		case i < 0:
			c.report(lint.CodeJustAttributeArguments, arg.Key.Span, "unknown key `%s` for `%s` attribute", arg.Key.Text, a.Name.Text)
		case !keys[i].accepts(arg) && arg.Value == nil:
			c.report(lint.CodeJustAttributeArguments, arg.Key.Span, "attribute key `%s` requires a value", arg.Key.Text)
		case !keys[i].accepts(arg):
			c.report(lint.CodeJustAttributeArguments, arg.Key.Span, "attribute key `%s` takes no value", arg.Key.Text)
		}
		seen[arg.Key.Text] = true
	}
}

func (c *checker) continueSignals(a Attribute) {
	for _, arg := range a.Args {
		if lit, ok := stringLiteral(arg.Value); ok && arg.Key.Text == "" && !slices.Contains(continueSignals, lit.Value) {
			c.report(lint.CodeJustAttributeArguments, arg.Span, "invalid signal `%s`: expected one of `%s`", lit.Value, strings.Join(continueSignals, "`, `"))
		}
	}
}

type argOption struct {
	literal *Expr
	bare    *Name
	value   string
	known   bool
}

func (o argOption) span() Span {
	if o.bare != nil {
		return o.bare.Span
	}
	return o.literal.Span
}

type argAttribute struct {
	attribute Attribute
	name      *Expr
	parameter string
	long      *argOption
	short     *argOption
	max, min  *uint64
}

var argumentCount = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

func cooked(e *Expr) (string, bool) {
	lit, ok := stringLiteral(e)
	if !ok || lit.Prefix == 'x' && (strings.ContainsRune(lit.Value, '$') || strings.HasPrefix(lit.Value, "~")) {
		return "", false
	}
	return lit.Value, true
}

func (c *checker) argAttribute(a Attribute) (argAttribute, bool) {
	name := a.positionalArg().Value
	parameter, known := cooked(name)
	if name == nil {
		return argAttribute{}, false
	}
	arg := argAttribute{attribute: a, name: name, parameter: parameter}
	arg.long = c.argOption(a, "long", arg)
	arg.short = c.argOption(a, "short", arg)
	option := arg.long != nil || arg.short != nil
	_, flag := a.keyed("flag")
	value, valued := a.keyed("value")
	pattern, patterned := a.keyed("pattern")
	for _, key := range optionKeys {
		if k, ok := a.keyed(key); ok && !option && attributeKeys[a.Name.Text][keyIndex(a.Name.Text, key)].accepts(k) {
			c.report(lint.CodeJustAttributeArguments, k.Key.Span, "argument attribute `%s` only valid with `long` or `short`", key)
		}
	}
	if k, _ := a.keyed("flag"); flag && k.Value == nil && option {
		switch {
		case valued && value.Value != nil:
			c.report(lint.CodeJustAttributeArguments, k.Key.Span, "argument `%s` may not have both `flag` and `value` attributes", parameter)
		case patterned && pattern.Value != nil:
			c.report(lint.CodeJustAttributeArguments, k.Key.Span, "argument `%s` may not have both `flag` and `pattern` attributes", parameter)
		}
	}
	arg.max, arg.min = c.argumentCount(a, "max"), c.argumentCount(a, "min")
	if arg.min != nil && arg.max != nil && *arg.min > *arg.max {
		k, _ := a.keyed("min")
		c.report(lint.CodeJustAttributeArguments, k.Key.Span, "argument attribute `min` `%d` exceeds `max` `%d`", *arg.min, *arg.max)
	}
	if patterned && pattern.Value != nil {
		c.argumentPattern(pattern)
	}
	return arg, known
}

func (c *checker) argOption(a Attribute, key string, arg argAttribute) *argOption {
	k, ok := a.keyed(key)
	if !ok {
		return nil
	}
	if k.Value == nil {
		value, known := cooked(arg.name)
		return &argOption{literal: arg.name, bare: &k.Key, value: value, known: known}
	}
	value, known := cooked(k.Value)
	option := &argOption{literal: k.Value, value: value, known: known}
	if !known {
		return option
	}
	switch {
	case strings.Contains(value, "="):
		c.report(lint.CodeJustAttributeArguments, k.Value.Span, "option name for parameter `%s` contains an equal sign", arg.parameter)
	case strings.HasPrefix(value, "-"):
		c.report(lint.CodeJustAttributeArguments, k.Value.Span, "option name for parameter `%s` starts with a dash", arg.parameter)
	case value == "":
		c.report(lint.CodeJustAttributeArguments, k.Value.Span, "option name for parameter `%s` is empty", arg.parameter)
	case key == "short" && utf8.RuneCountInString(value) != 1:
		c.report(lint.CodeJustAttributeArguments, k.Value.Span, "short option name for parameter `%s` must be a single character", arg.parameter)
	}
	return option
}

func (c *checker) argumentCount(a Attribute, key string) *uint64 {
	k, ok := a.keyed(key)
	if !ok {
		return nil
	}
	value, known := cooked(k.Value)
	if !known {
		return nil
	}
	if !argumentCount.MatchString(value) {
		c.report(lint.CodeJustAttributeArguments, k.Value.Span, "invalid `%s` value `%s`", key, value)
		return nil
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		c.report(lint.CodeJustAttributeArguments, k.Value.Span, "invalid `%s` value `%s`: too large", key, value)
		return nil
	}
	return &n
}

func (c *checker) argumentPattern(pattern AttributeArg) {
	values, ok := c.f.Constant(pattern.Value)
	if !ok {
		return
	}
	for _, v := range values {
		if err := checkRegex(v); err != nil {
			c.report(lint.CodeJustAttributeArguments, pattern.Key.Span, "argument pattern `%s` is not a regular expression just accepts: %v", v, err)
			return
		}
	}
}

func (c *checker) listArguments(args []argAttribute) {
	if c.lists {
		return
	}
	for _, a := range args {
		for _, key := range listArgumentKeys {
			if k, ok := a.attribute.keyed(key); ok {
				c.report(lint.CodeJustNeedsSetting, k.Key.Span, "`[arg(%s)]` needs `set lists`", key)
			}
		}
	}
}

func (c *checker) argAttributes(r RecipeHeader, args []argAttribute) {
	unused := make(map[string]argAttribute, len(args))
	for _, a := range args {
		unused[a.parameter] = a
	}
	c.duplicateOptions(r.Name, args)
	for _, p := range r.Params {
		a, ok := unused[p.Name.Text]
		if !ok {
			continue
		}
		delete(unused, p.Name.Text)
		if _, flag := a.attribute.keyed("flag"); flag && p.Default != nil {
			c.report(lint.CodeJustAttributeArguments, p.Name.Span, "flag parameter `%s` may not have a default", p.Name.Text)
		}
		_, multiple := a.attribute.keyed("multiple")
		if multiple || p.Variadic != Singular {
			continue
		}
		for _, key := range []string{"max", "min"} {
			if k, ok := a.attribute.keyed(key); ok {
				c.report(lint.CodeJustAttributeArguments, k.Key.Span, "argument attribute `%s` only valid with `multiple` or a variadic parameter", key)
				break
			}
		}
	}
	for _, a := range args {
		if _, undefined := unused[a.parameter]; undefined && r.Colon {
			c.report(lint.CodeJustAttributeArguments, a.name.Span, "argument attribute for undefined argument `%s`", a.parameter)
		}
	}
}

func (c *checker) duplicateOptions(recipe Name, args []argAttribute) {
	sorted := slices.Clone(args)
	slices.SortFunc(sorted, c.compareArgAttributes)
	longs, shorts := map[string]bool{}, map[rune]bool{}
	for _, a := range sorted {
		if o := a.long; o != nil && o.known {
			if longs[o.value] {
				c.report(lint.CodeJustAttributeArguments, o.span(), "recipe `%s` defines option `--%s` multiple times", recipe.Text, o.value)
				continue
			}
			longs[o.value] = true
		}
		if o := a.short; o != nil && o.known && o.value != "" {
			short, _ := utf8.DecodeRuneInString(o.value)
			if shorts[short] {
				c.report(lint.CodeJustAttributeArguments, o.span(), "recipe `%s` defines option `-%c` multiple times", recipe.Text, short)
			}
			shorts[short] = true
		}
	}
}

func (c *checker) compareArgAttributes(a, b argAttribute) int {
	return cmp.Or(
		c.compareKeys(a, b, "flag"),
		c.compareKeys(a, b, "help"),
		compareOptional(a.long, b.long, func(x, y *argOption) int { return c.compareLiterals(x.literal, y.literal) }),
		compareOptional(bareKey(a.long), bareKey(b.long), func(x, y *Name) int { return c.compareTokens(x.Span, y.Span) }),
		compareOptional(a.max, b.max, func(x, y *uint64) int { return cmp.Compare(*x, *y) }),
		c.compareKeys(a, b, "max"),
		compareOptional(a.min, b.min, func(x, y *uint64) int { return cmp.Compare(*x, *y) }),
		c.compareKeys(a, b, "min"),
		c.compareKeys(a, b, "multiple"),
		c.compareLiterals(a.name, b.name),
	)
}

func bareKey(o *argOption) *Name {
	if o == nil {
		return nil
	}
	return o.bare
}

func compareOptional[T any](a, b *T, compare func(x, y *T) int) int {
	if a == nil || b == nil {
		return compareBool(a != nil, b != nil)
	}
	return compare(a, b)
}

func (c *checker) compareKeys(a, b argAttribute, key string) int {
	ka, oka := a.attribute.keyed(key)
	kb, okb := b.attribute.keyed(key)
	if !oka || !okb {
		return compareBool(oka, okb)
	}
	return c.compareTokens(ka.Key.Span, kb.Key.Span)
}

func (c *checker) compareLiterals(a, b *Expr) int {
	x, y := a.Str, b.Str
	return cmp.Or(
		strings.Compare(x.Value, y.Value),
		compareBool(x.Prefix == 'x', y.Prefix == 'x'),
		cmp.Compare(strings.IndexByte("`\"'", x.Quote[0]), strings.IndexByte("`\"'", y.Quote[0])),
		compareBool(x.Indented(), y.Indented()),
		c.compareTokens(stringToken(x), stringToken(y)),
	)
}

func stringToken(s *StringLit) Span {
	return Span{Start: s.Inner.Start - len(s.Quote), End: s.Inner.End + len(s.Quote)}
}

func (c *checker) compareTokens(a, b Span) int {
	return cmp.Or(
		cmp.Compare(c.f.column(a.Start), c.f.column(b.Start)),
		cmp.Compare(a.End-a.Start, b.End-b.Start),
		cmp.Compare(a.Start, b.Start),
	)
}

type constFailure struct {
	span    Span
	message string
}

func (c *checker) constant(code lint.Code, e *Expr) {
	if failure, failed := c.constFailure(e); failed {
		c.report(code, failure.span, "%s", failure.message)
	}
}

func (c *checker) constFailure(e *Expr) (constFailure, bool) {
	if e == nil {
		return constFailure{}, false
	}
	switch e.Kind {
	case ExprBacktick:
		return constFailure{e.Span, "cannot call backticks in const context"}, true
	case ExprCall:
		if e.Name.Text == "assert" && len(e.Args) > 0 {
			return c.assertion(e)
		}
		return constFailure{e.Name.Span, "cannot call functions in const context"}, true
	case ExprVariable:
		if c.nonConstVariable(e.Name.Text) {
			return constFailure{e.Name.Span, fmt.Sprintf("cannot access non-const variable `%s` in const context", e.Name.Text)}, true
		}
		return constFailure{}, false
	case ExprIf:
		return c.constConditional(e)
	case ExprBinary:
		if e.Op == "&&" || e.Op == "||" {
			return c.constShortCircuit(e)
		}
	case ExprString:
		return c.constFirst(e.Str.Interps())
	case ExprNot, ExprRoot, ExprList, ExprGroup, ExprBad:
	}
	return c.constFirst(e.Args)
}

func (c *checker) constFirst(exprs []*Expr) (constFailure, bool) {
	for _, x := range exprs {
		if failure, failed := c.constFailure(x); failed {
			return failure, true
		}
	}
	return constFailure{}, false
}

func (c *checker) assertion(e *Expr) (constFailure, bool) {
	if failure, failed := c.constFailure(e.Args[0]); failed {
		return failure, true
	}
	holds, known := constants{f: c.f, visiting: map[string]bool{}}.condition(e.Args[0])
	if !known || holds {
		return constFailure{}, false
	}
	if len(e.Args) > 1 {
		if failure, failed := c.constFailure(e.Args[1]); failed {
			return failure, true
		}
	}
	return constFailure{e.Name.Span, "assertion fails in const context"}, true
}

func (c *checker) constConditional(e *Expr) (constFailure, bool) {
	if failure, failed := c.constFailure(e.Args[0]); failed {
		return failure, true
	}
	holds, known := constants{f: c.f, visiting: map[string]bool{}}.condition(e.Args[0])
	switch {
	case !known:
		return constFailure{}, false
	case holds:
		return c.constFailure(e.Args[1])
	case len(e.Args) > 2:
		return c.constFailure(e.Args[2])
	}
	return constFailure{}, false
}

func (c *checker) constShortCircuit(e *Expr) (constFailure, bool) {
	if failure, failed := c.constFailure(e.Args[0]); failed {
		return failure, true
	}
	values, known := c.f.Constant(e.Args[0])
	if !known || (len(values) > 0) == (e.Op == "||") {
		return constFailure{}, false
	}
	return c.constFailure(e.Args[1])
}

func (c *checker) nonConstVariable(name string) bool {
	if failed, ok := c.constVariables[name]; ok {
		return failed
	}
	c.constVariables[name] = false
	for _, a := range slices.Backward(c.f.Assignments()) {
		if a.Name.Text == name {
			_, failed := c.constFailure(a.Value)
			c.constVariables[name] = failed
			return failed
		}
	}
	return false
}

func (c *checker) constAttributes(r *Recipe) {
	for _, a := range r.Attributes {
		for _, arg := range a.Args {
			if slices.Contains(constAttributeArguments[a.Name.Text], arg.Key.Text) {
				c.constant(lint.CodeJustAttributeArguments, arg.Value)
			}
		}
	}
}
