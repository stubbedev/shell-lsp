package parser

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

var commandWrappers = map[string]string{
	"command": "",
	"builtin": "",
	"busybox": "",
	"exec":    "a",
	"time":    "fo",
}

func IsCommandWrapper(name string, bats bool) bool {
	_, wrapper := commandWrappers[name]
	return wrapper || (bats && name == "run")
}

func CommandStart(args []*syntax.Word, bats bool) int {
	for i := 0; i < len(args); {
		lit, ok := Literal(args[i])
		if !ok || !IsCommandWrapper(lit, bats) {
			return i
		}
		withArgument := commandWrappers[lit]
		i++
		for i < len(args) {
			opt, ok := Literal(args[i])
			if !ok || !strings.HasPrefix(opt, "-") || opt == "-" {
				break
			}
			i++
			if opt == "--" {
				break
			}
			if strings.ContainsAny(opt[1:], withArgument) {
				i++
			}
		}
	}
	return -1
}
