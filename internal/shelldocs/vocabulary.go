package shelldocs

import (
	"iter"

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
	PrintfDirectives:    printfDirectives,
	Redirections:        redirections,
	ArithmeticOperators: arithmeticOperators,
	ListOperators:       listOperators,
	OptionLetters:       optionLetters,
}

var vocabularyIndex = func() [vocabularyCount]table {
	var out [vocabularyCount]table
	for v, entries := range vocabularies {
		out[v] = index(entries)
	}
	return out
}()

func Terms(v Vocabulary, d dialect.Dialect) iter.Seq[Entry] {
	return func(yield func(Entry) bool) {
		for _, e := range vocabularies[v] {
			if e.Dialects.Has(d) && !yield(e) {
				return
			}
		}
	}
}

func Term(v Vocabulary, name string) (Entry, bool) {
	e, ok := vocabularyIndex[v][name]
	return e, ok
}

var (
	notPosix = dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Dash, dialect.Ksh, dialect.Busybox)
	bashKsh  = dialect.SetOf(dialect.Bash, dialect.Ksh)
	extended = dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Ksh, dialect.Busybox)
)

var emulateModes = []Entry{
	{Name: "zsh", Dialects: zshOnly, Doc: "Restore native zsh option settings."},
	{Name: "sh", Dialects: zshOnly, Doc: "Set options so that zsh behaves like a POSIX shell: word splitting of unquoted expansions, no `**` recursion, arrays indexed from 0 where relevant."},
	{Name: "ksh", Dialects: zshOnly, Doc: "Set options so that zsh behaves like ksh: arrays indexed from 0, `ksh_glob` patterns, word splitting of unquoted expansions."},
	{Name: "csh", Dialects: zshOnly, Doc: "Set options so that zsh behaves like csh as far as it can."},
	{Name: "bash", Dialects: zshOnly, Doc: "Accepted as a synonym for `sh` emulation; zsh has no separate bash mode."},
}

var trapConditions = []Entry{
	{Name: "EXIT", Dialects: every, Doc: "Runs the action when the shell exits, however it exits. `0` is a synonym."},
	{Name: "ERR", Dialects: bashKsh, Doc: "Runs the action whenever a command fails, under the same rules that make `set -e` exit."},
	{Name: "ZERR", Dialects: zshOnly, Doc: "Runs the action whenever a command fails, under the same rules that make `setopt err_exit` exit. `ERR` is accepted as an alias."},
	{Name: "DEBUG", Dialects: bashZshKsh, Doc: "Runs the action before every simple command (after it in zsh), which is how debuggers and tracing helpers hook in."},
	{Name: "RETURN", Dialects: bashOnly, Doc: "Runs the action when a function or a sourced file returns. Functions only inherit it with `set -T` or the `functrace` option."},
	{Name: "KEYBD", Dialects: kshOnly, Doc: "Runs the action for every key read by the line editor; `.sh.edchar` holds the key."},
}

var signals = []Entry{
	{Name: "HUP", Dialects: every, Doc: "Hangup (1): the controlling terminal went away. Daemons often use it to reload their configuration."},
	{Name: "INT", Dialects: every, Doc: "Interrupt (2): sent by Ctrl-C. Trapping it lets a script clean up before stopping."},
	{Name: "QUIT", Dialects: every, Doc: "Quit (3): sent by Ctrl-\\. Terminates and dumps core by default."},
	{Name: "ILL", Dialects: every, Doc: "Illegal instruction (4)."},
	{Name: "TRAP", Dialects: every, Doc: "Trace or breakpoint trap (5)."},
	{Name: "ABRT", Dialects: every, Doc: "Abort (6): raised by `abort()`; terminates and dumps core by default."},
	{Name: "BUS", Dialects: every, Doc: "Bus error: a bad memory access."},
	{Name: "FPE", Dialects: every, Doc: "Arithmetic exception (8), such as an integer division by zero."},
	{Name: "KILL", Dialects: every, Doc: "Kill (9): ends the process immediately. It cannot be caught, blocked or trapped."},
	{Name: "USR1", Dialects: every, Doc: "User-defined signal 1, with no meaning to the system."},
	{Name: "SEGV", Dialects: every, Doc: "Segmentation fault (11): an invalid memory reference."},
	{Name: "USR2", Dialects: every, Doc: "User-defined signal 2, with no meaning to the system."},
	{Name: "PIPE", Dialects: every, Doc: "Broken pipe (13): a write to a pipe whose reader has exited. Commands in a pipeline usually die of it quietly."},
	{Name: "ALRM", Dialects: every, Doc: "Alarm clock (14): a timer set with `alarm()` expired."},
	{Name: "TERM", Dialects: every, Doc: "Termination (15): the polite request to stop, and the default signal of `kill`."},
	{Name: "CHLD", Dialects: every, Doc: "A child process stopped or exited. Ignored by default."},
	{Name: "CONT", Dialects: every, Doc: "Continue a stopped process."},
	{Name: "STOP", Dialects: every, Doc: "Stop the process. It cannot be caught, blocked or trapped."},
	{Name: "TSTP", Dialects: every, Doc: "Terminal stop: sent by Ctrl-Z."},
	{Name: "TTIN", Dialects: every, Doc: "A background process tried to read from the terminal."},
	{Name: "TTOU", Dialects: every, Doc: "A background process tried to write to the terminal while `tostop` is set."},
	{Name: "URG", Dialects: every, Doc: "Urgent data arrived on a socket."},
	{Name: "XCPU", Dialects: every, Doc: "The CPU time limit set with `ulimit -t` was exceeded."},
	{Name: "XFSZ", Dialects: every, Doc: "The file size limit set with `ulimit -f` was exceeded."},
	{Name: "VTALRM", Dialects: every, Doc: "A virtual (user CPU time) timer expired."},
	{Name: "PROF", Dialects: every, Doc: "A profiling timer expired."},
	{Name: "WINCH", Dialects: every, Doc: "The terminal window changed size."},
	{Name: "IO", Dialects: every, Doc: "Input or output became possible on a descriptor set up for asynchronous I/O."},
	{Name: "SYS", Dialects: every, Doc: "A bad system call argument."},
}

