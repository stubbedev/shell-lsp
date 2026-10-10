package manpage

import (
	"regexp"
	"slices"
	"strings"
)

var (
	optionName = regexp.MustCompile(`^--?[A-Za-z0-9?#@][A-Za-z0-9_-]*`)
	docColumn  = regexp.MustCompile(`\s{2,}`)
)

type Option struct {
	Names []string
	Doc   string
}

func (p Page) Options() []Option {
	names := make([]string, 0, len(p.Sections))
	for name := range p.Sections {
		if name != sectionName && name != sectionSynopsis {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var options []Option
	seen := map[string]bool{}
	for _, name := range names {
		for _, opt := range parseOptions(p.Sections[name]) {
			if seen[opt.Names[0]] {
				continue
			}
			for _, n := range opt.Names {
				seen[n] = true
			}
			options = append(options, opt)
		}
	}
	return options
}

func parseOptions(lines []string) []Option {
	var options []Option
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "-") {
			continue
		}
		indent := indentation(lines[i])
		header, inline, _ := strings.Cut(docColumn.ReplaceAllString(trimmed, "\x00"), "\x00")
		opt := Option{Names: optionNames(header)}
		if len(opt.Names) == 0 {
			continue
		}
		body := []string{inline}
		for i+1 < len(lines) {
			next := lines[i+1]
			if strings.TrimSpace(next) == "" {
				break
			}
			if indentation(next) <= indent {
				break
			}
			body = append(body, next)
			i++
		}
		if doc := paragraphs(body); len(doc) > 0 {
			opt.Doc = doc[0]
		}
		options = append(options, opt)
	}
	return options
}

func optionNames(header string) []string {
	var names []string
	for part := range strings.SplitSeq(header, ",") {
		name := optionName.FindString(strings.TrimSpace(part))
		if name != "" && name != "-" && name != "--" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

func indentation(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}
