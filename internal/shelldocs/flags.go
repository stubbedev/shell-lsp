package shelldocs

import (
	"iter"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func BuiltinFlags(builtin string, s dialect.Shell) (iter.Seq[Entry], bool) {
	b, ok := Builtin(builtin)
	if !ok || !b.In(s) {
		return nil, false
	}
	flags, ok := builtinFlags[builtin]
	if !ok {
		return nil, false
	}
	return func(yield func(Entry) bool) {
		for _, e := range flags {
			if e.In(s) && !yield(e) {
				return
			}
		}
	}, true
}

func BuiltinFlag(builtin, flag string, s dialect.Shell) (Entry, bool) {
	flags, ok := BuiltinFlags(builtin, s)
	if !ok {
		return Entry{}, false
	}
	for e := range flags {
		if e.Name == flag {
			return e, true
		}
	}
	return Entry{}, false
}

var (
	bashZshBusybox    = dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Busybox).Shells()
	bashBusybox       = dialect.SetOf(dialect.Bash, dialect.Busybox).Shells()
	bashZshKshBusybox = dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Ksh, dialect.Busybox).Shells()
	promptReaders     = dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Ksh, dialect.Dash, dialect.Busybox).Shells()
	bashKshDash       = dialect.SetOf(dialect.Bash, dialect.Ksh, dialect.Dash).Shells()
	kshDash           = dialect.SetOf(dialect.Ksh, dialect.Dash).Shells()
	attributeFlags    = []Entry{
		{Name: "-a", shells: bashZshKsh, Doc: "Make the variable an indexed array."},
		{Name: "-A", shells: bashZshKsh, Doc: "Make the variable an associative array, indexed by strings."},
		{Name: "-i", shells: bashZshKsh, Doc: "Give the variable the integer attribute: assignments are evaluated as arithmetic."},
		{Name: "-r", shells: bashZshKsh, Doc: "Make the variable read-only."},
		{Name: "-x", shells: bashZshKsh, Doc: "Export the variable to the environment of commands."},
		{Name: "-l", shells: bashZshKsh, Doc: "Convert assigned values to lowercase."},
		{Name: "-u", shells: bashZshKsh, Doc: "Convert assigned values to uppercase."},
		{Name: "-n", shells: bashKsh, Doc: "Make the variable a name reference: reads and writes go to the variable it names."},
		{Name: "-t", shells: bashZshKsh, Doc: "Set the trace attribute; on a function, `DEBUG` and `RETURN` traps (bash) or tracing (zsh) apply inside it."},
		{Name: "-g", shells: bashZsh, Doc: "Inside a function, operate on the global variable instead of creating a local one."},
		{Name: "-f", shells: bashZshKsh, Doc: "Operate on functions instead of variables; alone, prints their definitions."},
		{Name: "-F", shells: bashZsh, Doc: "In bash, print only function names and attributes. In zsh, declare a floating-point variable printed in fixed notation."},
		{Name: "-p", shells: bashZshKsh, Doc: "Print the variables with their attributes, as commands that would recreate them."},
		{Name: "-I", shells: bashOnly, Doc: "For a new local variable, inherit the value and attributes of the variable with the same name in the calling scope."},
		{Name: "-E", shells: zshKsh, Doc: "Declare a floating-point variable printed in scientific notation."},
		{Name: "-Z", shells: zshKsh, Doc: "Right-justify and pad with leading zeros to the given width."},
		{Name: "-L", shells: zshKsh, Doc: "Left-justify to the given width, removing leading blanks."},
		{Name: "-R", shells: zshKsh, Doc: "Right-justify to the given width, padding with blanks."},
		{Name: "-T", shells: zshOnly, Doc: "Tie a scalar to an array, like `PATH` and `path`: `typeset -T SCALAR array [separator]`."},
		{Name: "-U", shells: zshOnly, Doc: "Keep only the first occurrence of each element of an array."},
		{Name: "-h", shells: zshOnly, Doc: "Hide a special parameter's special meaning in a local declaration."},
		{Name: "-H", shells: zshOnly, Doc: "Hide the value when the variable is printed."},
	}
	setFlags = append(letterEntries(func(letter string) string { return "-" + letter }),
		Entry{Name: "-o", shells: every, Doc: "Enable the option named by the next argument, such as `pipefail`; alone, list the options."},
		Entry{Name: "-A", shells: zshKsh, Doc: "Assign the remaining arguments to the named array: `set -A name values`."},
		Entry{Name: "-s", shells: zshKsh, Doc: "Sort the remaining arguments before assigning them to the positional parameters: `set -s -- b a` sets `a b`."},
	)
	readFlags = []Entry{
		{Name: "-r", shells: every, Doc: "Raw input: backslashes are kept instead of escaping characters or joining lines. Almost always wanted."},
		{Name: "-p", shells: promptReaders, Doc: "In bash, dash and busybox sh, print the next argument as a prompt first. In zsh and ksh, read from the co-process instead."},
		{Name: "-a", shells: bashOnly, Doc: "Assign the fields to the indexed array named by the next argument."},
		{Name: "-A", shells: zshKsh, Doc: "Assign the fields to the array named by the next argument."},
		{Name: "-d", shells: bashZshKshBusybox, Doc: "Stop at the first character of the next argument instead of a newline; `-d ''` reads up to a NUL byte."},
		{Name: "-n", shells: bashBusybox, Doc: "Return after reading at most the given number of characters."},
		{Name: "-N", shells: bashKsh, Doc: "Read exactly the given number of characters, ignoring delimiters."},
		{Name: "-k", shells: zshOnly, Doc: "Read the given number of characters (one by default)."},
		{Name: "-q", shells: zshOnly, Doc: "Read one character and succeed only if it is `y` or `Y`."},
		{Name: "-s", shells: bashZshKshBusybox, Doc: "In bash, zsh and busybox sh, do not echo the input, for passwords. In ksh, save the line in the history."},
		{Name: "-t", shells: bashZshKshBusybox, Doc: "Fail if a complete line is not read within the given number of seconds."},
		{Name: "-u", shells: bashZshKshBusybox, Doc: "Read from the given file descriptor instead of standard input."},
		{Name: "-e", shells: bashZsh, Doc: "In bash, use readline to edit the line. In zsh, echo the words read instead of assigning them."},
		{Name: "-i", shells: bashOnly, Doc: "With `-e`, put the given text into the editing buffer first."},
	}
	builtinFlags = map[string][]Entry{
		"declare": attributeFlags,
		"typeset": attributeFlags,
		"local":   attributeFlags,
		"set":     setFlags,
		"read":    readFlags,
		"export": {
			{Name: "-p", shells: every, Doc: "Print the exported variables as commands that would recreate them."},
			{Name: "-n", shells: bashBusybox, Doc: "Remove the export attribute instead of setting it."},
			{Name: "-f", shells: bashOnly, Doc: "Export functions instead of variables."},
		},
		"readonly": {
			{Name: "-p", shells: every, Doc: "Print the read-only variables as commands that would recreate them."},
			{Name: "-a", shells: bashOnly, Doc: "Operate on indexed arrays."},
			{Name: "-A", shells: bashOnly, Doc: "Operate on associative arrays."},
			{Name: "-f", shells: bashOnly, Doc: "Make functions read-only instead of variables."},
		},
		"unset": {
			{Name: "-v", shells: every, Doc: "Unset variables (the default in POSIX shells)."},
			{Name: "-f", shells: every, Doc: "Unset functions."},
			{Name: "-n", shells: bashOnly, Doc: "Unset a name reference itself instead of the variable it points to."},
		},
		"printf": {
			{Name: "-v", shells: bashZsh, Doc: "Assign the output to the variable named by the next argument instead of printing it."},
		},
		"echo": {
			{Name: "-n", shells: every, Doc: "Do not print the trailing newline. Not portable: POSIX leaves `echo -n` unspecified; use `printf`."},
			{Name: "-e", shells: bashZshBusybox, Doc: "Interpret backslash escapes such as `\\n` and `\\t`."},
			{Name: "-E", shells: bashZshBusybox, Doc: "Do not interpret backslash escapes."},
		},
		"print": {
			{Name: "-r", shells: zshKsh, Doc: "Raw output: do not interpret backslash escapes."},
			{Name: "-n", shells: zshKsh, Doc: "Do not print the trailing newline."},
			{Name: "-u", shells: zshKsh, Doc: "Print to the given file descriptor."},
			{Name: "-p", shells: zshKsh, Doc: "Print to the co-process."},
			{Name: "-s", shells: zshKsh, Doc: "Add the arguments to the history instead of printing them."},
			{Name: "-R", shells: zshKsh, Doc: "Emulate BSD `echo`: no escapes, and only `-n` is recognised afterwards."},
			{Name: "-l", shells: zshOnly, Doc: "Print each argument on its own line."},
			{Name: "-N", shells: zshOnly, Doc: "Terminate each argument with a NUL byte."},
			{Name: "-P", shells: zshOnly, Doc: "Expand prompt escape sequences."},
			{Name: "-f", shells: zshOnly, Doc: "Format the arguments like `printf` with the given format."},
			{Name: "-v", shells: zshOnly, Doc: "Assign the output to the given variable instead of printing it."},
			{Name: "-z", shells: zshOnly, Doc: "Push the arguments onto the editing buffer stack."},
			{Name: "-c", shells: zshOnly, Doc: "Print the arguments in columns."},
			{Name: "-o", shells: zshOnly, Doc: "Sort the arguments in ascending order."},
			{Name: "-O", shells: zshOnly, Doc: "Sort the arguments in descending order."},
		},
		"trap": {
			{Name: "-l", shells: bashOnly, Doc: "List the signal names and numbers."},
			{Name: "-p", shells: bashKsh, Doc: "Print the trap commands, for the given conditions or for all of them."},
		},
		"kill": {
			{Name: "-s", shells: every, Doc: "Send the signal named by the next argument, such as `-s TERM`."},
			{Name: "-l", shells: every, Doc: "List signal names; with an exit status, name the signal that caused it."},
			{Name: "-n", shells: bashKsh, Doc: "Send the signal with the given number."},
			{Name: "-L", shells: bashOnly, Doc: "Same as `-l`."},
		},
		"cd": {
			{Name: "-L", shells: every, Doc: "Follow symbolic links logically, so `..` returns through them (the default)."},
			{Name: "-P", shells: every, Doc: "Resolve symbolic links, so the new directory is the physical one."},
			{Name: "-e", shells: bashOnly, Doc: "With `-P`, fail if the resulting directory cannot be determined."},
			{Name: "-q", shells: zshOnly, Doc: "Do not print the directory stack or run `chpwd` hooks."},
			{Name: "-s", shells: zshOnly, Doc: "Refuse to change into a path that contains symbolic links."},
		},
		"pwd": {
			{Name: "-L", shells: every, Doc: "Print the logical directory, as reached through symbolic links (the default)."},
			{Name: "-P", shells: every, Doc: "Print the physical directory, with symbolic links resolved."},
		},
		"type": {
			{Name: "-a", shells: bashZsh, Doc: "Report every interpretation of the name, not only the one that would run."},
			{Name: "-f", shells: bashZsh, Doc: "Skip shell functions."},
			{Name: "-p", shells: bashZshBusybox, Doc: "Print only the path of the file that would run."},
			{Name: "-P", shells: bashOnly, Doc: "Search `PATH` even when the name is an alias, builtin or function."},
			{Name: "-t", shells: bashOnly, Doc: "Print a single word: `alias`, `keyword`, `function`, `builtin` or `file`."},
			{Name: "-w", shells: zshOnly, Doc: "Print the name and a word for its kind."},
			{Name: "-s", shells: zshOnly, Doc: "Also print the target of symbolic links."},
		},
		"command": {
			{Name: "-v", shells: every, Doc: "Print the path or name that would run instead of running it; fails when nothing matches, which makes `command -v name >/dev/null` the portable existence test."},
			{Name: "-V", shells: every, Doc: "Describe how the name would be interpreted, verbosely."},
			{Name: "-p", shells: every, Doc: "Search a default `PATH` that finds the standard utilities."},
		},
		"hash": {
			{Name: "-r", shells: every, Doc: "Forget every remembered command location."},
			{Name: "-d", shells: bashOnly, Doc: "Forget the remembered location of the given names."},
			{Name: "-l", shells: bashOnly, Doc: "Print the table in a form that can be read back as input."},
			{Name: "-p", shells: bashOnly, Doc: "Remember the given path for the name instead of searching `PATH`."},
			{Name: "-t", shells: bashOnly, Doc: "Print the remembered location of each name."},
		},
		"wait": {
			{Name: "-n", shells: bashBusybox, Doc: "Wait for the next background job to finish and return its status."},
			{Name: "-f", shells: bashOnly, Doc: "With job control, wait for the job to terminate rather than change state."},
			{Name: "-p", shells: bashOnly, Doc: "Store the process ID of the job that finished in the variable named by the next argument."},
		},
		"jobs": {
			{Name: "-l", shells: every, Doc: "Also list process IDs."},
			{Name: "-p", shells: every, Doc: "List only the process group leaders' IDs."},
			{Name: "-n", shells: bashOnly, Doc: "List only jobs whose status changed since the last report."},
			{Name: "-r", shells: bashOnly, Doc: "List only running jobs."},
			{Name: "-s", shells: bashOnly, Doc: "List only stopped jobs."},
		},
		"ulimit": {
			{Name: "-f", shells: every, Doc: "Largest file the shell and its children may write, in 512-byte blocks."},
			{Name: "-a", shells: notPosix, Doc: "Print every current limit."},
			{Name: "-H", shells: notPosix, Doc: "Set or print the hard limit, which only root may raise."},
			{Name: "-S", shells: notPosix, Doc: "Set or print the soft limit, the one enforced."},
			{Name: "-c", shells: notPosix, Doc: "Largest core dump, in blocks; `ulimit -c 0` disables core dumps."},
			{Name: "-d", shells: notPosix, Doc: "Largest data segment, in KiB."},
			{Name: "-n", shells: notPosix, Doc: "Most open file descriptors."},
			{Name: "-s", shells: notPosix, Doc: "Largest stack, in KiB."},
			{Name: "-t", shells: notPosix, Doc: "Most CPU time, in seconds."},
			{Name: "-v", shells: notPosix, Doc: "Largest virtual memory, in KiB."},
			{Name: "-u", shells: bashZshKshBusybox, Doc: "Most processes for the user."},
			{Name: "-e", shells: bashZshKshBusybox, Doc: "Highest scheduling priority (nice value) a process may raise itself to."},
			{Name: "-i", shells: bashZshKshBusybox, Doc: "Most pending signals."},
			{Name: "-l", shells: notPosix, Doc: "Most memory that may be locked into RAM, in KiB."},
			{Name: "-m", shells: notPosix, Doc: "Largest resident set size, in KiB."},
			{Name: "-p", shells: bashKshDash, Doc: "Pipe buffer size, in 512-byte blocks.", Notes: map[dialect.Dialect]string{
				dialect.Dash: "dash's `-p` is the most processes for the user.",
			}},
			{Name: "-q", shells: bashZshKshBusybox, Doc: "Most bytes in POSIX message queues."},
			{Name: "-r", shells: notPosix, Doc: "Highest real-time scheduling priority."},
			{Name: "-w", shells: kshDash, Doc: "In dash, the most file locks. In ksh93, the largest swap area, in KiB."},
			{Name: "-x", shells: bashZshKshBusybox, Doc: "Most file locks."},
		},
		"umask": {
			{Name: "-S", shells: every, Doc: "Print or accept the mask in symbolic form, such as `u=rwx,g=rx,o=`."},
			{Name: "-p", shells: bashOnly, Doc: "Print the mask as a `umask` command that would restore it."},
		},
		"mapfile":   mapfileFlags,
		"readarray": mapfileFlags,
		"exec": {
			{Name: "-a", shells: bashZshKshBusybox, Doc: "Pass the next argument to the command as its name (`$0`)."},
			{Name: "-c", shells: bashZshKsh, Doc: "Run the command with an empty environment."},
			{Name: "-l", shells: bashZsh, Doc: "Prefix the command's name with a dash, making it a login shell."},
		},
		"shopt": {
			{Name: "-s", shells: bashOnly, Doc: "Enable the named options."},
			{Name: "-u", shells: bashOnly, Doc: "Disable the named options."},
			{Name: "-q", shells: bashOnly, Doc: "Print nothing; the exit status tells whether every named option is on."},
			{Name: "-p", shells: bashOnly, Doc: "Print the options as `shopt` commands that would restore them."},
			{Name: "-o", shells: bashOnly, Doc: "Operate on the `set -o` options instead."},
		},
		"unalias": {
			{Name: "-a", shells: every, Doc: "Remove every alias."},
		},
		"alias": {
			{Name: "-p", shells: bashOnly, Doc: "Print the aliases as `alias` commands."},
			{Name: "-g", shells: zshOnly, Doc: "Define a global alias, expanded anywhere on the command line."},
			{Name: "-s", shells: zshOnly, Doc: "Define a suffix alias: `alias -s txt=less` opens `*.txt` files typed as commands."},
			{Name: "-L", shells: zshOnly, Doc: "Print the aliases as `alias` commands."},
		},
		"dirs": {
			{Name: "-c", shells: bashZsh, Doc: "Clear the directory stack."},
			{Name: "-l", shells: bashZsh, Doc: "Print full paths instead of abbreviating the home directory to `~`."},
			{Name: "-p", shells: bashZsh, Doc: "Print one entry per line."},
			{Name: "-v", shells: bashZsh, Doc: "Print one entry per line, numbered."},
		},
		"pushd": {
			{Name: "-n", shells: bashOnly, Doc: "Change the stack without changing directory."},
		},
		"popd": {
			{Name: "-n", shells: bashOnly, Doc: "Change the stack without changing directory."},
		},
		"disown": {
			{Name: "-a", shells: bashOnly, Doc: "Act on every job."},
			{Name: "-h", shells: bashOnly, Doc: "Keep the job in the table but do not send it `SIGHUP` when the shell exits."},
			{Name: "-r", shells: bashOnly, Doc: "Act only on running jobs."},
		},
		"enable": {
			{Name: "-n", shells: bashOnly, Doc: "Disable the named builtins, so commands of the same name in `PATH` run instead."},
			{Name: "-a", shells: bashOnly, Doc: "List every builtin with its state."},
			{Name: "-p", shells: bashOnly, Doc: "Print the builtins in a reusable form."},
		},
		"whence": {
			{Name: "-v", shells: zshKsh, Doc: "Describe each name verbosely."},
			{Name: "-p", shells: zshKsh, Doc: "Search `PATH` even for aliases, builtins and functions."},
			{Name: "-a", shells: zshKsh, Doc: "Report every interpretation, not only the first."},
			{Name: "-c", shells: zshOnly, Doc: "Print in csh `which` style."},
			{Name: "-w", shells: zshOnly, Doc: "Print the name and a word for its kind."},
		},
		"autoload": {
			{Name: "-U", shells: zshOnly, Doc: "Do not expand aliases while loading the function."},
			{Name: "-z", shells: zshOnly, Doc: "Load the function in native zsh style, whatever `ksh_autoload` says."},
			{Name: "-k", shells: zshOnly, Doc: "Load the function in ksh style."},
			{Name: "-X", shells: zshOnly, Doc: "Inside a function, replace it with its autoloaded definition and run that."},
		},
		"emulate": {
			{Name: "-L", shells: zshOnly, Doc: "Make the option changes local to the current function, along with traps."},
			{Name: "-R", shells: zshOnly, Doc: "Reset every option to the emulated shell's defaults, not only the ones that affect compatibility."},
		},
	}
	mapfileFlags = []Entry{
		{Name: "-t", shells: bashOnly, Doc: "Remove the trailing delimiter from each line. Almost always wanted."},
		{Name: "-d", shells: bashOnly, Doc: "Split on the first character of the next argument instead of newlines; `-d ''` splits on NUL bytes."},
		{Name: "-n", shells: bashOnly, Doc: "Read at most the given number of lines."},
		{Name: "-O", shells: bashOnly, Doc: "Start assigning at the given index instead of 0, without clearing the array."},
		{Name: "-s", shells: bashOnly, Doc: "Skip the given number of leading lines."},
		{Name: "-u", shells: bashOnly, Doc: "Read from the given file descriptor instead of standard input."},
		{Name: "-C", shells: bashOnly, Doc: "Call the given command every `-c` lines."},
		{Name: "-c", shells: bashOnly, Doc: "Number of lines between `-C` callbacks (5000 by default)."},
	}
)
