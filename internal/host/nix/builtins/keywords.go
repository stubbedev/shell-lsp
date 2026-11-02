package builtins

import "slices"

type Keyword struct {
	Name string
	Doc  string
}

var keywords = []Keyword{
	{Name: "assert", Doc: "`assert cond; body` evaluates `body` when `cond` is true and stops with an error otherwise."},
	{Name: "else", Doc: "Introduces the branch of an `if` expression taken when the condition is false."},
	{Name: "if", Doc: "`if cond then a else b` picks one of two expressions; `cond` must be a Boolean and the `else` branch is mandatory."},
	{Name: "in", Doc: "Ends the bindings of a `let` and starts the expression that can see them."},
	{Name: "inherit", Doc: "Binds names to same-named variables from the enclosing scope, or with `inherit (set) a b;` to attributes of `set`."},
	{Name: "let", Doc: "`let bindings in body` defines mutually recursive local names visible in `body`."},
	{Name: "or", Doc: "`set.attr or fallback` yields `fallback` when the selected attribute path is missing."},
	{Name: "rec", Doc: "Marks an attribute set whose attributes can refer to one another."},
	{Name: "then", Doc: "Introduces the branch of an `if` expression taken when the condition is true."},
	{Name: "with", Doc: "`with set; body` brings the attributes of `set` into scope in `body`; lexically bound names take precedence over them."},
}

func Keywords() []Keyword {
	return slices.Clone(keywords)
}
