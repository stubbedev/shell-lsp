package actions

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

var shebangs = map[dialect.Dialect]string{
	dialect.Bash:    "#!/usr/bin/env bash",
	dialect.Zsh:     "#!/usr/bin/env zsh",
	dialect.Ksh:     "#!/usr/bin/env ksh",
	dialect.Posix:   "#!/bin/sh",
	dialect.Dash:    "#!/usr/bin/env dash",
	dialect.Busybox: "#!/bin/busybox sh",
}

func addShebang(s *site) (Change, bool) {
	text := s.Tree.Text()
	shebang, ok := shebangs[s.Tree.Dialect]
	if !ok || strings.HasPrefix(text, "#!") || s.Tree.Bats() || dialect.CompletionFile(text) {
		return Change{}, false
	}
	return Change{Title: "Add " + shebang, Edits: []textedit.Edit{{Start: 0, End: 0, NewText: shebang + "\n"}}}, true
}
