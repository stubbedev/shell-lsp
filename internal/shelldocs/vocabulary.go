package shelldocs

import (
	"fmt"
	"iter"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

type Vocabulary uint8

const (
	SetOptions Vocabulary = iota
	ShoptOptions
	SetoptOptions
	EmulateModes
	Signals
	TrapConditions
	TestUnaryOperators
	TestBinaryOperators
	CondOperators
	ExpansionOperators
	ExpansionForms
	ZshParameterFlags
	ZshModifiers
	ZshGlobQualifiers
	ZshGlobFlags
	PrintfDirectives
	Redirections
	ArithmeticOperators
	ListOperators
	OptionLetters
	vocabularyCount
)

var vocabularies = [vocabularyCount][]Entry{
	SetOptions:          setOptions,
	ShoptOptions:        shoptOptions,
	SetoptOptions:       setoptOptions,
	EmulateModes:        emulateModes,
	Signals:             signals,
	TrapConditions:      trapConditions,
	TestUnaryOperators:  testUnaryOperators,
	TestBinaryOperators: testBinaryOperators,
	CondOperators:       condOperators,
	ExpansionOperators:  expansionOperators,
	ExpansionForms:      expansionForms,
	ZshParameterFlags:   zshParameterFlags,
	ZshModifiers:        zshModifiers,
	ZshGlobQualifiers:   zshGlobQualifiers,
	ZshGlobFlags:        zshGlobFlags,
	PrintfDirectives:    printfDirectives,
	Redirections:        redirections,
	ArithmeticOperators: arithmeticOperators,
	ListOperators:       listOperators,
	OptionLetters:       optionLetters,
}

var vocabularyIndex = func() [vocabularyCount]map[string][]Entry {
	var out [vocabularyCount]map[string][]Entry
	for v, entries := range vocabularies {
		byName := make(map[string][]Entry, len(entries))
		for _, e := range entries {
			for _, other := range byName[e.Name] {
				if other.shells&e.shells != 0 {
					panic(fmt.Sprintf("shelldocs: vocabulary %d describes %s twice for one shell", v, e.Name))
				}
			}
			byName[e.Name] = append(byName[e.Name], e)
		}
		out[v] = byName
	}
	return out
}()

func Terms(v Vocabulary, s dialect.Shell) iter.Seq[Entry] {
	return func(yield func(Entry) bool) {
		for _, e := range vocabularies[v] {
			if e.In(s) && !yield(e) {
				return
			}
		}
	}
}

func known(v Vocabulary, name string) bool {
	_, ok := vocabularyIndex[v][name]
	return ok
}

func Term(v Vocabulary, name string, s dialect.Shell) (Entry, bool) {
	entries := vocabularyIndex[v][name]
	if i := slices.IndexFunc(entries, func(e Entry) bool { return e.In(s) }); i >= 0 {
		return entries[i], true
	}
	if i := slices.IndexFunc(entries, func(e Entry) bool { return e.inDialect(s.Dialect) }); i >= 0 {
		return entries[i], true
	}
	if len(entries) == 0 {
		return Entry{}, false
	}
	return entries[0], true
}

func ZshModifier(modifier string) (Entry, bool) {
	key := strings.TrimRight(modifier, "0123456789")
	switch {
	case strings.HasPrefix(key, "gs"):
		key = "gs"
	case key != "":
		key = key[:1]
	}
	return Term(ZshModifiers, key, dialect.Zsh.Shell())
}

var (
	notPosix = dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Dash, dialect.Ksh, dialect.Busybox).Shells()
	bashKsh  = dialect.SetOf(dialect.Bash, dialect.Ksh).Shells()
	extended = dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Ksh, dialect.Busybox).Shells()
)

var emulateModes = []Entry{
	{Name: "zsh", shells: zshOnly, Doc: "Restore native zsh option settings."},
	{Name: "sh", shells: zshOnly, Doc: "Set options so that zsh behaves like a POSIX shell: word splitting of unquoted expansions, no `**` recursion, arrays indexed from 0 where relevant."},
	{Name: "ksh", shells: zshOnly, Doc: "Set options so that zsh behaves like ksh: arrays indexed from 0, `ksh_glob` patterns, word splitting of unquoted expansions."},
	{Name: "csh", shells: zshOnly, Doc: "Set options so that zsh behaves like csh as far as it can."},
	{Name: "bash", shells: zshOnly, Doc: "Accepted as a synonym for `sh` emulation; zsh has no separate bash mode."},
}

var trapConditions = []Entry{
	{Name: "EXIT", shells: every, Doc: "Runs the action when the shell exits, however it exits. `0` is a synonym."},
	{Name: "ERR", shells: bashKsh, Doc: "Runs the action whenever a command fails, under the same rules that make `set -e` exit."},
	{Name: "ZERR", shells: zshOnly, Doc: "Runs the action whenever a command fails, under the same rules that make `setopt err_exit` exit. `ERR` is accepted as an alias."},
	{Name: "DEBUG", shells: bashZshKsh, Doc: "Runs the action before every simple command (after it in zsh), which is how debuggers and tracing helpers hook in."},
	{Name: "RETURN", shells: bashOnly, Doc: "Runs the action when a function or a sourced file returns. Functions only inherit it with `set -T` or the `functrace` option."},
	{Name: "KEYBD", shells: kshOnly, Doc: "Runs the action for every key read by the line editor; `.sh.edchar` holds the key."},
}