var testUnaryOperators = []Entry{
	{Name: "-e", Dialects: every, Synopsis: "-e file", Doc: "True if `file` exists, whatever its type."},
	{Name: "-f", Dialects: every, Synopsis: "-f file", Doc: "True if `file` exists and is a regular file (symbolic links are followed)."},
	{Name: "-d", Dialects: every, Synopsis: "-d file", Doc: "True if `file` exists and is a directory."},
	{Name: "-s", Dialects: every, Synopsis: "-s file", Doc: "True if `file` exists and is not empty."},
	{Name: "-r", Dialects: every, Synopsis: "-r file", Doc: "True if `file` exists and the shell may read it."},
	{Name: "-w", Dialects: every, Synopsis: "-w file", Doc: "True if `file` exists and the shell may write to it."},
	{Name: "-x", Dialects: every, Synopsis: "-x file", Doc: "True if `file` exists and the shell may execute it (search it, for a directory)."},
	{Name: "-L", Dialects: every, Synopsis: "-L file", Doc: "True if `file` is a symbolic link, dangling or not."},
	{Name: "-h", Dialects: every, Synopsis: "-h file", Doc: "Same as `-L`: true if `file` is a symbolic link."},
	{Name: "-b", Dialects: every, Synopsis: "-b file", Doc: "True if `file` is a block device."},
	{Name: "-c", Dialects: every, Synopsis: "-c file", Doc: "True if `file` is a character device."},
	{Name: "-p", Dialects: every, Synopsis: "-p file", Doc: "True if `file` is a named pipe (FIFO)."},
	{Name: "-S", Dialects: every, Synopsis: "-S file", Doc: "True if `file` is a socket."},
	{Name: "-g", Dialects: every, Synopsis: "-g file", Doc: "True if `file` has its set-group-ID bit set."},
	{Name: "-u", Dialects: every, Synopsis: "-u file", Doc: "True if `file` has its set-user-ID bit set."},
	{Name: "-k", Dialects: notPosix, Synopsis: "-k file", Doc: "True if `file` has its sticky bit set."},
	{Name: "-O", Dialects: notPosix, Synopsis: "-O file", Doc: "True if `file` is owned by the effective user of the shell."},
	{Name: "-G", Dialects: notPosix, Synopsis: "-G file", Doc: "True if `file` belongs to the effective group of the shell."},
	{Name: "-N", Dialects: bashZsh, Synopsis: "-N file", Doc: "True if `file` was modified after it was last read."},
	{Name: "-t", Dialects: every, Synopsis: "-t fd", Doc: "True if descriptor `fd` is open on a terminal; `[ -t 1 ]` asks whether output goes to a terminal."},
	{Name: "-z", Dialects: every, Synopsis: "-z string", Doc: "True if `string` is empty."},
	{Name: "-n", Dialects: every, Synopsis: "-n string", Doc: "True if `string` is not empty. Quote the operand: an unquoted empty expansion leaves `[ -n ]`, which is true."},
	{Name: "-v", Dialects: bashZshKsh, Synopsis: "-v name", Doc: "True if the variable `name` is set, even to an empty value. Takes a name, not `$name`."},
	{Name: "-R", Dialects: bashOnly, Synopsis: "-R name", Doc: "True if `name` is set and is a name reference."},
	{Name: "-o", Dialects: bashZshKsh, Synopsis: "-o option", Doc: "Inside `[[ ]]`, true if the shell option `option` is enabled. Between two expressions in `[ ]`, `-o` is a logical or instead."},
}

