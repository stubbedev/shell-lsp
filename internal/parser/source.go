package parser

import "github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"

func IsSourceName(name string) bool { return name == "source" || name == "." }

func (c Command) IsSourceBuiltin() bool { return IsSourceName(c.Name) }

func (c Command) SourcedFile() (*syntax.Word, bool) {
	if !c.IsSourceBuiltin() {
		return nil, false
	}
	args := c.Args
	if len(args) > 0 {
		switch option, _ := Literal(args[0]); option {
		case "--":
			args = args[1:]
		case "-p":
			args = args[min(2, len(args)):]
		}
	}
	if len(args) == 0 {
		return nil, false
	}
	return args[0], true
}