var signals = []Entry{
	{Name: "HUP", shells: every, Doc: "Hangup (1): the controlling terminal went away. Daemons often use it to reload their configuration."},
	{Name: "INT", shells: every, Doc: "Interrupt (2): sent by Ctrl-C. Trapping it lets a script clean up before stopping."},
	{Name: "QUIT", shells: every, Doc: "Quit (3): sent by Ctrl-\\. Terminates and dumps core by default."},
	{Name: "ILL", shells: every, Doc: "Illegal instruction (4)."},
	{Name: "TRAP", shells: every, Doc: "Trace or breakpoint trap (5)."},
	{Name: "ABRT", shells: every, Doc: "Abort (6): raised by `abort()`; terminates and dumps core by default."},
	{Name: "BUS", shells: every, Doc: "Bus error: a bad memory access."},
	{Name: "FPE", shells: every, Doc: "Arithmetic exception (8), such as an integer division by zero."},
	{Name: "KILL", shells: every, Doc: "Kill (9): ends the process immediately. It cannot be caught, blocked or trapped."},
	{Name: "USR1", shells: every, Doc: "User-defined signal 1, with no meaning to the system."},
	{Name: "SEGV", shells: every, Doc: "Segmentation fault (11): an invalid memory reference."},
	{Name: "USR2", shells: every, Doc: "User-defined signal 2, with no meaning to the system."},
	{Name: "PIPE", shells: every, Doc: "Broken pipe (13): a write to a pipe whose reader has exited. Commands in a pipeline usually die of it quietly."},
	{Name: "ALRM", shells: every, Doc: "Alarm clock (14): a timer set with `alarm()` expired."},
	{Name: "TERM", shells: every, Doc: "Termination (15): the polite request to stop, and the default signal of `kill`."},
	{Name: "CHLD", shells: every, Doc: "A child process stopped or exited. Ignored by default."},
	{Name: "CONT", shells: every, Doc: "Continue a stopped process."},
	{Name: "STOP", shells: every, Doc: "Stop the process. It cannot be caught, blocked or trapped."},
	{Name: "TSTP", shells: every, Doc: "Terminal stop: sent by Ctrl-Z."},
	{Name: "TTIN", shells: every, Doc: "A background process tried to read from the terminal."},
	{Name: "TTOU", shells: every, Doc: "A background process tried to write to the terminal while `tostop` is set."},
	{Name: "URG", shells: every, Doc: "Urgent data arrived on a socket."},
	{Name: "XCPU", shells: every, Doc: "The CPU time limit set with `ulimit -t` was exceeded."},
	{Name: "XFSZ", shells: every, Doc: "The file size limit set with `ulimit -f` was exceeded."},
	{Name: "VTALRM", shells: every, Doc: "A virtual (user CPU time) timer expired."},
	{Name: "PROF", shells: every, Doc: "A profiling timer expired."},
	{Name: "WINCH", shells: every, Doc: "The terminal window changed size."},
	{Name: "IO", shells: every, Doc: "Input or output became possible on a descriptor set up for asynchronous I/O."},
	{Name: "SYS", shells: every, Doc: "A bad system call argument."},
}

var testUnaryOperators = []Entry{
	{Name: "-e", shells: every, Synopsis: "-e file", Doc: "True if `file` exists, whatever its type."},
	{Name: "-f", shells: every, Synopsis: "-f file", Doc: "True if `file` exists and is a regular file (symbolic links are followed)."},
	{Name: "-d", shells: every, Synopsis: "-d file", Doc: "True if `file` exists and is a directory."},
	{Name: "-s", shells: every, Synopsis: "-s file", Doc: "True if `file` exists and is not empty."},
	{Name: "-r", shells: every, Synopsis: "-r file", Doc: "True if `file` exists and the shell may read it."},
	{Name: "-w", shells: every, Synopsis: "-w file", Doc: "True if `file` exists and the shell may write to it."},
	{Name: "-x", shells: every, Synopsis: "-x file", Doc: "True if `file` exists and the shell may execute it (search it, for a directory)."},
	{Name: "-L", shells: every, Synopsis: "-L file", Doc: "True if `file` is a symbolic link, dangling or not."},
	{Name: "-h", shells: every, Synopsis: "-h file", Doc: "Same as `-L`: true if `file` is a symbolic link."},
	{Name: "-b", shells: every, Synopsis: "-b file", Doc: "True if `file` is a block device."},
	{Name: "-c", shells: every, Synopsis: "-c file", Doc: "True if `file` is a character device."},
	{Name: "-p", shells: every, Synopsis: "-p file", Doc: "True if `file` is a named pipe (FIFO)."},
	{Name: "-S", shells: every, Synopsis: "-S file", Doc: "True if `file` is a socket."},
	{Name: "-g", shells: every, Synopsis: "-g file", Doc: "True if `file` has its set-group-ID bit set."},
	{Name: "-u", shells: every, Synopsis: "-u file", Doc: "True if `file` has its set-user-ID bit set."},
	{Name: "-k", shells: notPosix, Synopsis: "-k file", Doc: "True if `file` has its sticky bit set."},
	{Name: "-O", shells: notPosix, Synopsis: "-O file", Doc: "True if `file` is owned by the effective user of the shell."},
	{Name: "-G", shells: notPosix, Synopsis: "-G file", Doc: "True if `file` belongs to the effective group of the shell."},
	{Name: "-N", shells: bashZsh, Synopsis: "-N file", Doc: "True if `file` was modified after it was last read."},
	{Name: "-t", shells: every, Synopsis: "-t fd", Doc: "True if descriptor `fd` is open on a terminal; `[ -t 1 ]` asks whether output goes to a terminal."},
	{Name: "-z", shells: every, Synopsis: "-z string", Doc: "True if `string` is empty."},
	{Name: "-n", shells: every, Synopsis: "-n string", Doc: "True if `string` is not empty. Quote the operand: an unquoted empty expansion leaves `[ -n ]`, which is true."},
	{Name: "-v", shells: bashZshKsh, Synopsis: "-v name", Doc: "True if the variable `name` is set, even to an empty value. Takes a name, not `$name`."},
	{Name: "-R", shells: bashOnly, Synopsis: "-R name", Doc: "True if `name` is set and is a name reference."},
	{Name: "-o", shells: bashZshKsh, Synopsis: "-o option", Doc: "Inside `[[ ]]`, true if the shell option `option` is enabled. Between two expressions in `[ ]`, `-o` is a logical or instead."},
}

