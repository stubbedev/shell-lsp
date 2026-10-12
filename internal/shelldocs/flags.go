package shelldocs

import (
	"iter"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func BuiltinFlags(builtin string, d dialect.Dialect) (iter.Seq[Entry], bool) {
	b, ok := Builtin(builtin)
	if !ok || !b.Dialects.Has(d) {
		return nil, false
	}
	flags, ok := builtinFlags[builtin]
	if !ok {
		return nil, false
	}
	return func(yield func(Entry) bool) {
		for _, e := range flags {
			if e.Dialects.Has(d) && !yield(e) {
				return
			}
		}
	}, true
}

func BuiltinFlag(builtin, flag string, d dialect.Dialect) (Entry, bool) {
	flags, ok := BuiltinFlags(builtin, d)
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
	bashZshBusybox    = dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Busybox)
	bashBusybox       = dialect.SetOf(dialect.Bash, dialect.Busybox)
	bashZshKshBusybox = dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Ksh, dialect.Busybox)
	promptReaders     = dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Ksh, dialect.Dash, dialect.Busybox)
	notAsh            = dialect.SetOf(dialect.Posix, dialect.Bash, dialect.Zsh, dialect.Ksh)
	bashKshDash       = dialect.SetOf(dialect.Bash, dialect.Ksh, dialect.Dash)
	kshDash           = dialect.SetOf(dialect.Ksh, dialect.Dash)
	attributeFlags    = []Entry{
		{Name: "-a", Dialects: bashZshKsh, Doc: "Make the variable an indexed array."},
		{Name: "-A", Dialects: bashZshKsh, Doc: "Make the variable an associative array, indexed by strings."},
		{Name: "-i", Dialects: bashZshKsh, Doc: "Give the variable the integer attribute: assignments are evaluated as arithmetic."},
		{Name: "-r", Dialects: bashZshKsh, Doc: "Make the variable read-only."},
		{Name: "-x", Dialects: bashZshKsh, Doc: "Export the variable to the environment of commands."},
		{Name: "-l", Dialects: bashZshKsh, Doc: "Convert assigned values to lowercase."},
		{Name: "-u", Dialects: bashZshKsh, Doc: "Convert assigned values to uppercase."},
		{Name: "-n", Dialects: bashKsh, Doc: "Make the variable a name reference: reads and writes go to the variable it names."},
		{Name: "-t", Dialects: bashZshKsh, Doc: "Set the trace attribute; on a function, `DEBUG` and `RETURN` traps (bash) or tracing (zsh) apply inside it."},
		{Name: "-g", Dialects: bashZsh, Doc: "Inside a function, operate on the global variable instead of creating a local one."},
		{Name: "-f", Dialects: bashZshKsh, Doc: "Operate on functions instead of variables; alone, prints their definitions."},
		{Name: "-F", Dialects: bashZsh, Doc: "In bash, print only function names and attributes. In zsh, declare a floating-point variable printed in fixed notation."},
		{Name: "-p", Dialects: bashZshKsh, Doc: "Print the variables with their attributes, as commands that would recreate them."},
		{Name: "-I", Dialects: bashOnly, Doc: "For a new local variable, inherit the value and attributes of the variable with the same name in the calling scope."},
		{Name: "-E", Dialects: zshKsh, Doc: "Declare a floating-point variable printed in scientific notation."},
		{Name: "-Z", Dialects: zshKsh, Doc: "Right-justify and pad with leading zeros to the given width."},
		{Name: "-L", Dialects: zshKsh, Doc: "Left-justify to the given width, removing leading blanks."},
		{Name: "-R", Dialects: zshKsh, Doc: "Right-justify to the given width, padding with blanks."},
		{Name: "-T", Dialects: zshOnly, Doc: "Tie a scalar to an array, like `PATH` and `path`: `typeset -T SCALAR array [separator]`."},
		{Name: "-U", Dialects: zshOnly, Doc: "Keep only the first occurrence of each element of an array."},
		{Name: "-h", Dialects: zshOnly, Doc: "Hide a special parameter's special meaning in a local declaration."},
		{Name: "-H", Dialects: zshOnly, Doc: "Hide the value when the variable is printed."},
	}
	setFlags = []Entry{
		{Name: "-e", Dialects: every, Doc: "`errexit`: exit when a command fails outside a condition."},
		{Name: "-u", Dialects: every, Doc: "`nounset`: make the expansion of an unset variable an error."},
		{Name: "-x", Dialects: every, Doc: "`xtrace`: print each command, prefixed with `PS4`, before running it."},
		{Name: "-v", Dialects: every, Doc: "`verbose`: print input lines as they are read."},
		{Name: "-f", Dialects: every, Doc: "`noglob`: disable filename generation."},
		{Name: "-n", Dialects: every, Doc: "`noexec`: read commands without running them, to check the syntax."},
		{Name: "-a", Dialects: every, Doc: "`allexport`: export every variable that is assigned."},
		{Name: "-b", Dialects: every, Doc: "`notify`: report job status changes immediately."},
		{Name: "-C", Dialects: every, Doc: "`noclobber`: refuse to overwrite existing files with `>`."},
		{Name: "-h", Dialects: notAsh, Doc: "`hashall`: remember command locations as they are looked up."},
		{Name: "-m", Dialects: every, Doc: "`monitor`: enable job control."},
		{Name: "-o", Dialects: every, Doc: "Enable the option named by the next argument, such as `pipefail`; alone, list the options."},
		{Name: "-E", Dialects: bashOnly, Doc: "`errtrace`: let functions, command substitutions and subshells inherit the `ERR` trap."},
		{Name: "-T", Dialects: bashOnly, Doc: "`functrace`: let functions inherit the `DEBUG` and `RETURN` traps."},
		{Name: "-B", Dialects: bashOnly, Doc: "`braceexpand`: enable brace expansion (on by default)."},
		{Name: "-H", Dialects: bashOnly, Doc: "`histexpand`: enable `!` history expansion."},
		{Name: "-P", Dialects: bashOnly, Doc: "`physical`: resolve symbolic links in `cd` and `pwd`."},
		{Name: "-k", Dialects: bashZshKsh, Doc: "`keyword`: treat assignments anywhere on the command line as environment assignments."},
		{Name: "-A", Dialects: zshKsh, Doc: "Assign the remaining arguments to the named array: `set -A name values`."},
	}
	readFlags = []Entry{
		{Name: "-r", Dialects: every, Doc: "Raw input: backslashes are kept instead of escaping characters or joining lines. Almost always wanted."},
		{Name: "-p", Dialects: promptReaders, Doc: "In bash, dash and busybox sh, print the next argument as a prompt first. In zsh and ksh, read from the co-process instead."},
		{Name: "-a", Dialects: bashOnly, Doc: "Assign the fields to the indexed array named by the next argument."},
		{Name: "-A", Dialects: zshKsh, Doc: "Assign the fields to the array named by the next argument."},
		{Name: "-d", Dialects: bashZshKshBusybox, Doc: "Stop at the first character of the next argument instead of a newline; `-d ''` reads up to a NUL byte."},
		{Name: "-n", Dialects: bashBusybox, Doc: "Return after reading at most the given number of characters."},
		{Name: "-N", Dialects: bashKsh, Doc: "Read exactly the given number of characters, ignoring delimiters."},
		{Name: "-k", Dialects: zshOnly, Doc: "Read the given number of characters (one by default)."},
		{Name: "-q", Dialects: zshOnly, Doc: "Read one character and succeed only if it is `y` or `Y`."},
		{Name: "-s", Dialects: bashZshKshBusybox, Doc: "In bash, zsh and busybox sh, do not echo the input, for passwords. In ksh, save the line in the history."},
		{Name: "-t", Dialects: bashZshKshBusybox, Doc: "Fail if a complete line is not read within the given number of seconds."},
		{Name: "-u", Dialects: bashZshKshBusybox, Doc: "Read from the given file descriptor instead of standard input."},
		{Name: "-e", Dialects: bashZsh, Doc: "In bash, use readline to edit the line. In zsh, echo the words read instead of assigning them."},
		{Name: "-i", Dialects: bashOnly, Doc: "With `-e`, put the given text into the editing buffer first."},
	}
	builtinFlags = map[string][]Entry{
		"declare": attributeFlags,
		"typeset": attributeFlags,
		"local":   attributeFlags,
		"set":     setFlags,
		"read":    readFlags,
		"export": {
			{Name: "-p", Dialects: every, Doc: "Print the exported variables as commands that would recreate them."},
			{Name: "-n", Dialects: bashBusybox, Doc: "Remove the export attribute instead of setting it."},
			{Name: "-f", Dialects: bashOnly, Doc: "Export functions instead of variables."},
		},
		"readonly": {
			{Name: "-p", Dialects: every, Doc: "Print the read-only variables as commands that would recreate them."},
			{Name: "-a", Dialects: bashOnly, Doc: "Operate on indexed arrays."},
			{Name: "-A", Dialects: bashOnly, Doc: "Operate on associative arrays."},
			{Name: "-f", Dialects: bashOnly, Doc: "Make functions read-only instead of variables."},
		},
		"unset": {
			{Name: "-v", Dialects: every, Doc: "Unset variables (the default in POSIX shells)."},
			{Name: "-f", Dialects: every, Doc: "Unset functions."},
			{Name: "-n", Dialects: bashOnly, Doc: "Unset a name reference itself instead of the variable it points to."},
		},
		"printf": {
			{Name: "-v", Dialects: bashZsh, Doc: "Assign the output to the variable named by the next argument instead of printing it."},
		},
		"echo": {
			{Name: "-n", Dialects: every, Doc: "Do not print the trailing newline. Not portable: POSIX leaves `echo -n` unspecified; use `printf`."},
			{Name: "-e", Dialects: bashZshBusybox, Doc: "Interpret backslash escapes such as `\\n` and `\\t`."},
			{Name: "-E", Dialects: bashZshBusybox, Doc: "Do not interpret backslash escapes."},
		},
		"print": {
			{Name: "-r", Dialects: zshKsh, Doc: "Raw output: do not interpret backslash escapes."},
			{Name: "-n", Dialects: zshKsh, Doc: "Do not print the trailing newline."},
			{Name: "-u", Dialects: zshKsh, Doc: "Print to the given file descriptor."},
			{Name: "-p", Dialects: zshKsh, Doc: "Print to the co-process."},
			{Name: "-s", Dialects: zshKsh, Doc: "Add the arguments to the history instead of printing them."},
			{Name: "-R", Dialects: zshKsh, Doc: "Emulate BSD `echo`: no escapes, and only `-n` is recognised afterwards."},
			{Name: "-l", Dialects: zshOnly, Doc: "Print each argument on its own line."},
			{Name: "-N", Dialects: zshOnly, Doc: "Terminate each argument with a NUL byte."},
			{Name: "-P", Dialects: zshOnly, Doc: "Expand prompt escape sequences."},
			{Name: "-f", Dialects: zshOnly, Doc: "Format the arguments like `printf` with the given format."},
			{Name: "-v", Dialects: zshOnly, Doc: "Assign the output to the given variable instead of printing it."},
			{Name: "-z", Dialects: zshOnly, Doc: "Push the arguments onto the editing buffer stack."},
			{Name: "-c", Dialects: zshOnly, Doc: "Print the arguments in columns."},
			{Name: "-o", Dialects: zshOnly, Doc: "Sort the arguments in ascending order."},
			{Name: "-O", Dialects: zshOnly, Doc: "Sort the arguments in descending order."},
		},
		"trap": {
			{Name: "-l", Dialects: bashOnly, Doc: "List the signal names and numbers."},
			{Name: "-p", Dialects: bashKsh, Doc: "Print the trap commands, for the given conditions or for all of them."},
		},
		"kill": {
			{Name: "-s", Dialects: every, Doc: "Send the signal named by the next argument, such as `-s TERM`."},
			{Name: "-l", Dialects: every, Doc: "List signal names; with an exit status, name the signal that caused it."},
			{Name: "-n", Dialects: bashKsh, Doc: "Send the signal with the given number."},
			{Name: "-L", Dialects: bashOnly, Doc: "Same as `-l`."},
		},
		"cd": {
			{Name: "-L", Dialects: every, Doc: "Follow symbolic links logically, so `..` returns through them (the default)."},
			{Name: "-P", Dialects: every, Doc: "Resolve symbolic links, so the new directory is the physical one."},
			{Name: "-e", Dialects: bashOnly, Doc: "With `-P`, fail if the resulting directory cannot be determined."},
			{Name: "-q", Dialects: zshOnly, Doc: "Do not print the directory stack or run `chpwd` hooks."},
			{Name: "-s", Dialects: zshOnly, Doc: "Refuse to change into a path that contains symbolic links."},
		},
		"pwd": {
			{Name: "-L", Dialects: every, Doc: "Print the logical directory, as reached through symbolic links (the default)."},
			{Name: "-P", Dialects: every, Doc: "Print the physical directory, with symbolic links resolved."},
		},
		"type": {
			{Name: "-a", Dialects: bashZsh, Doc: "Report every interpretation of the name, not only the one that would run."},
			{Name: "-f", Dialects: bashZsh, Doc: "Skip shell functions."},
			{Name: "-p", Dialects: bashZshBusybox, Doc: "Print only the path of the file that would run."},
			{Name: "-P", Dialects: bashOnly, Doc: "Search `PATH` even when the name is an alias, builtin or function."},
			{Name: "-t", Dialects: bashOnly, Doc: "Print a single word: `alias`, `keyword`, `function`, `builtin` or `file`."},
			{Name: "-w", Dialects: zshOnly, Doc: "Print the name and a word for its kind."},
			{Name: "-s", Dialects: zshOnly, Doc: "Also print the target of symbolic links."},
		},
		"command": {
			{Name: "-v", Dialects: every, Doc: "Print the path or name that would run instead of running it; fails when nothing matches, which makes `command -v name >/dev/null` the portable existence test."},
			{Name: "-V", Dialects: every, Doc: "Describe how the name would be interpreted, verbosely."},
			{Name: "-p", Dialects: every, Doc: "Search a default `PATH` that finds the standard utilities."},
		},
		"hash": {
			{Name: "-r", Dialects: every, Doc: "Forget every remembered command location."},
			{Name: "-d", Dialects: bashOnly, Doc: "Forget the remembered location of the given names."},
			{Name: "-l", Dialects: bashOnly, Doc: "Print the table in a form that can be read back as input."},
			{Name: "-p", Dialects: bashOnly, Doc: "Remember the given path for the name instead of searching `PATH`."},
			{Name: "-t", Dialects: bashOnly, Doc: "Print the remembered location of each name."},
		},
		"wait": {
			{Name: "-n", Dialects: bashBusybox, Doc: "Wait for the next background job to finish and return its status."},
			{Name: "-f", Dialects: bashOnly, Doc: "With job control, wait for the job to terminate rather than change state."},
			{Name: "-p", Dialects: bashOnly, Doc: "Store the process ID of the job that finished in the variable named by the next argument."},
		},
		"jobs": {
			{Name: "-l", Dialects: every, Doc: "Also list process IDs."},
			{Name: "-p", Dialects: every, Doc: "List only the process group leaders' IDs."},
			{Name: "-n", Dialects: bashOnly, Doc: "List only jobs whose status changed since the last report."},
			{Name: "-r", Dialects: bashOnly, Doc: "List only running jobs."},
			{Name: "-s", Dialects: bashOnly, Doc: "List only stopped jobs."},
		},
		"ulimit": {
			{Name: "-f", Dialects: every, Doc: "Largest file the shell and its children may write, in 512-byte blocks."},
			{Name: "-a", Dialects: notPosix, Doc: "Print every current limit."},
			{Name: "-H", Dialects: notPosix, Doc: "Set or print the hard limit, which only root may raise."},
			{Name: "-S", Dialects: notPosix, Doc: "Set or print the soft limit, the one enforced."},
			{Name: "-c", Dialects: notPosix, Doc: "Largest core dump, in blocks; `ulimit -c 0` disables core dumps."},
			{Name: "-d", Dialects: notPosix, Doc: "Largest data segment, in KiB."},
			{Name: "-n", Dialects: notPosix, Doc: "Most open file descriptors."},
			{Name: "-s", Dialects: notPosix, Doc: "Largest stack, in KiB."},
			{Name: "-t", Dialects: notPosix, Doc: "Most CPU time, in seconds."},
			{Name: "-v", Dialects: notPosix, Doc: "Largest virtual memory, in KiB."},
			{Name: "-u", Dialects: bashZshKshBusybox, Doc: "Most processes for the user."},
			{Name: "-e", Dialects: bashZshKshBusybox, Doc: "Highest scheduling priority (nice value) a process may raise itself to."},
			{Name: "-i", Dialects: bashZshKshBusybox, Doc: "Most pending signals."},
			{Name: "-l", Dialects: notPosix, Doc: "Most memory that may be locked into RAM, in KiB."},
			{Name: "-m", Dialects: notPosix, Doc: "Largest resident set size, in KiB."},
			{Name: "-p", Dialects: bashKshDash, Doc: "Pipe buffer size, in 512-byte blocks.", Notes: map[dialect.Dialect]string{
				dialect.Dash: "dash's `-p` is the most processes for the user.",
			}},
			{Name: "-q", Dialects: bashZshKshBusybox, Doc: "Most bytes in POSIX message queues."},
			{Name: "-r", Dialects: notPosix, Doc: "Highest real-time scheduling priority."},
			{Name: "-w", Dialects: kshDash, Doc: "In dash, the most file locks. In ksh93, the largest swap area, in KiB."},
			{Name: "-x", Dialects: bashZshKshBusybox, Doc: "Most file locks."},
		},
		"umask": {
			{Name: "-S", Dialects: every, Doc: "Print or accept the mask in symbolic form, such as `u=rwx,g=rx,o=`."},
			{Name: "-p", Dialects: bashOnly, Doc: "Print the mask as a `umask` command that would restore it."},
		},
		"mapfile":   mapfileFlags,
		"readarray": mapfileFlags,
		"exec": {
			{Name: "-a", Dialects: bashZsh, Doc: "Pass the next argument to the command as its name (`$0`)."},
			{Name: "-c", Dialects: bashZsh, Doc: "Run the command with an empty environment."},
			{Name: "-l", Dialects: bashZsh, Doc: "Prefix the command's name with a dash, making it a login shell."},
		},
		"shopt": {
			{Name: "-s", Dialects: bashOnly, Doc: "Enable the named options."},
			{Name: "-u", Dialects: bashOnly, Doc: "Disable the named options."},
			{Name: "-q", Dialects: bashOnly, Doc: "Print nothing; the exit status tells whether every named option is on."},
			{Name: "-p", Dialects: bashOnly, Doc: "Print the options as `shopt` commands that would restore them."},
			{Name: "-o", Dialects: bashOnly, Doc: "Operate on the `set -o` options instead."},
		},
		"unalias": {
			{Name: "-a", Dialects: every, Doc: "Remove every alias."},
		},
		"alias": {
			{Name: "-p", Dialects: bashOnly, Doc: "Print the aliases as `alias` commands."},
			{Name: "-g", Dialects: zshOnly, Doc: "Define a global alias, expanded anywhere on the command line."},
			{Name: "-s", Dialects: zshOnly, Doc: "Define a suffix alias: `alias -s txt=less` opens `*.txt` files typed as commands."},
			{Name: "-L", Dialects: zshOnly, Doc: "Print the aliases as `alias` commands."},
		},
		"dirs": {
			{Name: "-c", Dialects: bashZsh, Doc: "Clear the directory stack."},
			{Name: "-l", Dialects: bashZsh, Doc: "Print full paths instead of abbreviating the home directory to `~`."},
			{Name: "-p", Dialects: bashZsh, Doc: "Print one entry per line."},
			{Name: "-v", Dialects: bashZsh, Doc: "Print one entry per line, numbered."},
		},
		"pushd": {
			{Name: "-n", Dialects: bashOnly, Doc: "Change the stack without changing directory."},
		},
		"popd": {
			{Name: "-n", Dialects: bashOnly, Doc: "Change the stack without changing directory."},
		},
		"disown": {
			{Name: "-a", Dialects: bashOnly, Doc: "Act on every job."},
			{Name: "-h", Dialects: bashOnly, Doc: "Keep the job in the table but do not send it `SIGHUP` when the shell exits."},
			{Name: "-r", Dialects: bashOnly, Doc: "Act only on running jobs."},
		},
		"enable": {
			{Name: "-n", Dialects: bashOnly, Doc: "Disable the named builtins, so commands of the same name in `PATH` run instead."},
			{Name: "-a", Dialects: bashOnly, Doc: "List every builtin with its state."},
			{Name: "-p", Dialects: bashOnly, Doc: "Print the builtins in a reusable form."},
		},
		"whence": {
			{Name: "-v", Dialects: zshKsh, Doc: "Describe each name verbosely."},
			{Name: "-p", Dialects: zshKsh, Doc: "Search `PATH` even for aliases, builtins and functions."},
			{Name: "-a", Dialects: zshKsh, Doc: "Report every interpretation, not only the first."},
			{Name: "-c", Dialects: zshOnly, Doc: "Print in csh `which` style."},
			{Name: "-w", Dialects: zshOnly, Doc: "Print the name and a word for its kind."},
		},
		"autoload": {
			{Name: "-U", Dialects: zshOnly, Doc: "Do not expand aliases while loading the function."},
			{Name: "-z", Dialects: zshOnly, Doc: "Load the function in native zsh style, whatever `ksh_autoload` says."},
			{Name: "-k", Dialects: zshOnly, Doc: "Load the function in ksh style."},
			{Name: "-X", Dialects: zshOnly, Doc: "Inside a function, replace it with its autoloaded definition and run that."},
		},
		"emulate": {
			{Name: "-L", Dialects: zshOnly, Doc: "Make the option changes local to the current function, along with traps."},
			{Name: "-R", Dialects: zshOnly, Doc: "Reset every option to the emulated shell's defaults, not only the ones that affect compatibility."},
		},
	}
	mapfileFlags = []Entry{
		{Name: "-t", Dialects: bashOnly, Doc: "Remove the trailing delimiter from each line. Almost always wanted."},
		{Name: "-d", Dialects: bashOnly, Doc: "Split on the first character of the next argument instead of newlines; `-d ''` splits on NUL bytes."},
		{Name: "-n", Dialects: bashOnly, Doc: "Read at most the given number of lines."},
		{Name: "-O", Dialects: bashOnly, Doc: "Start assigning at the given index instead of 0, without clearing the array."},
		{Name: "-s", Dialects: bashOnly, Doc: "Skip the given number of leading lines."},
		{Name: "-u", Dialects: bashOnly, Doc: "Read from the given file descriptor instead of standard input."},
		{Name: "-C", Dialects: bashOnly, Doc: "Call the given command every `-c` lines."},
		{Name: "-c", Dialects: bashOnly, Doc: "Number of lines between `-C` callbacks (5000 by default)."},
	}
)
