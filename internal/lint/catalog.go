package lint

import "slices"

type meta struct {
	severity Severity
	tag      Tag
	optional optionalCheck
	summary  string
	bug      bool
}

func Codes() []Code {
	out := make([]Code, 0, len(catalog))
	for code := range catalog {
		out = append(out, code)
	}
	slices.Sort(out)
	return out
}