var testBinaryOperators = []Entry{
	{Name: "=", shells: every, Synopsis: "string1 = string2", Doc: "True if the strings are equal. Inside `[[ ]]` an unquoted right-hand side is a glob pattern."},
	{Name: "==", shells: extended, Synopsis: "string1 == string2", Doc: "Same as `=`. Not portable to POSIX `[`; inside `[[ ]]` an unquoted right-hand side is a glob pattern."},
	{Name: "!=", shells: every, Synopsis: "string1 != string2", Doc: "True if the strings differ. Inside `[[ ]]` an unquoted right-hand side is a glob pattern."},
	{Name: "-eq", shells: every, Synopsis: "n1 -eq n2", Doc: "True if the integers are equal."},
	{Name: "-ne", shells: every, Synopsis: "n1 -ne n2", Doc: "True if the integers differ."},
	{Name: "-lt", shells: every, Synopsis: "n1 -lt n2", Doc: "True if `n1` is less than `n2`."},
	{Name: "-le", shells: every, Synopsis: "n1 -le n2", Doc: "True if `n1` is less than or equal to `n2`."},
	{Name: "-gt", shells: every, Synopsis: "n1 -gt n2", Doc: "True if `n1` is greater than `n2`."},
	{Name: "-ge", shells: every, Synopsis: "n1 -ge n2", Doc: "True if `n1` is greater than or equal to `n2`."},
	{Name: "-nt", shells: every, Synopsis: "file1 -nt file2", Doc: "True if `file1` was modified more recently than `file2`, or if only `file1` exists."},
	{Name: "-ot", shells: every, Synopsis: "file1 -ot file2", Doc: "True if `file1` was modified before `file2`, or if only `file2` exists."},
	{Name: "-ef", shells: every, Synopsis: "file1 -ef file2", Doc: "True if both names refer to the same file (same device and inode)."},
}

var condOperators = []Entry{
	{Name: "=~", shells: bashZshKsh, Synopsis: "string =~ regex", Doc: "True if `string` matches the extended regular expression. Quoted parts of `regex` match literally; keep the pattern in a variable to avoid quoting surprises. Captures land in `BASH_REMATCH` (bash) or `match` (zsh)."},
	{Name: "<", shells: bashZshKsh, Synopsis: "string1 < string2", Doc: "True if `string1` sorts before `string2` in the current locale. Inside `[ ]` it would be a redirection and must be escaped as `\\<`."},
	{Name: ">", shells: bashZshKsh, Synopsis: "string1 > string2", Doc: "True if `string1` sorts after `string2` in the current locale. Inside `[ ]` it would be a redirection and must be escaped as `\\>`."},
	{Name: "&&", shells: bashZshKsh, Synopsis: "expr1 && expr2", Doc: "True if both expressions are true; `expr2` is only evaluated when `expr1` is true."},
	{Name: "||", shells: bashZshKsh, Synopsis: "expr1 || expr2", Doc: "True if either expression is true; `expr2` is only evaluated when `expr1` is false."},
}

