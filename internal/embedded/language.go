package embedded

import (
	"path"

	"github.com/stubbedev/shell-lsp/internal/embedded/awk"
	"github.com/stubbedev/shell-lsp/internal/embedded/jq"
	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
	"github.com/stubbedev/shell-lsp/internal/embedded/sed"
)

type Language uint8

const (
	Awk Language = iota
	Sed
	Jq
)

type language struct {
	name     string
	commands []string
	parse    func(string) lang.Result
	doc      func(string, lang.Kind) (lang.Doc, bool)
	cli      cli
}

func undocumented(string, lang.Kind) (lang.Doc, bool) { return lang.Doc{}, false }

var languages = [...]language{
	Awk: {
		name:     "awk",
		commands: []string{"awk", "gawk", "mawk", "nawk", "original-awk"},
		parse:    awk.Parse,
		doc:      awk.Doc,
		cli: cli{
			short: [128]option{
				'F': valued, 'v': valued, 'f': file, 'e': source, 'E': file, 'i': valued, 'l': valued, 'W': valued,
				'b': flag, 'c': flag, 'C': flag, 'g': flag, 'h': flag, 'M': flag, 'n': flag, 'N': flag, 'O': flag,
				'P': flag, 'r': flag, 's': flag, 'S': flag, 't': flag, 'V': flag,
				'd': glued, 'D': glued, 'L': glued, 'o': glued, 'p': glued,
			},
			long: map[string]option{
				"field-separator": valued, "assign": valued, "file": file, "source": source, "exec": file,
				"include": valued, "load": valued,
				"posix": flag, "traditional": flag, "compat": flag, "re-interval": flag, "characters-as-bytes": flag,
				"sandbox": flag, "optimize": flag, "no-optimize": flag, "version": flag, "help": flag, "usage": flag,
				"use-lc-numeric": flag, "bignum": flag, "non-decimal-data": flag, "copyright": flag, "gen-pot": flag,
				"lint": flag, "lint-old": flag, "dump-variables": flag, "profile": flag, "pretty-print": flag,
				"debug": flag, "csv": flag,
			},
		},
	},
	Sed: {
		name:     "sed",
		commands: []string{"sed", "gsed"},
		parse:    sed.Parse,
		doc:      undocumented,
		cli: cli{
			short: [128]option{
				'e': source, 'f': file, 'l': valued,
				'n': flag, 'E': flag, 'r': flag, 's': flag, 'u': flag, 'z': flag, 'b': flag,
				'i': glued,
			},
			long: map[string]option{
				"expression": source, "file": file, "line-length": valued,
				"quiet": flag, "silent": flag, "regexp-extended": flag, "separate": flag, "unbuffered": flag,
				"null-data": flag, "zero-terminated": flag, "posix": flag, "sandbox": flag, "debug": flag,
				"follow-symlinks": flag, "binary": flag, "help": flag, "version": flag,
				"in-place": flag,
			},
			permute: true,
		},
	},
	Jq: {
		name:     "jq",
		commands: []string{"jq", "gojq", "jaq"},
		parse:    jq.Parse,
		doc:      jq.Doc,
		cli: cli{
			short: [128]option{
				'L': valued, 'f': unsupported,
				'n': flag, 'r': flag, 'j': flag, 'a': flag, 'c': flag, 's': flag, 'S': flag, 'C': flag, 'M': flag,
				'e': flag, 'R': flag, 'h': flag, 'V': flag,
			},
			long: map[string]option{
				"indent": valued, "arg": paired, "argjson": paired, "slurpfile": paired, "rawfile": paired,
				"from-file":  unsupported,
				"null-input": flag, "raw-input": flag, "slurp": flag, "compact-output": flag, "tab": flag,
				"color-output": flag, "monochrome-output": flag, "ascii-output": flag, "unbuffered": flag,
				"sort-keys": flag, "raw-output": flag, "raw-output0": flag, "join-output": flag, "exit-status": flag,
				"seq": flag, "stream": flag, "stream-errors": flag, "args": flag, "jsonargs": flag, "help": flag,
				"version": flag, "build-configuration": flag, "binary": flag,
			},
			permute: true,
		},
	},
}

func (l Language) String() string { return languages[l].name }

var commandLanguages = func() map[string]Language {
	out := map[string]Language{}
	for l := range Language(len(languages)) {
		for _, name := range languages[l].commands {
			out[name] = l
		}
	}
	return out
}()

func languageOf(command string) (Language, bool) {
	l, ok := commandLanguages[path.Base(command)]
	return l, ok
}