var testBinaryOperators = []Entry{
	{Name: "=", Dialects: every, Synopsis: "string1 = string2", Doc: "True if the strings are equal. Inside `[[ ]]` an unquoted right-hand side is a glob pattern."},
	{Name: "==", Dialects: extended, Synopsis: "string1 == string2", Doc: "Same as `=`. Not portable to POSIX `[`; inside `[[ ]]` an unquoted right-hand side is a glob pattern."},
	{Name: "!=", Dialects: every, Synopsis: "string1 != string2", Doc: "True if the strings differ. Inside `[[ ]]` an unquoted right-hand side is a glob pattern."},
	{Name: "-eq", Dialects: every, Synopsis: "n1 -eq n2", Doc: "True if the integers are equal."},
	{Name: "-ne", Dialects: every, Synopsis: "n1 -ne n2", Doc: "True if the integers differ."},
	{Name: "-lt", Dialects: every, Synopsis: "n1 -lt n2", Doc: "True if `n1` is less than `n2`."},
	{Name: "-le", Dialects: every, Synopsis: "n1 -le n2", Doc: "True if `n1` is less than or equal to `n2`."},
	{Name: "-gt", Dialects: every, Synopsis: "n1 -gt n2", Doc: "True if `n1` is greater than `n2`."},
	{Name: "-ge", Dialects: every, Synopsis: "n1 -ge n2", Doc: "True if `n1` is greater than or equal to `n2`."},
	{Name: "-nt", Dialects: every, Synopsis: "file1 -nt file2", Doc: "True if `file1` was modified more recently than `file2`, or if only `file1` exists."},
	{Name: "-ot", Dialects: every, Synopsis: "file1 -ot file2", Doc: "True if `file1` was modified before `file2`, or if only `file2` exists."},
	{Name: "-ef", Dialects: every, Synopsis: "file1 -ef file2", Doc: "True if both names refer to the same file (same device and inode)."},
}

var condOperators = []Entry{
	{Name: "=~", Dialects: bashZshKsh, Synopsis: "string =~ regex", Doc: "True if `string` matches the extended regular expression. Quoted parts of `regex` match literally; keep the pattern in a variable to avoid quoting surprises. Captures land in `BASH_REMATCH` (bash) or `match` (zsh)."},
	{Name: "<", Dialects: bashZshKsh, Synopsis: "string1 < string2", Doc: "True if `string1` sorts before `string2` in the current locale. Inside `[ ]` it would be a redirection and must be escaped as `\\<`."},
	{Name: ">", Dialects: bashZshKsh, Synopsis: "string1 > string2", Doc: "True if `string1` sorts after `string2` in the current locale. Inside `[ ]` it would be a redirection and must be escaped as `\\>`."},
	{Name: "&&", Dialects: bashZshKsh, Synopsis: "expr1 && expr2", Doc: "True if both expressions are true; `expr2` is only evaluated when `expr1` is true."},
	{Name: "||", Dialects: bashZshKsh, Synopsis: "expr1 || expr2", Doc: "True if either expression is true; `expr2` is only evaluated when `expr1` is false."},
}

