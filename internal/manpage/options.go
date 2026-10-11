package manpage

import (
	"regexp"
	"slices"
	"strings"
)

var optionName = regexp.MustCompile(`^--?[A-Za-z0-9?#@][A-Za-z0-9_-]*`)

type Option struct {
	Names  []string
	Doc    string
	Valued bool
}

func collectOptions(doc document) []Option {
	var options []Option
	seen := map[string]bool{}
	for _, s := range doc.sections {
		if s.name == sectionName || s.name == sectionSynopsis {
			continue
		}
		options = appendOptions(options, s.blocks, seen)
	}
	return options
}

func appendOptions(options []Option, blocks []block, seen map[string]bool) []Option {
	var grouped Option
	for _, b := range blocks {
		if b.kind != taggedBlock || !strings.HasPrefix(b.tag, "-") {
			grouped = Option{}
			continue
		}
		option := grouped
		option.addNames(b.tag)
		if b.text == "" {
			grouped = option
			continue
		}
		grouped = Option{}
		if len(option.Names) == 0 || slices.ContainsFunc(option.Names, func(n string) bool { return seen[n] }) {
			continue
		}
		for _, n := range option.Names {
			seen[n] = true
		}
		option.Doc = b.text
		options = append(options, option)
	}
	return options
}

func (o *Option) addNames(tag string) {
	for part := range strings.SplitSeq(tag, ",") {
		part = strings.TrimSpace(part)
		name := optionName.FindString(part)
		if name == "" || name == "-" || name == "--" {
			continue
		}
		o.Valued = o.Valued || takesValue(part[len(name):])
		if !slices.Contains(o.Names, name) {
			o.Names = append(o.Names, name)
		}
	}
}

func takesValue(syntax string) bool {
	if strings.HasPrefix(syntax, "=") {
		return true
	}
	value := strings.TrimLeft(syntax, " ")
	return len(value) < len(syntax) && value != "" && !strings.ContainsAny(value[:1], "-[|")
}
