package lint

import "slices"

type meta struct {
	severity Severity
	tag      Tag
	optional string
	summary  string
}

var behaviourOptionals = []string{uppercaseOptional}

func OptionalChecks() []string {
	names := slices.Clone(behaviourOptionals)
	for _, m := range catalog {
		if m.optional != "" {
			names = append(names, m.optional)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func Codes() []Code {
	out := make([]Code, 0, len(catalog))
	for code := range catalog {
		out = append(out, code)
	}
	slices.Sort(out)
	return out
}