var expansionOperators = []Entry{
	{Name: ":-", Dialects: every, Synopsis: "${name:-word}", Doc: "Expands to `word` when `name` is unset or empty, otherwise to its value."},
	{Name: "-", Dialects: every, Synopsis: "${name-word}", Doc: "Expands to `word` when `name` is unset, otherwise to its value, even an empty one."},
	{Name: ":=", Dialects: every, Synopsis: "${name:=word}", Doc: "Assigns `word` to `name` when it is unset or empty, then expands to the value."},
	{Name: "=", Dialects: every, Synopsis: "${name=word}", Doc: "Assigns `word` to `name` when it is unset, then expands to the value."},
	{Name: ":?", Dialects: every, Synopsis: "${name:?message}", Doc: "Fails with `message` when `name` is unset or empty; a non-interactive shell exits."},
	{Name: "?", Dialects: every, Synopsis: "${name?message}", Doc: "Fails with `message` when `name` is unset; a non-interactive shell exits."},
	{Name: ":+", Dialects: every, Synopsis: "${name:+word}", Doc: "Expands to `word` when `name` is set and not empty, otherwise to nothing."},
	{Name: "+", Dialects: every, Synopsis: "${name+word}", Doc: "Expands to `word` when `name` is set, even to an empty value, otherwise to nothing."},
	{Name: "#", Dialects: every, Synopsis: "${name#pattern}", Doc: "Removes the shortest prefix matching the glob `pattern`."},
	{Name: "##", Dialects: every, Synopsis: "${name##pattern}", Doc: "Removes the longest prefix matching the glob `pattern`; `${path##*/}` keeps the last path component."},
	{Name: "%", Dialects: every, Synopsis: "${name%pattern}", Doc: "Removes the shortest suffix matching the glob `pattern`; `${file%.*}` drops one extension."},
	{Name: "%%", Dialects: every, Synopsis: "${name%%pattern}", Doc: "Removes the longest suffix matching the glob `pattern`."},
	{Name: "/", Dialects: extended, Synopsis: "${name/pattern/string}", Doc: "Replaces the first match of `pattern` with `string`."},
	{Name: "//", Dialects: extended, Synopsis: "${name//pattern/string}", Doc: "Replaces every match of `pattern` with `string`."},
	{Name: "/#", Dialects: extended, Synopsis: "${name/#pattern/string}", Doc: "Replaces `pattern` only where it matches at the start of the value."},
	{Name: "/%", Dialects: extended, Synopsis: "${name/%pattern/string}", Doc: "Replaces `pattern` only where it matches at the end of the value."},
	{Name: ":", Dialects: extended, Synopsis: "${name:offset:length}", Doc: "Substring starting at the zero-based `offset`, at most `length` characters long; for `@` and arrays, a range of elements. A negative offset needs a space or parentheses: `${name: -2}`."},
	{Name: "^", Dialects: bashOnly, Synopsis: "${name^pattern}", Doc: "Uppercases the first character when it matches `pattern` (any character when omitted)."},
	{Name: "^^", Dialects: bashOnly, Synopsis: "${name^^pattern}", Doc: "Uppercases every character matching `pattern` (all of them when omitted)."},
	{Name: ",", Dialects: bashOnly, Synopsis: "${name,pattern}", Doc: "Lowercases the first character when it matches `pattern` (any character when omitted)."},
	{Name: ",,", Dialects: bashOnly, Synopsis: "${name,,pattern}", Doc: "Lowercases every character matching `pattern` (all of them when omitted)."},
	{Name: "@Q", Dialects: bashOnly, Synopsis: "${name@Q}", Doc: "Quotes the value so that it can be read back as shell input."},
	{Name: "@E", Dialects: bashOnly, Synopsis: "${name@E}", Doc: "Expands backslash escape sequences in the value, as `$'...'` would."},
	{Name: "@P", Dialects: bashOnly, Synopsis: "${name@P}", Doc: "Expands the value as if it were a prompt string such as `PS1`."},
	{Name: "@A", Dialects: bashOnly, Synopsis: "${name@A}", Doc: "Produces an assignment statement (with `declare` and attributes) that recreates the variable."},
	{Name: "@K", Dialects: bashOnly, Synopsis: "${name@K}", Doc: "Like `@Q`, but an associative or indexed array becomes quoted key-value pairs."},
	{Name: "@k", Dialects: bashOnly, Synopsis: "${name[@]@k}", Doc: "Like `@K`, but each key and value becomes a separate word."},
	{Name: "@a", Dialects: bashOnly, Synopsis: "${name@a}", Doc: "Expands to the attribute flags of the variable, such as `a`, `A`, `x` or `r`."},
	{Name: "@U", Dialects: bashOnly, Synopsis: "${name@U}", Doc: "Uppercases the whole value."},
	{Name: "@u", Dialects: bashOnly, Synopsis: "${name@u}", Doc: "Uppercases the first character of the value."},
	{Name: "@L", Dialects: bashOnly, Synopsis: "${name@L}", Doc: "Lowercases the whole value."},
	{Name: ":#", Dialects: zshOnly, Synopsis: "${name:#pattern}", Doc: "Removes the array elements matching `pattern`; for a scalar, the whole value if it matches."},
	{Name: ":|", Dialects: zshOnly, Synopsis: "${name:|array}", Doc: "Removes the elements that also appear in `array` (set difference)."},
	{Name: ":*", Dialects: zshOnly, Synopsis: "${name:*array}", Doc: "Keeps only the elements that also appear in `array` (set intersection)."},
	{Name: ":^", Dialects: zshOnly, Synopsis: "${name:^array}", Doc: "Zips the two arrays, alternating their elements and stopping at the end of the shorter one."},
	{Name: ":^^", Dialects: zshOnly, Synopsis: "${name:^^array}", Doc: "Zips the two arrays, repeating the shorter one until the longer one is used up."},
	{Name: "::=", Dialects: zshOnly, Synopsis: "${name::=word}", Doc: "Assigns `word` to `name` unconditionally, then expands to it."},
}

var expansionForms = []Entry{
	{Name: "length", Dialects: every, Synopsis: "${#name}", Doc: "Length of the value in characters; for `${#array[@]}`, the number of elements."},
	{Name: "indirect", Dialects: bashOnly, Synopsis: "${!name}", Doc: "Expands the variable whose name is the value of `name` (indirection)."},
	{Name: "keys", Dialects: bashKsh, Synopsis: "${!name[@]}", Doc: "Expands to the indices or keys of the array."},
	{Name: "names", Dialects: bashKsh, Synopsis: "${!prefix*}", Doc: "Expands to the names of every variable starting with `prefix`; `${!prefix@}` keeps them as separate words."},
	{Name: "width", Dialects: kshOnly, Synopsis: "${%name}", Doc: "Display width of the value in terminal columns (mksh)."},
	{Name: "isset", Dialects: zshOnly, Synopsis: "${+name}", Doc: "Expands to 1 if `name` is set, otherwise to 0."},
}

