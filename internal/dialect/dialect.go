// Package dialect identifies which shell a document is written in and
// exposes the feature set of each shell so downstream features branch
// on structured capabilities instead of string comparisons.
package dialect

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/directive"
)

type Dialect uint8

const (
	Bash Dialect = iota
	Zsh
	Posix
	Dash
	Ksh
	Busybox
)

func (d Dialect) String() string {
	switch d {
	case Bash:
		return "bash"
	case Zsh:
		return "zsh"
	case Posix:
		return "sh"
	case Dash:
		return "dash"
	case Ksh:
		return "ksh"
	case Busybox:
		return "busybox"
	}
	return "unknown"
}

// FromName maps a shell name, dialect alias or language identifier to a
// dialect. It accepts every spelling the detectors can produce so each
// source (shebang, languageId, shellcheck directive, modeline) funnels
// through one table.
func FromName(name string) (Dialect, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash", "bats", "shellscript", "bashrc":
		return Bash, true
	case "zsh":
		return Zsh, true
	case "sh", "posix", "posixsh":
		return Posix, true
	case "dash", "ash":
		return Dash, true
	case "busybox", "busybox sh", "busybox ash":
		return Busybox, true
	case "ksh", "ksh88", "ksh93", "mksh", "oksh":
		return Ksh, true
	}
	return 0, false
}

type Features struct {
	Arrays              bool
	AssociativeArrays   bool
	DoubleBracket       bool
	ArithmeticCommand   bool
	ProcessSubstitution bool
	GlobQualifiers      bool
	Coproc              bool
	Nameref             bool
	Local               bool
	Select              bool
	SplitsUnquoted      bool
	LastPipeInShell     bool
	DeclarationAssigns  bool
}

func (d Dialect) Features() Features {
	switch d {
	case Bash:
		return Features{
			Arrays: true, AssociativeArrays: true, DoubleBracket: true,
			ArithmeticCommand: true, ProcessSubstitution: true, Coproc: true,
			Nameref: true, Local: true, Select: true, SplitsUnquoted: true, DeclarationAssigns: true,
		}
	case Zsh:
		return Features{
			Arrays: true, AssociativeArrays: true, DoubleBracket: true,
			ArithmeticCommand: true, ProcessSubstitution: true, GlobQualifiers: true,
			Coproc: true, Nameref: true, Local: true, Select: true, LastPipeInShell: true, DeclarationAssigns: true,
		}
	case Posix:
		return Features{SplitsUnquoted: true}
	case Dash:
		return Features{Local: true, SplitsUnquoted: true, DeclarationAssigns: true}
	case Busybox:
		return Features{DoubleBracket: true, Local: true, SplitsUnquoted: true, DeclarationAssigns: true}
	case Ksh:
		return Features{
			Arrays: true, AssociativeArrays: true, DoubleBracket: true,
			ArithmeticCommand: true, ProcessSubstitution: true, Coproc: true,
			Nameref: true, Local: true, Select: true, SplitsUnquoted: true, LastPipeInShell: true, DeclarationAssigns: true,
		}
	}
	return Features{}
}

// Input is everything detection may look at for one document.
type Input struct {
	URI        string
	LanguageID string
	Text       string
}

// Detect resolves the dialect of a document. Precedence: explicit
// override, a shellcheck shell directive in the comment header before
// the first command (an unrecognised shell there means bash, as in
// shellcheck), shebang (with interpreter resolved through PATH so
// `#!/usr/bin/env bash` works, flags like `#!/bin/sh -e` tolerated),
// file extension, editor languageId, vim modeline. The default is
// bash, matching the common editor setup where shellscript buffers
// are bash.
func Detect(override *Dialect, in Input) Dialect {
	if override != nil {
		return *override
	}
	if name, ok := headerShellDirective(in.Text); ok {
		if d, known := FromName(name); known {
			return d
		}
		return Bash
	}
	if d, ok := FromShebang(in.Text); ok {
		return d
	}
	if d, ok := FromFileName(in.URI); ok {
		return d
	}
	if d, ok := FromName(in.LanguageID); ok {
		return d
	}
	if d, ok := fromModeline(in.Text); ok {
		return d
	}
	return Bash
}

func FromShebang(text string) (Dialect, bool) {
	name, ok := Interpreter(text)
	if !ok {
		return 0, false
	}
	return FromName(name)
}

func Interpreter(text string) (string, bool) {
	first, _, _ := strings.Cut(text, "\n")
	rest, ok := strings.CutPrefix(first, "#!")
	if !ok {
		return "", false
	}
	fields := strings.Fields(rest)
	if len(fields) > 0 && filepath.Base(fields[0]) == "env" {
		i := slices.IndexFunc(fields[1:], func(field string) bool {
			return !strings.HasPrefix(field, "-") && !strings.Contains(field, "=")
		})
		if i < 0 {
			return "", false
		}
		fields = fields[1+i:]
	}
	if len(fields) == 0 {
		return "", false
	}
	name := filepath.Base(fields[0])
	if name == "busybox" && len(fields) > 1 {
		return name + " " + fields[1], true
	}
	return name, true
}

func FromFileName(path string) (Dialect, bool) {
	base := filepath.Base(path)
	switch {
	case strings.HasPrefix(base, ".bash"), strings.HasSuffix(base, ".bash"), strings.HasSuffix(base, ".bats"):
		return Bash, true
	case strings.HasSuffix(base, ".zsh"), base == ".zshrc", base == ".zprofile", base == ".zshenv", base == ".zlogin", base == ".zlogout":
		return Zsh, true
	case strings.HasSuffix(base, ".dash"):
		return Dash, true
	case strings.HasSuffix(base, ".sh"), base == ".profile":
		return Posix, true
	case strings.HasSuffix(base, ".ksh"), base == ".kshrc", base == ".mkshrc":
		return Ksh, true
	}
	return 0, false
}

func headerShellDirective(text string) (string, bool) {
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "#") {
			return "", false
		}
		if d, ok := directive.Parse(trimmed); ok {
			if value, ok := d.Value("shell"); ok {
				return value, true
			}
		}
	}
	return "", false
}

func fromModeline(text string) (Dialect, bool) {
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		modeline, ok := strings.CutPrefix(trimmed, "# vim:")
		if !ok {
			modeline, ok = strings.CutPrefix(trimmed, "# vi:")
			if !ok {
				continue
			}
		}
		for setting := range strings.FieldsSeq(modeline) {
			value, ok := strings.CutPrefix(setting, "ft=")
			if !ok {
				value, ok = strings.CutPrefix(setting, "filetype=")
				if !ok {
					continue
				}
			}
			if d, ok := FromName(strings.TrimSuffix(value, ":")); ok {
				return d, true
			}
		}
	}
	return 0, false
}

type Set uint8

const All Set = 1<<Bash | 1<<Zsh | 1<<Posix | 1<<Dash | 1<<Ksh | 1<<Busybox

func SetOf(dialects ...Dialect) Set {
	var s Set
	for _, d := range dialects {
		s |= 1 << d
	}
	return s
}

func (s Set) Has(d Dialect) bool { return s&(1<<d) != 0 }
