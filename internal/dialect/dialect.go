package dialect

import (
	"fmt"
	"iter"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
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

const genericLanguageID = "shellscript"

type family uint8

const (
	posixFamily family = iota
	zshFamily
	kshFamily
)

type spec struct {
	name     string
	aliases  []string
	family   family
	width    uint8
	language syntax.LangVariant
	features Features
}

var specs = [...]spec{
	Bash: {
		name: "bash", aliases: []string{"bats", genericLanguageID, "bashrc"},
		family: posixFamily, width: 3, language: syntax.LangBash,
		features: Features{
			Arrays: true, AssociativeArrays: true, DoubleBracket: true,
			ArithmeticCommand: true, ProcessSubstitution: true, Coproc: true,
			Nameref: true, Local: true, Select: true, SplitsUnquoted: true, GlobsUnquoted: true, DeclarationAssigns: true,
			QuotedRegexLiteral: true, SubscriptsRunCode: true,
		},
	},
	Zsh: {
		name: "zsh", family: zshFamily, language: syntax.LangZsh,
		features: Features{
			Arrays: true, AssociativeArrays: true, DoubleBracket: true,
			ArithmeticCommand: true, ProcessSubstitution: true, GlobQualifiers: true,
			Coproc: true, Nameref: true, Local: true, Select: true, LastPipeInShell: true, DeclarationAssigns: true,
			ArithmeticStatus: true, AssignsPositionals: true, AssociativePairs: true,
		},
	},
	Posix: {
		name: "sh", aliases: []string{"posix", "posixsh"},
		family: posixFamily, width: 0, language: syntax.LangBash,
		features: Features{SplitsUnquoted: true, GlobsUnquoted: true, QuotedRegexLiteral: true},
	},
	Dash: {
		name: "dash", aliases: []string{"ash"},
		family: posixFamily, width: 1, language: syntax.LangBash,
		features: Features{Local: true, SplitsUnquoted: true, GlobsUnquoted: true, DeclarationAssigns: true, QuotedRegexLiteral: true},
	},
	Ksh: {
		name: "ksh", aliases: []string{"ksh88", "ksh93", "mksh", "oksh"},
		family: kshFamily, language: syntax.LangKsh93,
		features: Features{
			Arrays: true, AssociativeArrays: true, DoubleBracket: true,
			ArithmeticCommand: true, ProcessSubstitution: true, Coproc: true,
			Nameref: true, Local: true, Select: true, SplitsUnquoted: true, GlobsUnquoted: true, LastPipeInShell: true,
			DeclarationAssigns: true, QuotedRegexLiteral: true, SubscriptsRunCode: true,
		},
	},
	Busybox: {
		name: "busybox", aliases: []string{"busybox sh", "busybox ash"},
		family: posixFamily, width: 2, language: syntax.LangBash,
		features: Features{
			DoubleBracket: true, Local: true, SplitsUnquoted: true, GlobsUnquoted: true, DeclarationAssigns: true,
			QuotedRegexLiteral: true,
		},
	},
}

var All = func() Set {
	var s Set
	for d := range specs {
		s |= 1 << d
	}
	return s
}()

var byName = func() map[string]Dialect {
	out := map[string]Dialect{}
	for d := range All.Members() {
		for _, name := range append([]string{d.String()}, specs[d].aliases...) {
			out[name] = d
		}
	}
	return out
}()

func (d Dialect) spec() spec {
	if int(d) >= len(specs) {
		panic(fmt.Sprintf("dialect: unknown dialect %d", d))
	}
	return specs[d]
}

func (d Dialect) String() string { return d.spec().name }

func (d Dialect) Language() syntax.LangVariant { return d.spec().language }

func (d Dialect) Features() Features { return d.spec().features }

func (d Dialect) family() family { return d.spec().family }

func Names() []string {
	names := make([]string, 0, len(specs))
	for d := range All.Members() {
		names = append(names, d.String())
	}
	return names
}

func FromName(name string) (Dialect, bool) {
	d, ok := byName[strings.ToLower(strings.TrimSpace(name))]
	return d, ok
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
// a zsh completion system tag (`#compdef`, `#autoload`) on the first
// line, file name or extension, editor languageId, vim modeline. The
// default is bash, matching the common editor setup where shellscript
// buffers are bash; Resolve also reports what decided it.
func Detect(override *Dialect, in Input) Dialect {
	return Resolve(override, in).Dialect
}

func Resolve(override *Dialect, in Input) Detection {
	found := decide(override, in)
	found.Flavor = ShellOf(found.Dialect, in).Flavor
	return found
}

func decide(override *Dialect, in Input) Detection {
	if override != nil {
		return Detection{Dialect: *override, Source: SourceOverride, Evidence: override.String()}
	}
	if name, ok := headerShellDirective(in.Text); ok {
		d, known := FromName(name)
		if !known {
			d = Bash
		}
		return Detection{Dialect: d, Source: SourceDirective, Evidence: name}
	}
	if name, ok := Interpreter(in.Text); ok {
		if d, known := FromName(name); known {
			first, _, _ := strings.Cut(in.Text, "\n")
			return Detection{Dialect: d, Source: SourceShebang, Evidence: strings.TrimSpace(first)}
		}
	}
	if tag, ok := completionTag(in.Text); ok {
		return Detection{Dialect: Zsh, Source: SourceCompletionTag, Evidence: tag}
	}
	if found, ok := fromFileName(in.URI); ok {
		return found
	}
	if d, ok := FromName(in.LanguageID); ok && in.LanguageID != genericLanguageID {
		return Detection{Dialect: d, Source: SourceLanguageID, Evidence: in.LanguageID}
	}
	if d, value, ok := fromModeline(in.Text); ok {
		return Detection{Dialect: d, Source: SourceModeline, Evidence: value}
	}
	return Detection{Dialect: Bash, Source: SourceDefault}
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

func CompletionFile(text string) bool {
	_, ok := completionTag(text)
	return ok
}

func completionTag(text string) (string, bool) {
	first, _, _ := strings.Cut(text, "\n")
	tag, _, _ := strings.Cut(strings.TrimRight(first, "\r"), " ")
	return tag, slices.Contains(completionTags, tag)
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
	found, ok := fromFileName(path)
	return found.Dialect, ok
}

var extensions = []struct {
	suffix  string
	dialect Dialect
}{
	{".bash", Bash}, {".bats", Bash}, {".zsh", Zsh}, {".dash", Dash}, {".sh", Posix}, {".ksh", Ksh},
}

func fromFileName(path string) (Detection, bool) {
	base := filepath.Base(path)
	if d, ok := rcFiles[base]; ok {
		return Detection{Dialect: d, Source: SourceFileName, Evidence: base}, true
	}
	if strings.HasPrefix(base, ".bash") {
		return Detection{Dialect: Bash, Source: SourceFileName, Evidence: base}, true
	}
	for _, ext := range extensions {
		if strings.HasSuffix(base, ext.suffix) {
			return Detection{Dialect: ext.dialect, Source: SourceExtension, Evidence: ext.suffix}, true
		}
	}
	return Detection{}, false
}

func headerShellDirective(text string) (string, bool) {
	found, ok := findShellDirective(text)
	return found.pair.Value, ok
}

type shellDirective struct {
	line directive.Line
	pair directive.Pair
}

func findShellDirective(text string) (shellDirective, bool) {
	for _, line := range directive.HeaderLines(text) {
		if pair, ok := line.Directive.Pair(directive.Shell); ok {
			return shellDirective{line: line, pair: pair}, true
		}
	}
	return shellDirective{}, false
}

func fromModeline(text string) (Dialect, string, bool) {
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
			value = strings.TrimSuffix(value, ":")
			if d, ok := FromName(value); ok {
				return d, value, true
			}
		}
	}
	return 0, "", false
}

type Set uint8

func SetOf(dialects ...Dialect) Set {
	var s Set
	for _, d := range dialects {
		s |= 1 << d
	}
	return s
}

func (s Set) Has(d Dialect) bool { return s&(1<<d) != 0 }

func (s Set) Members() iter.Seq[Dialect] {
	return func(yield func(Dialect) bool) {
		for d := Bash; s>>d != 0; d++ {
			if s.Has(d) && !yield(d) {
				return
			}
		}
	}
}

func Shared(dialects []Dialect) (Dialect, func(Dialect) bool) {
	counts := map[family]int{}
	var order []family
	for _, d := range dialects {
		if counts[d.family()] == 0 {
			order = append(order, d.family())
		}
		counts[d.family()]++
	}
	if len(order) == 0 {
		return Posix, func(d Dialect) bool { return d.family() == posixFamily }
	}
	chosen := order[0]
	for _, f := range order[1:] {
		if counts[f] > counts[chosen] {
			chosen = f
		}
	}
	fits := func(d Dialect) bool { return d.family() == chosen }
	widest := slices.MaxFunc(slices.DeleteFunc(slices.Clone(dialects), func(d Dialect) bool { return !fits(d) }), func(a, b Dialect) int {
		return int(a.spec().width) - int(b.spec().width)
	})
	return widest, fits
}
