package manpage

import (
	"regexp"
	"slices"
	"strings"
)

var optionName = regexp.MustCompile(`^--?[A-Za-z0-9?#@][A-Za-z0-9_-]*`)

type Option struct {
	Names []string
	Doc   string
}

func collectOptions(doc document) []Option {
	var options []Option
	seen := map[string]bool{}
	for _, s := range doc.sections {
		if s.name == sectionName || s.name == sectionSynopsis {
			continue
		}
		var grouped []string
		for _, b := range s.blocks {
			if b.kind != taggedBlock || !strings.HasPrefix(b.tag, "-") {
				grouped = nil
				continue
			}
			names := grouped
			for _, n := range optionNames(b.tag) {
				if !slices.Contains(names, n) {
					names = append(names, n)
				}
			}
			if b.text == "" {
				grouped = names
				continue
			}
			grouped = nil
			if len(names) == 0 || slices.ContainsFunc(names, func(n string) bool { return seen[n] }) {
				continue
			}
			for _, n := range names {
				seen[n] = true
			}
			options = append(options, Option{Names: names, Doc: b.text})
		}
	}
	return options
}

func optionNames(tag string) []string {
	var names []string
	for part := range strings.SplitSeq(tag, ",") {
		name := optionName.FindString(strings.TrimSpace(part))
		if name != "" && name != "-" && name != "--" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}
