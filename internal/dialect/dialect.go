// Package dialect identifies which shell a document is written in and
// exposes the feature set of each shell so downstream features branch
// on structured capabilities instead of string comparisons.
package dialect

import (
	"maps"
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
	ArithmeticStatus    bool
	AssignsPositionals  bool
	AssociativePairs    bool
	GlobsUnquoted       bool
	QuotedRegexLiteral  bool
	SubscriptsRunCode   bool
}

func (d Dialect) Features() Features {
	switch d {
	case Bash:
		return Features{
			Arrays: true, AssociativeArrays: true, DoubleBracket: true,
			ArithmeticCommand: true, ProcessSubstitution: true, Coproc: true,
			Nameref: true, Local: true, Select: true, SplitsUnquoted: true, GlobsUnquoted: true, DeclarationAssigns: true,
			QuotedRegexLiteral: true, SubscriptsRunCode: true,
		}
	case Zsh:
		return Features{
			Arrays: true, AssociativeArrays: true, DoubleBracket: true,
			ArithmeticCommand: true, ProcessSubstitution: true, GlobQualifiers: true,
			Coproc: true, Nameref: true, Local: true, Select: true, LastPipeInShell: true, DeclarationAssigns: true,
			ArithmeticStatus: true, AssignsPositionals: true, AssociativePairs: true,
		}
	case Posix:
		return Features{SplitsUnquoted: true, GlobsUnquoted: true, QuotedRegexLiteral: true}
	case Dash:
		return Features{Local: true, SplitsUnquoted: true, GlobsUnquoted: true, DeclarationAssigns: true, QuotedRegexLiteral: true}
	case Busybox:
		return Features{
			DoubleBracket: true, Local: true, SplitsUnquoted: true, GlobsUnquoted: true, DeclarationAssigns: true,
			QuotedRegexLiteral: true,
		}
	case Ksh:
		return Features{
			Arrays: true, AssociativeArrays: true, DoubleBracket: true,
			ArithmeticCommand: true, ProcessSubstitution: true, Coproc: true,
			Nameref: true, Local: true, Select: true, SplitsUnquoted: true, GlobsUnquoted: true, LastPipeInShell: true,
			DeclarationAssigns: true, QuotedRegexLiteral: true, SubscriptsRunCode: true,
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

func (in Input) Bats() bool {
	if strings.HasSuffix(in.URI, ".bats") {
		return true
	}
	name, ok := Interpreter(in.Text)
	return ok && name == "bats"
}

func (in Input) Mksh() bool {
	if name, ok := headerShellDirective(in.Text); ok {
		return name == "mksh"
	}
	if name, ok := Interpreter(in.Text); ok {
		return name == "mksh"
	}
	return filepath.Base(in.URI) == ".mkshrc"
}

// Detect resolves the dialect of a document. Precedence: explicit
// override, a shellcheck shell directive in the comment header before
// the first command (an unrecognised shell there means bash, as in
// shellcheck), shebang (with interpreter resolved through PATH so
// `#!/usr/bin/env bash` works, flags like `#!/bin/sh -e` tolerated),
// a zsh completion system tag (`#compdef`, `#autoload`) on the first
// line, file extension, editor languageId, vim modeline. The default is
// bash, matching the common editor setup where shellscript buffers
// are bash; DetectNamed also reports whether anything named the shell.
func Detect(override *Dialect, in Input) Dialect {
	d, _ := DetectNamed(override, in)
	return d
}

func DetectNamed(override *Dialect, in Input) (Dialect, bool) {
	if override != nil {
		return *override, true
	}
	if name, ok := headerShellDirective(in.Text); ok {
		if d, known := FromName(name); known {
			return d, true
		}
		return Bash, true
	}
	if d, ok := FromShebang(in.Text); ok {
		return d, true
	}
	if completionTag(in.Text) {
		return Zsh, true
	}
	if d, ok := FromFileName(in.URI); ok {
		return d, true
	}
	if d, ok := FromName(in.LanguageID); ok {
		return d, true
	}
	if d, ok := fromModeline(in.Text); ok {
		return d, true
	}
	return Bash, false
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
	if name == nixShell {
		if interpreter, ok := nixShellInterpreter(text); ok {
			return interpreter, true
		}
	}
	return name, true
}

const nixShell = "nix-shell"

func nixShellInterpreter(text string) (string, bool) {
	for line := range strings.SplitSeq(text, "\n") {
		rest, ok := strings.CutPrefix(line, "#!")
		if !ok {
			return "", false
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 || filepath.Base(fields[0]) != nixShell {
			continue
		}
		if i := slices.Index(fields, "-i"); i >= 0 && i+1 < len(fields) {
			return filepath.Base(fields[i+1]), true
		}
	}
	return "", false
}

var completionTags = []string{"#compdef", "#autoload"}

func completionTag(text string) bool {
	first, _, _ := strings.Cut(text, "\n")
	tag, _, _ := strings.Cut(strings.TrimRight(first, "\r"), " ")
	return slices.Contains(completionTags, tag)
}

var rcFiles = map[string]Dialect{
	".bashrc": Bash, ".bash_profile": Bash, ".bash_login": Bash, ".bash_logout": Bash, ".bash_aliases": Bash,
	".profile": Posix, ".zshrc": Zsh, ".zprofile": Zsh, ".zshenv": Zsh, ".zlogin": Zsh, ".zlogout": Zsh,
	".kshrc": Ksh, ".mkshrc": Ksh,
}

func IsRCFile(path string) bool {
	_, ok := rcFiles[filepath.Base(path)]
	return ok
}

func RCFileNames() []string {
	return slices.Sorted(maps.Keys(rcFiles))
}

func FromFileName(path string) (Dialect, bool) {
	base := filepath.Base(path)
	if d, ok := rcFiles[base]; ok {
		return d, true
	}
	switch {
	case strings.HasPrefix(base, ".bash"), strings.HasSuffix(base, ".bash"), strings.HasSuffix(base, ".bats"):
		return Bash, true
	case strings.HasSuffix(base, ".zsh"):
		return Zsh, true
	case strings.HasSuffix(base, ".dash"):
		return Dash, true
	case strings.HasSuffix(base, ".sh"):
		return Posix, true
	case strings.HasSuffix(base, ".ksh"):
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
			if value, ok := d.Value(directive.Shell); ok {
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