var expansionOperators = []Entry{
	{Name: ":-", shells: every, Synopsis: "${name:-word}", Doc: "Expands to `word` when `name` is unset or empty, otherwise to its value."},
	{Name: "-", shells: every, Synopsis: "${name-word}", Doc: "Expands to `word` when `name` is unset, otherwise to its value, even an empty one."},
	{Name: ":=", shells: every, Synopsis: "${name:=word}", Doc: "Assigns `word` to `name` when it is unset or empty, then expands to the value."},
	{Name: "=", shells: every, Synopsis: "${name=word}", Doc: "Assigns `word` to `name` when it is unset, then expands to the value."},
	{Name: ":?", shells: every, Synopsis: "${name:?message}", Doc: "Fails with `message` when `name` is unset or empty; a non-interactive shell exits."},
	{Name: "?", shells: every, Synopsis: "${name?message}", Doc: "Fails with `message` when `name` is unset; a non-interactive shell exits."},
	{Name: ":+", shells: every, Synopsis: "${name:+word}", Doc: "Expands to `word` when `name` is set and not empty, otherwise to nothing."},
	{Name: "+", shells: every, Synopsis: "${name+word}", Doc: "Expands to `word` when `name` is set, even to an empty value, otherwise to nothing."},
	{Name: "#", shells: every, Synopsis: "${name#pattern}", Doc: "Removes the shortest prefix matching the glob `pattern`."},
	{Name: "##", shells: every, Synopsis: "${name##pattern}", Doc: "Removes the longest prefix matching the glob `pattern`; `${path##*/}` keeps the last path component."},
	{Name: "%", shells: every, Synopsis: "${name%pattern}", Doc: "Removes the shortest suffix matching the glob `pattern`; `${file%.*}` drops one extension."},
	{Name: "%%", shells: every, Synopsis: "${name%%pattern}", Doc: "Removes the longest suffix matching the glob `pattern`."},
	{Name: "/", shells: extended, Synopsis: "${name/pattern/string}", Doc: "Replaces the first match of `pattern` with `string`."},
	{Name: "//", shells: extended, Synopsis: "${name//pattern/string}", Doc: "Replaces every match of `pattern` with `string`."},
	{Name: "/#", shells: extended, Synopsis: "${name/#pattern/string}", Doc: "Replaces `pattern` only where it matches at the start of the value."},
	{Name: "/%", shells: extended, Synopsis: "${name/%pattern/string}", Doc: "Replaces `pattern` only where it matches at the end of the value."},
	{Name: ":", shells: extended, Synopsis: "${name:offset:length}", Doc: "Substring starting at the zero-based `offset`, at most `length` characters long; for `@` and arrays, a range of elements. A negative offset needs a space or parentheses: `${name: -2}`."},
	{Name: "^", shells: bashOnly, Synopsis: "${name^pattern}", Doc: "Uppercases the first character when it matches `pattern` (any character when omitted)."},
	{Name: "^^", shells: bashOnly, Synopsis: "${name^^pattern}", Doc: "Uppercases every character matching `pattern` (all of them when omitted)."},
	{Name: ",", shells: bashOnly, Synopsis: "${name,pattern}", Doc: "Lowercases the first character when it matches `pattern` (any character when omitted)."},
	{Name: ",,", shells: bashOnly, Synopsis: "${name,,pattern}", Doc: "Lowercases every character matching `pattern` (all of them when omitted)."},
	{Name: "@Q", shells: bashOnly, Synopsis: "${name@Q}", Doc: "Quotes the value so that it can be read back as shell input."},
	{Name: "@E", shells: bashOnly, Synopsis: "${name@E}", Doc: "Expands backslash escape sequences in the value, as `$'...'` would."},
	{Name: "@P", shells: bashOnly, Synopsis: "${name@P}", Doc: "Expands the value as if it were a prompt string such as `PS1`."},
	{Name: "@A", shells: bashOnly, Synopsis: "${name@A}", Doc: "Produces an assignment statement (with `declare` and attributes) that recreates the variable."},
	{Name: "@K", shells: bashOnly, Synopsis: "${name@K}", Doc: "Like `@Q`, but an associative or indexed array becomes quoted key-value pairs."},
	{Name: "@k", shells: bashOnly, Synopsis: "${name[@]@k}", Doc: "Like `@K`, but each key and value becomes a separate word."},
	{Name: "@a", shells: bashOnly, Synopsis: "${name@a}", Doc: "Expands to the attribute flags of the variable, such as `a`, `A`, `x` or `r`."},
	{Name: "@U", shells: bashOnly, Synopsis: "${name@U}", Doc: "Uppercases the whole value."},
	{Name: "@u", shells: bashOnly, Synopsis: "${name@u}", Doc: "Uppercases the first character of the value."},
	{Name: "@L", shells: bashOnly, Synopsis: "${name@L}", Doc: "Lowercases the whole value."},
	{Name: ":#", shells: zshOnly, Synopsis: "${name:#pattern}", Doc: "Removes the array elements matching `pattern`; for a scalar, the whole value if it matches."},
	{Name: ":|", shells: zshOnly, Synopsis: "${name:|array}", Doc: "Removes the elements that also appear in `array` (set difference)."},
	{Name: ":*", shells: zshOnly, Synopsis: "${name:*array}", Doc: "Keeps only the elements that also appear in `array` (set intersection)."},
	{Name: ":^", shells: zshOnly, Synopsis: "${name:^array}", Doc: "Zips the two arrays, alternating their elements and stopping at the end of the shorter one."},
	{Name: ":^^", shells: zshOnly, Synopsis: "${name:^^array}", Doc: "Zips the two arrays, repeating the shorter one until the longer one is used up."},
	{Name: "::=", shells: zshOnly, Synopsis: "${name::=word}", Doc: "Assigns `word` to `name` unconditionally, then expands to it."},
}

var expansionForms = []Entry{
	{Name: "length", shells: every, Synopsis: "${#name}", Doc: "Length of the value in characters; for `${#array[@]}`, the number of elements."},
	{Name: "indirect", shells: bashOnly, Synopsis: "${!name}", Doc: "Expands the variable whose name is the value of `name` (indirection)."},
	{Name: "keys", shells: bashKsh, Synopsis: "${!name[@]}", Doc: "Expands to the indices or keys of the array."},
	{Name: "names", shells: bashKsh, Synopsis: "${!prefix*}", Doc: "Expands to the names of every variable starting with `prefix`; `${!prefix@}` keeps them as separate words."},
	{Name: "width", shells: kshOnly, Synopsis: "${%name}", Doc: "Display width of the value in terminal columns (mksh)."},
	{Name: "isset", shells: zshOnly, Synopsis: "${+name}", Doc: "Expands to 1 if `name` is set, otherwise to 0."},
}

