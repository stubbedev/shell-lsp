package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/directive"
)

type meta struct {
	severity Severity
	tag      Tag
	optional string
	summary  string
}

type OptionalCheck struct {
	Name    string
	Summary string
	Codes   []Code
}

var behaviourOptionals = []OptionalCheck{{
	Name:    uppercaseOptional,
	Summary: "SC2154 also reports unassigned variables with all-uppercase names, which are otherwise taken to come from the environment.",
}}

func OptionalChecks() []OptionalCheck {
	out := slices.Clone(behaviourOptionals)
	index := make(map[string]int, len(out))
	for i, check := range out {
		index[check.Name] = i
	}
	for _, code := range Codes() {
		name := catalog[code].optional
		if name == "" {
			continue
		}
		i, ok := index[name]
		if !ok {
			i = len(out)
			index[name] = i
			out = append(out, OptionalCheck{Name: name})
		}
		out[i].Codes = append(out[i].Codes, code)
	}
	slices.SortFunc(out, func(a, b OptionalCheck) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func IsOptionalCheck(name string) bool {
	return slices.ContainsFunc(OptionalChecks(), func(check OptionalCheck) bool { return check.Name == name })
}

func EnablesOptional(name string) bool {
	return name == directive.All || IsOptionalCheck(name)
}

func Codes() []Code {
	out := make([]Code, 0, len(catalog))
	for code := range catalog {
		out = append(out, code)
	}
	slices.Sort(out)
	return out
}
