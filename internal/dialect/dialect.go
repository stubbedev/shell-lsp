// Package dialect identifies which shell a document is written in and
// exposes the feature set of each shell so downstream features branch
// on structured capabilities instead of string comparisons.
package dialect

import (
	"path/filepath"
	"strings"
)

type Dialect uint8

const (
	Bash Dialect = iota
	Zsh
	Posix
	Dash
	Ksh
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
	}
	return "unknown"
}

// FromName maps a shell name, dialect alias or language identifier to a
// dialect. It accepts every spelling the detectors can produce so each
// source (shebang, languageId, shellcheck directive, modeline) funnels
// through one table.
func FromName(name string) (Dialect, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash", "shellscript", "bashrc":
		return Bash, true
	case "zsh":
		return Zsh, true
	case "sh", "posix", "posixsh":
		return Posix, true
	case "dash", "ash":
		return Dash, true
	case "ksh", "ksh93", "mksh":
		return Ksh, true
	}
	return 0, false
}

func (d Dialect) ShellcheckName() (string, bool) {
	switch d {
	case Bash, Posix, Dash, Ksh:
		return d.String(), true
	}
	return "", false
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
}

func (d Dialect) Features() Features {
	switch d {
	case Bash:
		return Features{
			Arrays: true, AssociativeArrays: true, DoubleBracket: true,
			ArithmeticCommand: true, ProcessSubstitution: true, Coproc: true,
			Nameref: true, Local: true, Select: true,
		}
	case Zsh:
		return Features{
			Arrays: true, AssociativeArrays: true, DoubleBracket: true,
			ArithmeticCommand: true, ProcessSubstitution: true, GlobQualifiers: true,
			Coproc: true, Nameref: true, Local: true, Select: true,
		}
	case Posix:
		return Features{}
	case Dash:
		return Features{Local: true}
	case Ksh:
		return Features{
			Arrays: true, AssociativeArrays: true, DoubleBracket: true,
			ArithmeticCommand: true, ProcessSubstitution: true, Coproc: true,
			Nameref: true, Local: true, Select: true,
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

// Detect resolves the dialect of a document. Precedence, per issue #4:
// explicit override, shebang (with interpreter resolved through PATH so
// `#!/usr/bin/env bash` works, flags like `#!/bin/sh -e` tolerated),
// file extension, editor languageId, shellcheck directive, vim
// modeline. The default is bash, matching the common editor setup
// where shellscript buffers are bash.
func Detect(override *Dialect, in Input) Dialect {
	if override != nil {
		return *override
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
	if d, ok := fromShellcheckDirective(in.Text); ok {
		return d
	}
	if d, ok := fromModeline(in.Text); ok {
		return d
	}
	return Bash
}

// FromShebang parses the first line of a script. The interpreter is
// resolved through PATH, so `#!/usr/bin/env zsh` and custom install
// prefixes both work; interpreter flags are ignored.
func FromShebang(text string) (Dialect, bool) {
	first, _, _ := strings.Cut(text, "\n")
	if !strings.HasPrefix(first, "#!") {
		return 0, false
	}
	fields := strings.Fields(strings.TrimPrefix(first, "#!"))
	if len(fields) == 0 {
		return 0, false
	}
	interpreter := filepath.Base(fields[0])
	if interpreter == "env" && len(fields) > 1 {
		for _, flag := range fields[1:] {
			if strings.HasPrefix(flag, "-") {
				continue
			}
			interpreter = filepath.Base(flag)
			break
		}
	}
	return FromName(interpreter)
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

func fromShellcheckDirective(text string) (Dialect, bool) {
	seen := 0
	for line := range strings.SplitSeq(text, "\n") {
		seen++
		if seen > 50 {
			break
		}
		trimmed := strings.TrimSpace(line)
		fields := strings.Fields(trimmed)
		if len(fields) < 2 || fields[0]+" "+fields[1] != "# shellcheck" {
			continue
		}
		for _, field := range fields[2:] {
			if value, ok := strings.CutPrefix(field, "shell="); ok {
				return FromName(value)
			}
		}
	}
	return 0, false
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

const All Set = 1<<Bash | 1<<Zsh | 1<<Posix | 1<<Dash | 1<<Ksh

func SetOf(dialects ...Dialect) Set {
	var s Set
	for _, d := range dialects {
		s |= 1 << d
	}
	return s
}

func (s Set) Has(d Dialect) bool { return s&(1<<d) != 0 }