var zshParameterFlags = []Entry{
	{Name: "@", shells: zshOnly, Doc: "Keep array elements as separate words inside double quotes."},
	{Name: "A", shells: zshOnly, Doc: "Create an array parameter with `${...=...}`; `AA` creates an associative array."},
	{Name: "a", shells: zshOnly, Doc: "Sort in array index order (reversed with `O`)."},
	{Name: "c", shells: zshOnly, Doc: "With `${#...}`, count the characters of the joined words."},
	{Name: "C", shells: zshOnly, Doc: "Capitalise the first letter of each word."},
	{Name: "e", shells: zshOnly, Doc: "Perform parameter, command and arithmetic substitution on the result."},
	{Name: "f", shells: zshOnly, Doc: "Split the result into lines, like `(ps:\\n:)`."},
	{Name: "F", shells: zshOnly, Doc: "Join array elements with newlines, like `(pj:\\n:)`."},
	{Name: "i", shells: zshOnly, Doc: "Sort case-insensitively."},
	{Name: "j", shells: zshOnly, Doc: "Join array elements with the separator that follows, as in `(j:,:)`."},
	{Name: "k", shells: zshOnly, Doc: "Substitute the keys of an associative array."},
	{Name: "L", shells: zshOnly, Doc: "Convert to lowercase."},
	{Name: "n", shells: zshOnly, Doc: "Sort numerically."},
	{Name: "o", shells: zshOnly, Doc: "Sort in ascending order."},
	{Name: "O", shells: zshOnly, Doc: "Sort in descending order."},
	{Name: "P", shells: zshOnly, Doc: "Use the value as the name of the parameter to expand (indirection)."},
	{Name: "q", shells: zshOnly, Doc: "Quote special characters with backslashes; `qq` single-quotes, `qqq` double-quotes, `q-` quotes minimally."},
	{Name: "Q", shells: zshOnly, Doc: "Remove one level of quoting."},
	{Name: "s", shells: zshOnly, Doc: "Split on the separator that follows, as in `(s:/:)`."},
	{Name: "t", shells: zshOnly, Doc: "Substitute the type of the parameter, such as `scalar`, `array` or `association-local`."},
	{Name: "u", shells: zshOnly, Doc: "Keep only the first occurrence of each word."},
	{Name: "U", shells: zshOnly, Doc: "Convert to uppercase."},
	{Name: "v", shells: zshOnly, Doc: "Substitute the values of an associative array; with `k`, keys and values alternate."},
	{Name: "V", shells: zshOnly, Doc: "Make special characters visible."},
	{Name: "w", shells: zshOnly, Doc: "With `${#...}`, count words."},
	{Name: "z", shells: zshOnly, Doc: "Split into words the way the shell parser would."},
	{Name: "0", shells: zshOnly, Doc: "Split on NUL bytes, like `(ps:\\0:)`."},
	{Name: "%", shells: zshOnly, Doc: "Expand prompt escape sequences."},
	{Name: "~", shells: zshOnly, Doc: "Treat the separator of `j` and `s` as a pattern."},
	{Name: "M", shells: zshOnly, Doc: "With `:#`, keep the elements that match instead of removing them; with `#`, `%` and `/`, include the matched part in the result."},
	{Name: "S", shells: zshOnly, Doc: "With `#`, `%` and `/`, match substrings anywhere in the value, not only at its start or end."},
	{Name: "B", shells: zshOnly, Doc: "With `#`, `%` and `/`, include the index where the match begins in the result."},
	{Name: "E", shells: zshOnly, Doc: "With `#`, `%` and `/`, include the index where the match ends in the result."},
	{Name: "N", shells: zshOnly, Doc: "With `#`, `%` and `/`, include the length of the match in the result."},
	{Name: "I", shells: zshOnly, Doc: "With `#`, `%` and `/`, use the given match, as in `(I:2:)` for the second one."},
	{Name: "g", shells: zshOnly, Doc: "Process escape sequences like `print` and `echo` do; the argument picks the style, as in `(g::)`."},
	{Name: "l", shells: zshOnly, Doc: "Pad on the left to the given width, as in `(l:10::0:)`."},
	{Name: "r", shells: zshOnly, Doc: "Pad on the right to the given width, as in `(r:10:)`."},
	{Name: "Z", shells: zshOnly, Doc: "Split like `z`, with options for comments and newlines, as in `(Z:C:)`."},
	{Name: "D", shells: zshOnly, Doc: "Shorten directory names with `~` and named directories."},
	{Name: "_", shells: zshOnly, Doc: "Reserved for future use; takes a delimited argument."},
	{Name: "#", shells: zshOnly, Doc: "Evaluate the value as an arithmetic expression and output the character with that code."},
	{Name: "x", shells: zshOnly, Doc: "With `q`, fail when a value cannot be quoted safely."},
}

var zshModifiers = []Entry{
	{Name: "h", shells: zshOnly, Doc: "Head: remove the last path component, like `dirname`."},
	{Name: "t", shells: zshOnly, Doc: "Tail: keep only the last path component, like `basename`."},
	{Name: "r", shells: zshOnly, Doc: "Root: remove the file extension."},
	{Name: "e", shells: zshOnly, Doc: "Extension: keep only the file extension."},
	{Name: "a", shells: zshOnly, Doc: "Make the path absolute."},
	{Name: "A", shells: zshOnly, Doc: "Make the path absolute and resolve symbolic links."},
	{Name: "c", shells: zshOnly, Doc: "Resolve a command name to its path."},
	{Name: "l", shells: zshOnly, Doc: "Convert to lowercase."},
	{Name: "u", shells: zshOnly, Doc: "Convert to uppercase."},
	{Name: "q", shells: zshOnly, Doc: "Quote the result."},
	{Name: "Q", shells: zshOnly, Doc: "Remove one level of quoting."},
	{Name: "P", shells: zshOnly, Doc: "Make the path absolute and physical, without symbolic links or `..`."},
	{Name: "s", shells: zshOnly, Doc: "Substitute once, as in `:s/old/new/`."},
	{Name: "gs", shells: zshOnly, Doc: "Substitute every occurrence, as in `:gs/old/new/`."},
}