var zshParameterFlags = []Entry{
	{Name: "@", Dialects: zshOnly, Doc: "Keep array elements as separate words inside double quotes."},
	{Name: "A", Dialects: zshOnly, Doc: "Create an array parameter with `${...=...}`; `AA` creates an associative array."},
	{Name: "a", Dialects: zshOnly, Doc: "Sort in array index order (reversed with `O`)."},
	{Name: "c", Dialects: zshOnly, Doc: "With `${#...}`, count the characters of the joined words."},
	{Name: "C", Dialects: zshOnly, Doc: "Capitalise the first letter of each word."},
	{Name: "e", Dialects: zshOnly, Doc: "Perform parameter, command and arithmetic substitution on the result."},
	{Name: "f", Dialects: zshOnly, Doc: "Split the result into lines, like `(ps:\\n:)`."},
	{Name: "F", Dialects: zshOnly, Doc: "Join array elements with newlines, like `(pj:\\n:)`."},
	{Name: "i", Dialects: zshOnly, Doc: "Sort case-insensitively."},
	{Name: "j", Dialects: zshOnly, Doc: "Join array elements with the separator that follows, as in `(j:,:)`."},
	{Name: "k", Dialects: zshOnly, Doc: "Substitute the keys of an associative array."},
	{Name: "L", Dialects: zshOnly, Doc: "Convert to lowercase."},
	{Name: "n", Dialects: zshOnly, Doc: "Sort numerically."},
	{Name: "o", Dialects: zshOnly, Doc: "Sort in ascending order."},
	{Name: "O", Dialects: zshOnly, Doc: "Sort in descending order."},
	{Name: "P", Dialects: zshOnly, Doc: "Use the value as the name of the parameter to expand (indirection)."},
	{Name: "q", Dialects: zshOnly, Doc: "Quote special characters with backslashes; `qq` single-quotes, `qqq` double-quotes, `q-` quotes minimally."},
	{Name: "Q", Dialects: zshOnly, Doc: "Remove one level of quoting."},
	{Name: "s", Dialects: zshOnly, Doc: "Split on the separator that follows, as in `(s:/:)`."},
	{Name: "t", Dialects: zshOnly, Doc: "Substitute the type of the parameter, such as `scalar`, `array` or `association-local`."},
	{Name: "u", Dialects: zshOnly, Doc: "Keep only the first occurrence of each word."},
	{Name: "U", Dialects: zshOnly, Doc: "Convert to uppercase."},
	{Name: "v", Dialects: zshOnly, Doc: "Substitute the values of an associative array; with `k`, keys and values alternate."},
	{Name: "V", Dialects: zshOnly, Doc: "Make special characters visible."},
	{Name: "w", Dialects: zshOnly, Doc: "With `${#...}`, count words."},
	{Name: "z", Dialects: zshOnly, Doc: "Split into words the way the shell parser would."},
	{Name: "0", Dialects: zshOnly, Doc: "Split on NUL bytes, like `(ps:\\0:)`."},
	{Name: "%", Dialects: zshOnly, Doc: "Expand prompt escape sequences."},
	{Name: "~", Dialects: zshOnly, Doc: "Treat the separator of `j` and `s` as a pattern."},
}

var zshModifiers = []Entry{
	{Name: "h", Dialects: zshOnly, Doc: "Head: remove the last path component, like `dirname`."},
	{Name: "t", Dialects: zshOnly, Doc: "Tail: keep only the last path component, like `basename`."},
	{Name: "r", Dialects: zshOnly, Doc: "Root: remove the file extension."},
	{Name: "e", Dialects: zshOnly, Doc: "Extension: keep only the file extension."},
	{Name: "a", Dialects: zshOnly, Doc: "Make the path absolute."},
	{Name: "A", Dialects: zshOnly, Doc: "Make the path absolute and resolve symbolic links."},
	{Name: "c", Dialects: zshOnly, Doc: "Resolve a command name to its path."},
	{Name: "l", Dialects: zshOnly, Doc: "Convert to lowercase."},
	{Name: "u", Dialects: zshOnly, Doc: "Convert to uppercase."},
	{Name: "q", Dialects: zshOnly, Doc: "Quote the result."},
	{Name: "Q", Dialects: zshOnly, Doc: "Remove one level of quoting."},
	{Name: "P", Dialects: zshOnly, Doc: "Make the path absolute and physical, without symbolic links or `..`."},
	{Name: "s", Dialects: zshOnly, Doc: "Substitute once, as in `:s/old/new/`."},
	{Name: "gs", Dialects: zshOnly, Doc: "Substitute every occurrence, as in `:gs/old/new/`."},
}

