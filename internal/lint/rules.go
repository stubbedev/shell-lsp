package lint

var rules = []rule{
	{node: checkUnquotedExpansion},
	{node: checkUnquotedSubstitution},
}
