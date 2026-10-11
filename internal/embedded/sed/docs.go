package sed

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
)

var commands = lang.Docs{
	"s": {"[addr]s/regexp/replacement/[flags]", "Replaces the text matching `regexp` in the pattern space with `replacement`, in which `&` stands for the whole match and `\\1`…`\\9` for its groups. Only the first match is replaced unless a flag says otherwise."},
	"y": {"[addr]y/source/dest/", "Replaces each character of the pattern space that occurs in `source` with the character at the same position in `dest`. Both lists must be equally long."},
	"p": {"[addr]p", "Prints the pattern space."},
	"d": {"[addr]d", "Deletes the pattern space and starts the next cycle without printing it."},
	"D": {"[addr]D", "Deletes the pattern space up to its first newline and restarts the cycle with the rest, without reading a new line; behaves like `d` when there is no newline."},
	"a": {"[addr]a text", "Queues `text` to be printed after the current line, at the end of the cycle or when the next line is read."},
	"i": {"[addr]i text", "Prints `text` right away, before the current line."},
	"c": {"[addr]c text", "Deletes the pattern space and prints `text` in its place; over a range, `text` is printed once at the end of the range."},
	"n": {"[addr]n", "Prints the pattern space unless `-n` is given and replaces it with the next line of input. Without more input sed exits without running the rest of the script."},
	"N": {"[addr]N", "Appends a newline and the next line of input to the pattern space. Without more input GNU sed prints the pattern space and exits."},
	"P": {"[addr]P", "Prints the pattern space up to its first newline."},
	"h": {"[addr]h", "Copies the pattern space into the hold space, replacing what it held."},
	"H": {"[addr]H", "Appends a newline and the pattern space to the hold space."},
	"g": {"[addr]g", "Copies the hold space into the pattern space, replacing what it held."},
	"G": {"[addr]G", "Appends a newline and the hold space to the pattern space."},
	"x": {"[addr]x", "Exchanges the pattern space and the hold space."},
	"b": {"[addr]b [label]", "Branches to `label`, or to the end of the script when no label is given."},
	"t": {"[addr]t [label]", "Branches to `label`, or to the end of the script, if an `s` command has replaced something since the last input line was read or the last `t` or `T` branch was taken."},
	"T": {"[addr]T [label]", "GNU: branches to `label`, or to the end of the script, if no `s` command has replaced anything since the last input line was read or the last `t` or `T` branch was taken."},
	"q": {"[addr]q [exit-code]", "Prints the pattern space unless `-n` is given and exits, with `exit-code` as the status in GNU sed."},
	"Q": {"[addr]Q [exit-code]", "GNU: exits right away without printing the pattern space."},
	"r": {"[addr]r file", "Queues the contents of `file` to be printed at the end of the cycle; a file that cannot be read is ignored."},
	"R": {"[addr]R file", "GNU: queues the next line of `file` to be printed at the end of the cycle."},
	"w": {"[addr]w file", "Writes the pattern space to `file`, which is created or emptied when sed starts."},
	"W": {"[addr]W file", "GNU: writes the pattern space up to its first newline to `file`."},
	"e": {"[addr]e [command]", "GNU: runs `command` and prints its output before the end of the cycle; without a command, runs the pattern space as a command and replaces it with the output."},
	"F": {"[addr]F", "GNU: prints the name of the current input file, `-` for standard input."},
	"z": {"[addr]z", "GNU: empties the pattern space."},
	"l": {"[addr]l [width]", "Prints the pattern space unambiguously: unprintable characters as escapes, long lines wrapped at `width` and the end marked with `$`."},
	"=": {"[addr]=", "Prints the current input line number and a newline."},
	":": {":label", "Marks a position in the script that `b`, `t` and `T` can branch to."},
	"v": {"v [version]", "GNU: fails unless sed supports GNU extensions, at least of `version`; does nothing otherwise."},
}

var options = lang.Docs{
	"g": {"s/regexp/replacement/g", "Replaces every match that does not overlap an earlier one, instead of only the first."},
	"p": {"s/regexp/replacement/p", "Prints the pattern space when a replacement was made."},
	"w": {"s/regexp/replacement/w file", "Writes the pattern space to `file` when a replacement was made."},
	"e": {"s/regexp/replacement/e", "GNU: runs the pattern space as a shell command after the replacement and replaces it with the command's output."},
	"i": {"s/regexp/replacement/i", "GNU: matches `regexp` regardless of case."},
	"I": {"/regexp/I", "GNU: matches `regexp` regardless of case."},
	"m": {"s/regexp/replacement/m", "GNU: multi-line mode: `^` and `$` also match next to newlines in the pattern space."},
	"M": {"/regexp/M", "GNU: multi-line mode: `^` and `$` also match next to newlines in the pattern space."},
}

var nthMatch = lang.Doc{Signature: "s/regexp/replacement/N", Summary: "Replaces only the Nth match; together with `g`, the Nth and every later match."}

func Doc(name string, mods lang.Modifier) (lang.Doc, bool) {
	if mods&lang.Option == 0 {
		return commands.Lookup(name)
	}
	if name != "" && strings.Trim(name, "0123456789") == "" {
		return nthMatch, true
	}
	return options.Lookup(name)
}
