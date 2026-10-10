package parser

import "mvdan.cc/sh/v3/syntax"

func (c Command) IsSource() bool { return c.Name == "source" || c.Name == "." }

func (c Command) SourcedFile() (*syntax.Word, bool) {
	if !c.IsSource() {
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
