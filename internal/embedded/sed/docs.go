package sed

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
)

var commands = lang.Docs{
	"s": {Signature: "[addr]s/regexp/replacement/[flags]", Summary: "Replaces the text matching `regexp` in the pattern space with `replacement`, in which `&` stands for the whole match and `\\1`…`\\9` for its groups. Only the first match is replaced unless a flag says otherwise."},
	"y": {Signature: "[addr]y/source/dest/", Summary: "Replaces each character of the pattern space that occurs in `source` with the character at the same position in `dest`. Both lists must be equally long."},
	"p": {Signature: "[addr]p", Summary: "Prints the pattern space."},
	"d": {Signature: "[addr]d", Summary: "Deletes the pattern space and starts the next cycle without printing it."},
	"D": {Signature: "[addr]D", Summary: "Deletes the pattern space up to its first newline and restarts the cycle with the rest, without reading a new line; behaves like `d` when there is no newline."},
	"a": {Signature: "[addr]a text", Summary: "Queues `text` to be printed after the current line, at the end of the cycle or when the next line is read."},
	"i": {Signature: "[addr]i text", Summary: "Prints `text` right away, before the current line."},
	"c": {Signature: "[addr]c text", Summary: "Deletes the pattern space and prints `text` in its place; over a range, `text` is printed once at the end of the range."},
	"n": {Signature: "[addr]n", Summary: "Prints the pattern space unless `-n` is given and replaces it with the next line of input. Without more input sed exits without running the rest of the script."},
	"N": {Signature: "[addr]N", Summary: "Appends a newline and the next line of input to the pattern space. Without more input GNU sed prints the pattern space and exits."},
	"P": {Signature: "[addr]P", Summary: "Prints the pattern space up to its first newline."},
	"h": {Signature: "[addr]h", Summary: "Copies the pattern space into the hold space, replacing what it held."},
	"H": {Signature: "[addr]H", Summary: "Appends a newline and the pattern space to the hold space."},
	"g": {Signature: "[addr]g", Summary: "Copies the hold space into the pattern space, replacing what it held."},
	"G": {Signature: "[addr]G", Summary: "Appends a newline and the hold space to the pattern space."},
	"x": {Signature: "[addr]x", Summary: "Exchanges the pattern space and the hold space."},
	"b": {Signature: "[addr]b [label]", Summary: "Branches to `label`, or to the end of the script when no label is given."},
	"t": {Signature: "[addr]t [label]", Summary: "Branches to `label`, or to the end of the script, if an `s` command has replaced something since the last input line was read or the last `t` or `T` branch was taken."},
	"T": {Signature: "[addr]T [label]", Summary: "GNU: branches to `label`, or to the end of the script, if no `s` command has replaced anything since the last input line was read or the last `t` or `T` branch was taken."},
	"q": {Signature: "[addr]q [exit-code]", Summary: "Prints the pattern space unless `-n` is given and exits, with `exit-code` as the status in GNU sed."},
	"Q": {Signature: "[addr]Q [exit-code]", Summary: "GNU: exits right away without printing the pattern space."},
	"r": {Signature: "[addr]r file", Summary: "Queues the contents of `file` to be printed at the end of the cycle; a file that cannot be read is ignored."},
	"R": {Signature: "[addr]R file", Summary: "GNU: queues the next line of `file` to be printed at the end of the cycle."},
	"w": {Signature: "[addr]w file", Summary: "Writes the pattern space to `file`, which is created or emptied when sed starts."},
	"W": {Signature: "[addr]W file", Summary: "GNU: writes the pattern space up to its first newline to `file`."},
	"e": {Signature: "[addr]e [command]", Summary: "GNU: runs `command` and prints its output before the end of the cycle; without a command, runs the pattern space as a command and replaces it with the output."},
	"F": {Signature: "[addr]F", Summary: "GNU: prints the name of the current input file, `-` for standard input."},
	"z": {Signature: "[addr]z", Summary: "GNU: empties the pattern space."},
	"l": {Signature: "[addr]l [width]", Summary: "Prints the pattern space unambiguously: unprintable characters as escapes, long lines wrapped at `width` and the end marked with `$`."},
	"=": {Signature: "[addr]=", Summary: "Prints the current input line number and a newline."},
	":": {Signature: ":label", Summary: "Marks a position in the script that `b`, `t` and `T` can branch to."},
	"v": {Signature: "v [version]", Summary: "GNU: fails unless sed supports GNU extensions, at least of `version`; does nothing otherwise."},
}

var options = lang.Docs{
	"g": {Signature: "s/regexp/replacement/g", Summary: "Replaces every match that does not overlap an earlier one, instead of only the first."},
	"p": {Signature: "s/regexp/replacement/p", Summary: "Prints the pattern space when a replacement was made."},
	"w": {Signature: "s/regexp/replacement/w file", Summary: "Writes the pattern space to `file` when a replacement was made."},
	"e": {Signature: "s/regexp/replacement/e", Summary: "GNU: runs the pattern space as a shell command after the replacement and replaces it with the command's output."},
	"i": {Signature: "s/regexp/replacement/i", Summary: "GNU: matches `regexp` regardless of case."},
	"I": {Signature: "/regexp/I", Summary: "GNU: matches `regexp` regardless of case."},
	"m": {Signature: "s/regexp/replacement/m", Summary: "GNU: multi-line mode: `^` and `$` also match next to newlines in the pattern space."},
	"M": {Signature: "/regexp/M", Summary: "GNU: multi-line mode: `^` and `$` also match next to newlines in the pattern space."},
}

var nthMatch = lang.Doc{Signature: "s/regexp/replacement/N", Summary: "Replaces only the Nth match; together with `g`, the Nth and every later match."}

func Doc(name string, tok lang.Token) (lang.Doc, bool) {
	table := options
	switch {
	case tok.Modifiers&lang.Option == 0:
		table = commands
	case name != "" && strings.Trim(name, "0123456789") == "":
		return nthMatch, true
	}
	doc, ok := table[name]
	return doc, ok
}
