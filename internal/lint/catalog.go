package lint

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

type meta struct {
	severity Severity
	shells   dialect.Set
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