var zshGlobQualifiers = []Entry{
	{Name: "/", shells: zshOnly, Doc: "Directories."},
	{Name: ".", shells: zshOnly, Doc: "Regular files."},
	{Name: "@", shells: zshOnly, Doc: "Symbolic links."},
	{Name: "=", shells: zshOnly, Doc: "Sockets."},
	{Name: "p", shells: zshOnly, Doc: "Named pipes (FIFOs)."},
	{Name: "*", shells: zshOnly, Doc: "Executable regular files."},
	{Name: "%", shells: zshOnly, Doc: "Device files; `%b` block and `%c` character devices."},
	{Name: "r", shells: zshOnly, Doc: "Files readable by their owner."},
	{Name: "w", shells: zshOnly, Doc: "Files writable by their owner."},
	{Name: "x", shells: zshOnly, Doc: "Files executable by their owner."},
	{Name: "R", shells: zshOnly, Doc: "Files readable by everyone."},
	{Name: "W", shells: zshOnly, Doc: "Files writable by everyone."},
	{Name: "X", shells: zshOnly, Doc: "Files executable by everyone."},
	{Name: "U", shells: zshOnly, Doc: "Files owned by the effective user."},
	{Name: "G", shells: zshOnly, Doc: "Files owned by the effective group."},
	{Name: "L", shells: zshOnly, Doc: "Files by size, as in `L+10` (over 10 bytes) or `Lk-5` (under 5 KiB)."},
	{Name: "m", shells: zshOnly, Doc: "Files by modification age, as in `mh-1` (changed within the last hour) or `md+7`."},
	{Name: "a", shells: zshOnly, Doc: "Files by access age, with the same units as `m`."},
	{Name: "N", shells: zshOnly, Doc: "Null glob: expand to nothing instead of failing when nothing matches."},
	{Name: "D", shells: zshOnly, Doc: "Include names starting with a dot."},
	{Name: "n", shells: zshOnly, Doc: "Sort numbers numerically."},
	{Name: "o", shells: zshOnly, Doc: "Sort ascending by the key that follows: `on` name, `oL` size, `om` modification time, `oa` access time."},
	{Name: "O", shells: zshOnly, Doc: "Sort descending by the key that follows, like `o`."},
	{Name: "[", shells: zshOnly, Doc: "Select a range of the sorted matches, as in `[1]` (the first) or `[1,5]`."},
	{Name: "^", shells: zshOnly, Doc: "Negate the qualifiers that follow."},
	{Name: "-", shells: zshOnly, Doc: "Follow symbolic links for the qualifiers that follow."},
	{Name: "e", shells: zshOnly, Doc: "Keep matches for which the given code succeeds, as in `e:'[[ -s $REPLY ]]':`."},
	{Name: "+", shells: zshOnly, Doc: "Keep matches for which the named function succeeds, with the match in `REPLY`."},
	{Name: "Y", shells: zshOnly, Doc: "Stop after the given number of matches, as in `Y1`."},
	{Name: "F", shells: zshOnly, Doc: "Full directories: directories with at least one entry."},
	{Name: "A", shells: zshOnly, Doc: "Files readable by their group."},
	{Name: "I", shells: zshOnly, Doc: "Files writable by their group."},
	{Name: "E", shells: zshOnly, Doc: "Files executable by their group."},
	{Name: "s", shells: zshOnly, Doc: "Setuid files."},
	{Name: "S", shells: zshOnly, Doc: "Setgid files."},
	{Name: "t", shells: zshOnly, Doc: "Files with the sticky bit set."},
	{Name: "f", shells: zshOnly, Doc: "Files by permission bits, as in `f644` or `f:u+x:`."},
	{Name: "u", shells: zshOnly, Doc: "Files owned by the given user, as in `u0` or `u:root:`."},
	{Name: "g", shells: zshOnly, Doc: "Files owned by the given group, as in `g0` or `g:wheel:`."},
	{Name: "d", shells: zshOnly, Doc: "Files on the given device, as in `d17`."},
	{Name: "l", shells: zshOnly, Doc: "Files by hard link count, as in `l+1` (more than one link)."},
	{Name: "c", shells: zshOnly, Doc: "Files by inode change age, with the same units as `m`."},
	{Name: "M", shells: zshOnly, Doc: "Mark directories with a trailing `/`."},
	{Name: "T", shells: zshOnly, Doc: "Append a type character to each name, as `ls -F` does."},
	{Name: "P", shells: zshOnly, Doc: "Put the given word before every match as a separate word, as in `P:-f:`."},
	{Name: ",", shells: zshOnly, Doc: "Separates alternative sets of qualifiers; a file is kept when any set matches it."},
	{Name: ":", shells: zshOnly, Doc: "Apply the modifiers that follow to every match, as in `(:t)` or `(:r)`."},
}

