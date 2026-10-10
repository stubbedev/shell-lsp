package lint

import "slices"

type meta struct {
	severity Severity
	tag      Tag
}

var catalog = map[Code]meta{
	1071: {severity: SeverityError},
	2006: {severity: SeverityStyle, tag: TagDeprecated},
	2046: {severity: SeverityWarning},
	2048: {severity: SeverityWarning},
	2068: {severity: SeverityError},
	2086: {severity: SeverityInfo},
	2206: {severity: SeverityWarning},
	2207: {severity: SeverityWarning},
	2223: {severity: SeverityInfo},
}

func Codes() []Code {
	out := make([]Code, 0, len(catalog))
	for code := range catalog {
		out = append(out, code)
	}
	slices.Sort(out)
	return out
}