var zshGlobQualifiers = []Entry{
	{Name: "/", Dialects: zshOnly, Doc: "Directories."},
	{Name: ".", Dialects: zshOnly, Doc: "Regular files."},
	{Name: "@", Dialects: zshOnly, Doc: "Symbolic links."},
	{Name: "=", Dialects: zshOnly, Doc: "Sockets."},
	{Name: "p", Dialects: zshOnly, Doc: "Named pipes (FIFOs)."},
	{Name: "*", Dialects: zshOnly, Doc: "Executable regular files."},
	{Name: "%", Dialects: zshOnly, Doc: "Device files; `%b` block and `%c` character devices."},
	{Name: "r", Dialects: zshOnly, Doc: "Files readable by their owner."},
	{Name: "w", Dialects: zshOnly, Doc: "Files writable by their owner."},
	{Name: "x", Dialects: zshOnly, Doc: "Files executable by their owner."},
	{Name: "R", Dialects: zshOnly, Doc: "Files readable by everyone."},
	{Name: "W", Dialects: zshOnly, Doc: "Files writable by everyone."},
	{Name: "X", Dialects: zshOnly, Doc: "Files executable by everyone."},
	{Name: "U", Dialects: zshOnly, Doc: "Files owned by the effective user."},
	{Name: "G", Dialects: zshOnly, Doc: "Files owned by the effective group."},
	{Name: "L", Dialects: zshOnly, Doc: "Files by size, as in `L+10` (over 10 bytes) or `Lk-5` (under 5 KiB)."},
	{Name: "m", Dialects: zshOnly, Doc: "Files by modification age, as in `mh-1` (changed within the last hour) or `md+7`."},
	{Name: "a", Dialects: zshOnly, Doc: "Files by access age, with the same units as `m`."},
	{Name: "N", Dialects: zshOnly, Doc: "Null glob: expand to nothing instead of failing when nothing matches."},
	{Name: "D", Dialects: zshOnly, Doc: "Include names starting with a dot."},
	{Name: "n", Dialects: zshOnly, Doc: "Sort numbers numerically."},
	{Name: "o", Dialects: zshOnly, Doc: "Sort ascending by the key that follows: `on` name, `oL` size, `om` modification time, `oa` access time."},
	{Name: "O", Dialects: zshOnly, Doc: "Sort descending by the key that follows, like `o`."},
	{Name: "[", Dialects: zshOnly, Doc: "Select a range of the sorted matches, as in `[1]` (the first) or `[1,5]`."},
	{Name: "^", Dialects: zshOnly, Doc: "Negate the qualifiers that follow."},
	{Name: "-", Dialects: zshOnly, Doc: "Follow symbolic links for the qualifiers that follow."},
	{Name: "e", Dialects: zshOnly, Doc: "Keep matches for which the given code succeeds, as in `e:'[[ -s $REPLY ]]':`."},
	{Name: "+", Dialects: zshOnly, Doc: "Keep matches for which the named function succeeds, with the match in `REPLY`."},
	{Name: "Y", Dialects: zshOnly, Doc: "Stop after the given number of matches, as in `Y1`."},
}

var printfDirectives = []Entry{
	{Name: "%s", Dialects: every, Doc: "The argument as a string."},
	{Name: "%b", Dialects: every, Doc: "The argument as a string, with backslash escapes such as `\\n` expanded."},
	{Name: "%c", Dialects: every, Doc: "The first character of the argument."},
	{Name: "%d", Dialects: every, Doc: "The argument as a signed decimal integer. A leading quote gives a character's code: `printf %d \"'A\"`."},
	{Name: "%i", Dialects: every, Doc: "Same as `%d`."},
	{Name: "%u", Dialects: every, Doc: "The argument as an unsigned decimal integer."},
	{Name: "%o", Dialects: every, Doc: "The argument as an unsigned octal integer."},
	{Name: "%x", Dialects: every, Doc: "The argument as an unsigned hexadecimal integer in lowercase."},
	{Name: "%X", Dialects: every, Doc: "The argument as an unsigned hexadecimal integer in uppercase."},
	{Name: "%f", Dialects: extended, Doc: "The argument as a decimal floating-point number; `%.2f` rounds to two decimals."},
	{Name: "%e", Dialects: extended, Doc: "The argument in scientific notation, such as `1.5e+03`."},
	{Name: "%g", Dialects: extended, Doc: "The argument as `%f` or `%e`, whichever is shorter."},
	{Name: "%q", Dialects: bashZshKsh, Doc: "The argument quoted so that the shell can read it back as one word."},
	{Name: "%(", Dialects: bashKsh, Synopsis: "%(%F)T", Doc: "The argument, seconds since the epoch, formatted as a date with the `strftime` format between the parentheses; `-1` means now."},
	{Name: "%%", Dialects: every, Doc: "A literal `%`."},
}