var zshGlobFlags = []Entry{
	{Name: "i", shells: zshOnly, Doc: "Case-insensitive: the rest of the pattern, up to the end of its group, matches letters in either case."},
	{Name: "l", shells: zshOnly, Doc: "Lowercase letters in the rest of the pattern also match uppercase ones; uppercase letters still only match uppercase."},
	{Name: "I", shells: zshOnly, Doc: "Case-sensitive again, undoing `(#i)` and `(#l)`."},
	{Name: "b", shells: zshOnly, Doc: "Backreferences: every parenthesised group after this flag stores what it matched in `$match`, with offsets in `$mbegin` and `$mend`."},
	{Name: "B", shells: zshOnly, Doc: "Backreferences off again, undoing `(#b)`."},
	{Name: "m", shells: zshOnly, Doc: "Stores the whole matched text in `$MATCH`, with its offsets in `$MBEGIN` and `$MEND`."},
	{Name: "M", shells: zshOnly, Doc: "Stops setting `$MATCH`, undoing `(#m)`."},
	{Name: "a", shells: zshOnly, Doc: "Approximate matching: allows up to the given number of errors (insertions, deletions, transpositions or substitutions), as in `(#a1)`."},
	{Name: "s", shells: zshOnly, Doc: "Matches only at the start of the value, like `^` in a regular expression."},
	{Name: "e", shells: zshOnly, Doc: "Matches only at the end of the value, like `$` in a regular expression."},
	{Name: "u", shells: zshOnly, Doc: "Treats multibyte characters as single characters, whatever the `multibyte` option says."},
	{Name: "U", shells: zshOnly, Doc: "Matches byte by byte, whatever the `multibyte` option says."},
	{Name: "q", shells: zshOnly, Doc: "Glob qualifiers inside the pattern, as in `*(#q.)`; ignored where no file names are generated."},
	{Name: "c", shells: zshOnly, Doc: "Repeats what comes before it: `(#c3)` exactly three times, `(#c1,3)` one to three times."},
}

var printfDirectives = []Entry{
	{Name: "%s", shells: every, Doc: "The argument as a string."},
	{Name: "%b", shells: every, Doc: "The argument as a string, with backslash escapes such as `\\n` expanded."},
	{Name: "%c", shells: every, Doc: "The first character of the argument."},
	{Name: "%d", shells: every, Doc: "The argument as a signed decimal integer. A leading quote gives a character's code: `printf %d \"'A\"`."},
	{Name: "%i", shells: every, Doc: "Same as `%d`."},
	{Name: "%u", shells: every, Doc: "The argument as an unsigned decimal integer."},
	{Name: "%o", shells: every, Doc: "The argument as an unsigned octal integer."},
	{Name: "%x", shells: every, Doc: "The argument as an unsigned hexadecimal integer in lowercase."},
	{Name: "%X", shells: every, Doc: "The argument as an unsigned hexadecimal integer in uppercase."},
	{Name: "%f", shells: extended, Doc: "The argument as a decimal floating-point number; `%.2f` rounds to two decimals."},
	{Name: "%e", shells: extended, Doc: "The argument in scientific notation, such as `1.5e+03`."},
	{Name: "%g", shells: extended, Doc: "The argument as `%f` or `%e`, whichever is shorter."},
	{Name: "%q", shells: bashZshKsh, Doc: "The argument quoted so that the shell can read it back as one word."},
	{Name: "%(", shells: bashKsh, Synopsis: "%(%F)T", Doc: "The argument, seconds since the epoch, formatted as a date with the `strftime` format between the parentheses; `-1` means now."},
	{Name: "%%", shells: every, Doc: "A literal `%`."},
}

var redirections = []Entry{
	{Name: ">", shells: every, Synopsis: "[n]> file", Doc: "Redirects output (descriptor `n`, default 1) to `file`, truncating it first. Fails on an existing file when `noclobber` is set."},
	{Name: ">>", shells: every, Synopsis: "[n]>> file", Doc: "Redirects output (descriptor `n`, default 1) to `file`, appending to it."},
	{Name: "<", shells: every, Synopsis: "[n]< file", Doc: "Reads input (descriptor `n`, default 0) from `file`."},
	{Name: "<>", shells: every, Synopsis: "[n]<> file", Doc: "Opens `file` for reading and writing on descriptor `n` (default 0), creating it if needed."},
	{Name: ">|", shells: every, Synopsis: "[n]>| file", Doc: "Like `>`, but overwrites `file` even when `noclobber` is set."},
	{Name: ">&", shells: every, Synopsis: "[n]>&m", Doc: "Makes descriptor `n` (default 1) a copy of descriptor `m`, so `2>&1` sends errors where output goes. `>&-` closes the descriptor."},
	{Name: "<&", shells: every, Synopsis: "[n]<&m", Doc: "Makes descriptor `n` (default 0) a copy of input descriptor `m`; `<&-` closes it."},
	{Name: "&>", shells: bashZsh, Synopsis: "&> file", Doc: "Redirects both output and errors to `file`; the portable spelling is `> file 2>&1`."},
	{Name: "&>>", shells: bashZsh, Synopsis: "&>> file", Doc: "Appends both output and errors to `file`; the portable spelling is `>> file 2>&1`."},
	{Name: "<<", shells: every, Synopsis: "<< DELIM", Doc: "Here-document: the following lines up to `DELIM` become the input. Unless the delimiter is quoted, the body undergoes parameter, command and arithmetic expansion."},
	{Name: "<<-", shells: every, Synopsis: "<<- DELIM", Doc: "Here-document that strips leading tab characters (not spaces) from the body and the closing delimiter, so it can be indented with tabs."},
	{Name: "<<#", shells: kshOnly, Synopsis: "<<# DELIM", Doc: "Here-document that strips leading blanks (spaces and tabs) from the body and the closing delimiter, so it can be indented with either."},
	{Name: "<<<", shells: bashZshKsh, Synopsis: "<<< word", Doc: "Here-string: `word` is expanded and fed to standard input with a trailing newline."},
}

var arithmeticOperators = []Entry{
	{Name: "+", shells: every, Doc: "Addition, or unary plus."},
	{Name: "-", shells: every, Doc: "Subtraction, or negation."},
	{Name: "*", shells: every, Doc: "Multiplication."},
	{Name: "/", shells: every, Doc: "Integer division, truncating towards zero; dividing by zero is an error."},
	{Name: "%", shells: every, Doc: "Remainder of the integer division, with the sign of the dividend."},
	{Name: "**", shells: bashZshKsh, Doc: "Exponentiation."},
	{Name: "<<", shells: every, Doc: "Bitwise shift left."},
	{Name: ">>", shells: every, Doc: "Bitwise shift right."},
	{Name: "&", shells: every, Doc: "Bitwise and."},
	{Name: "|", shells: every, Doc: "Bitwise or."},
	{Name: "^", shells: every, Doc: "Bitwise exclusive or."},
	{Name: "~", shells: every, Doc: "Bitwise negation."},
	{Name: "!", shells: every, Doc: "Logical not: 1 for 0, 0 otherwise."},
	{Name: "&&", shells: every, Doc: "Logical and; the right side is evaluated only when the left one is non-zero."},
	{Name: "||", shells: every, Doc: "Logical or; the right side is evaluated only when the left one is zero."},
	{Name: "==", shells: every, Doc: "1 if both sides are equal, 0 otherwise."},
	{Name: "!=", shells: every, Doc: "1 if the sides differ, 0 otherwise."},
	{Name: "<", shells: every, Doc: "1 if the left side is smaller, 0 otherwise."},
	{Name: "<=", shells: every, Doc: "1 if the left side is smaller or equal, 0 otherwise."},
	{Name: ">", shells: every, Doc: "1 if the left side is greater, 0 otherwise."},
	{Name: ">=", shells: every, Doc: "1 if the left side is greater or equal, 0 otherwise."},
	{Name: "=", shells: every, Doc: "Assignment; the expression has the assigned value."},
	{Name: "+=", shells: every, Doc: "Adds the right side to the variable."},
	{Name: "-=", shells: every, Doc: "Subtracts the right side from the variable."},
	{Name: "*=", shells: every, Doc: "Multiplies the variable by the right side."},
	{Name: "/=", shells: every, Doc: "Divides the variable by the right side."},
	{Name: "%=", shells: every, Doc: "Replaces the variable with the remainder of dividing it by the right side."},
	{Name: "<<=", shells: every, Doc: "Shifts the variable left by the right side."},
	{Name: ">>=", shells: every, Doc: "Shifts the variable right by the right side."},
	{Name: "&=", shells: every, Doc: "Bitwise-ands the variable with the right side."},
	{Name: "|=", shells: every, Doc: "Bitwise-ors the variable with the right side."},
	{Name: "^=", shells: every, Doc: "Bitwise-xors the variable with the right side."},
	{Name: "++", shells: bashZshKsh, Doc: "Increment: `++i` yields the new value, `i++` the old one."},
	{Name: "--", shells: bashZshKsh, Doc: "Decrement: `--i` yields the new value, `i--` the old one."},
	{Name: "?", shells: every, Doc: "Conditional: `cond ? a : b` is `a` when `cond` is non-zero, `b` otherwise."},
	{Name: ":", shells: every, Doc: "Separates the branches of a `? :` conditional."},
	{Name: ",", shells: bashZshKsh, Doc: "Evaluates both sides and yields the right one."},
}

var listOperators = []Entry{
	{Name: "&&", shells: every, Synopsis: "cmd1 && cmd2", Doc: "Runs `cmd2` only if `cmd1` succeeds. The list fails if either command fails, but a failing `cmd1` does not trigger `set -e`."},
	{Name: "||", shells: every, Synopsis: "cmd1 || cmd2", Doc: "Runs `cmd2` only if `cmd1` fails. `cmd || exit` is the usual way to stop on an error."},
	{Name: "|", shells: every, Synopsis: "cmd1 | cmd2", Doc: "Pipes the output of `cmd1` into the input of `cmd2`. Each side runs in its own subshell (bash runs the last one in the current shell with `lastpipe`, zsh and ksh always do)."},
	{Name: "|&", shells: bashZsh, Synopsis: "cmd1 |& cmd2", Doc: "Pipes both the output and the errors of `cmd1` into `cmd2`, like `cmd1 2>&1 | cmd2`. In ksh, `cmd |&` starts a co-process instead."},
	{Name: "&", shells: every, Synopsis: "cmd &", Doc: "Runs `cmd` in the background; `$!` holds its process ID."},
	{Name: ";", shells: every, Synopsis: "cmd1; cmd2", Doc: "Runs the commands one after the other."},
	{Name: ";;", shells: every, Synopsis: "pattern) list ;;", Doc: "Ends a `case` branch; no further pattern is tried."},
	{Name: ";&", shells: bashZshKsh, Synopsis: "pattern) list ;&", Doc: "Ends a `case` branch and falls through into the next branch's commands without testing its pattern."},
	{Name: ";;&", shells: bashOnly, Synopsis: "pattern) list ;;&", Doc: "Ends a `case` branch and keeps testing the following patterns."},
	{Name: ";|", shells: zshOnly, Synopsis: "pattern) list ;|", Doc: "Ends a `case` branch and keeps testing the following patterns."},
}

var optionLetters = append(letterEntries(func(letter string) string { return letter }),
	Entry{Name: "i", shells: every, Doc: "The shell is interactive; `case $- in *i*)` is the usual test."},
	Entry{Name: "s", shells: every, Doc: "Commands are read from standard input."},
	Entry{Name: "c", shells: every, Doc: "Commands came from the `-c` argument."},
)