var redirections = []Entry{
	{Name: ">", Dialects: every, Synopsis: "[n]> file", Doc: "Redirects output (descriptor `n`, default 1) to `file`, truncating it first. Fails on an existing file when `noclobber` is set."},
	{Name: ">>", Dialects: every, Synopsis: "[n]>> file", Doc: "Redirects output (descriptor `n`, default 1) to `file`, appending to it."},
	{Name: "<", Dialects: every, Synopsis: "[n]< file", Doc: "Reads input (descriptor `n`, default 0) from `file`."},
	{Name: "<>", Dialects: every, Synopsis: "[n]<> file", Doc: "Opens `file` for reading and writing on descriptor `n` (default 0), creating it if needed."},
	{Name: ">|", Dialects: every, Synopsis: "[n]>| file", Doc: "Like `>`, but overwrites `file` even when `noclobber` is set."},
	{Name: ">&", Dialects: every, Synopsis: "[n]>&m", Doc: "Makes descriptor `n` (default 1) a copy of descriptor `m`, so `2>&1` sends errors where output goes. `>&-` closes the descriptor."},
	{Name: "<&", Dialects: every, Synopsis: "[n]<&m", Doc: "Makes descriptor `n` (default 0) a copy of input descriptor `m`; `<&-` closes it."},
	{Name: "&>", Dialects: bashZsh, Synopsis: "&> file", Doc: "Redirects both output and errors to `file`; the portable spelling is `> file 2>&1`."},
	{Name: "&>>", Dialects: bashZsh, Synopsis: "&>> file", Doc: "Appends both output and errors to `file`; the portable spelling is `>> file 2>&1`."},
	{Name: "<<", Dialects: every, Synopsis: "<< DELIM", Doc: "Here-document: the following lines up to `DELIM` become the input. Unless the delimiter is quoted, the body undergoes parameter, command and arithmetic expansion."},
	{Name: "<<-", Dialects: every, Synopsis: "<<- DELIM", Doc: "Here-document that strips leading tab characters (not spaces) from the body and the closing delimiter, so it can be indented with tabs."},
	{Name: "<<#", Dialects: kshOnly, Synopsis: "<<# DELIM", Doc: "Here-document that strips leading blanks (spaces and tabs) from the body and the closing delimiter, so it can be indented with either."},
	{Name: "<<<", Dialects: bashZshKsh, Synopsis: "<<< word", Doc: "Here-string: `word` is expanded and fed to standard input with a trailing newline."},
}

var arithmeticOperators = []Entry{
	{Name: "+", Dialects: every, Doc: "Addition, or unary plus."},
	{Name: "-", Dialects: every, Doc: "Subtraction, or negation."},
	{Name: "*", Dialects: every, Doc: "Multiplication."},
	{Name: "/", Dialects: every, Doc: "Integer division, truncating towards zero; dividing by zero is an error."},
	{Name: "%", Dialects: every, Doc: "Remainder of the integer division, with the sign of the dividend."},
	{Name: "**", Dialects: bashZshKsh, Doc: "Exponentiation."},
	{Name: "<<", Dialects: every, Doc: "Bitwise shift left."},
	{Name: ">>", Dialects: every, Doc: "Bitwise shift right."},
	{Name: "&", Dialects: every, Doc: "Bitwise and."},
	{Name: "|", Dialects: every, Doc: "Bitwise or."},
	{Name: "^", Dialects: every, Doc: "Bitwise exclusive or."},
	{Name: "~", Dialects: every, Doc: "Bitwise negation."},
	{Name: "!", Dialects: every, Doc: "Logical not: 1 for 0, 0 otherwise."},
	{Name: "&&", Dialects: every, Doc: "Logical and; the right side is evaluated only when the left one is non-zero."},
	{Name: "||", Dialects: every, Doc: "Logical or; the right side is evaluated only when the left one is zero."},
	{Name: "==", Dialects: every, Doc: "1 if both sides are equal, 0 otherwise."},
	{Name: "!=", Dialects: every, Doc: "1 if the sides differ, 0 otherwise."},
	{Name: "<", Dialects: every, Doc: "1 if the left side is smaller, 0 otherwise."},
	{Name: "<=", Dialects: every, Doc: "1 if the left side is smaller or equal, 0 otherwise."},
	{Name: ">", Dialects: every, Doc: "1 if the left side is greater, 0 otherwise."},
	{Name: ">=", Dialects: every, Doc: "1 if the left side is greater or equal, 0 otherwise."},
	{Name: "=", Dialects: every, Doc: "Assignment; the expression has the assigned value."},
	{Name: "+=", Dialects: every, Doc: "Adds the right side to the variable."},
	{Name: "-=", Dialects: every, Doc: "Subtracts the right side from the variable."},
	{Name: "*=", Dialects: every, Doc: "Multiplies the variable by the right side."},
	{Name: "/=", Dialects: every, Doc: "Divides the variable by the right side."},
	{Name: "%=", Dialects: every, Doc: "Replaces the variable with the remainder of dividing it by the right side."},
	{Name: "<<=", Dialects: every, Doc: "Shifts the variable left by the right side."},
	{Name: ">>=", Dialects: every, Doc: "Shifts the variable right by the right side."},
	{Name: "&=", Dialects: every, Doc: "Bitwise-ands the variable with the right side."},
	{Name: "|=", Dialects: every, Doc: "Bitwise-ors the variable with the right side."},
	{Name: "^=", Dialects: every, Doc: "Bitwise-xors the variable with the right side."},
	{Name: "++", Dialects: bashZshKsh, Doc: "Increment: `++i` yields the new value, `i++` the old one."},
	{Name: "--", Dialects: bashZshKsh, Doc: "Decrement: `--i` yields the new value, `i--` the old one."},
	{Name: "?", Dialects: every, Doc: "Conditional: `cond ? a : b` is `a` when `cond` is non-zero, `b` otherwise."},
	{Name: ":", Dialects: every, Doc: "Separates the branches of a `? :` conditional."},
	{Name: ",", Dialects: bashZshKsh, Doc: "Evaluates both sides and yields the right one."},
}

var listOperators = []Entry{
	{Name: "&&", Dialects: every, Synopsis: "cmd1 && cmd2", Doc: "Runs `cmd2` only if `cmd1` succeeds. The list fails if either command fails, but a failing `cmd1` does not trigger `set -e`."},
	{Name: "||", Dialects: every, Synopsis: "cmd1 || cmd2", Doc: "Runs `cmd2` only if `cmd1` fails. `cmd || exit` is the usual way to stop on an error."},
	{Name: "|", Dialects: every, Synopsis: "cmd1 | cmd2", Doc: "Pipes the output of `cmd1` into the input of `cmd2`. Each side runs in its own subshell (bash runs the last one in the current shell with `lastpipe`, zsh and ksh always do)."},
	{Name: "|&", Dialects: bashZsh, Synopsis: "cmd1 |& cmd2", Doc: "Pipes both the output and the errors of `cmd1` into `cmd2`, like `cmd1 2>&1 | cmd2`. In ksh, `cmd |&` starts a co-process instead."},
	{Name: "&", Dialects: every, Synopsis: "cmd &", Doc: "Runs `cmd` in the background; `$!` holds its process ID."},
	{Name: ";", Dialects: every, Synopsis: "cmd1; cmd2", Doc: "Runs the commands one after the other."},
	{Name: ";;", Dialects: every, Synopsis: "pattern) list ;;", Doc: "Ends a `case` branch; no further pattern is tried."},
	{Name: ";&", Dialects: bashZshKsh, Synopsis: "pattern) list ;&", Doc: "Ends a `case` branch and falls through into the next branch's commands without testing its pattern."},
	{Name: ";;&", Dialects: bashOnly, Synopsis: "pattern) list ;;&", Doc: "Ends a `case` branch and keeps testing the following patterns."},
	{Name: ";|", Dialects: zshOnly, Synopsis: "pattern) list ;|", Doc: "Ends a `case` branch and keeps testing the following patterns."},
}

var optionLetters = []Entry{
	{Name: "a", Dialects: every, Doc: "`allexport`: every assigned variable is exported."},
	{Name: "b", Dialects: every, Doc: "`notify`: job status changes are reported immediately."},
	{Name: "C", Dialects: every, Doc: "`noclobber`: `>` refuses to overwrite files."},
	{Name: "e", Dialects: every, Doc: "`errexit`: the shell exits when a command fails."},
	{Name: "f", Dialects: every, Doc: "`noglob`: filename generation is off."},
	{Name: "h", Dialects: every, Doc: "`hashall`: command locations are remembered."},
	{Name: "i", Dialects: every, Doc: "The shell is interactive; `case $- in *i*)` is the usual test."},
	{Name: "m", Dialects: every, Doc: "`monitor`: job control is on."},
	{Name: "n", Dialects: every, Doc: "`noexec`: commands are read but not run."},
	{Name: "u", Dialects: every, Doc: "`nounset`: expanding an unset variable is an error."},
	{Name: "v", Dialects: every, Doc: "`verbose`: input lines are printed as they are read."},
	{Name: "x", Dialects: every, Doc: "`xtrace`: commands are printed before they run."},
	{Name: "s", Dialects: every, Doc: "Commands are read from standard input."},
	{Name: "c", Dialects: every, Doc: "Commands came from the `-c` argument."},
	{Name: "B", Dialects: bashOnly, Doc: "`braceexpand`: brace expansion is on."},
	{Name: "H", Dialects: bashOnly, Doc: "`histexpand`: `!` history expansion is on."},
	{Name: "E", Dialects: bashOnly, Doc: "`errtrace`: the `ERR` trap is inherited by functions."},
	{Name: "T", Dialects: bashOnly, Doc: "`functrace`: the `DEBUG` and `RETURN` traps are inherited by functions."},
}
