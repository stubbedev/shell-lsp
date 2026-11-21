package shelldocs

import "github.com/stubbedev/shell-lsp/internal/dialect"

var (
	every      = dialect.All.Shells()
	bashZshKsh = dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Ksh).Shells()
	bashZsh    = dialect.SetOf(dialect.Bash, dialect.Zsh).Shells()
	bashOnly   = dialect.SetOf(dialect.Bash).Shells()
	zshOnly    = dialect.SetOf(dialect.Zsh).Shells()
	zshKsh     = dialect.SetOf(dialect.Zsh, dialect.Ksh).Shells()
	localShell = dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Dash, dialect.Ksh, dialect.Busybox).Shells()
)

var builtins = []Entry{
	{Name: ":", shells: every, Synopsis: ": [arguments]", Doc: "Null command. Expands its arguments, performs redirections and returns 0."},
	{Name: ".", shells: every, Synopsis: ". file [arguments]", Doc: "Reads and executes commands from `file` in the current shell. The file is searched for in `PATH` when it contains no slash."},
	{Name: "source", shells: bashZsh, Synopsis: "source file [arguments]", Doc: "Synonym for `.`: executes `file` in the current shell.", Notes: map[dialect.Dialect]string{
		dialect.Zsh: "zsh searches the current directory before `PATH`, unlike `.`.",
	}},
	{Name: "alias", shells: every, Synopsis: "alias [name[=value] ...]", Doc: "Defines or prints aliases. Without arguments, prints every alias.", Notes: map[dialect.Dialect]string{
		dialect.Zsh: "`alias -g` defines global aliases expanded anywhere on the line; `alias -s` defines suffix aliases.",
	}},
	{Name: "unalias", shells: every, Synopsis: "unalias [-a] name ...", Doc: "Removes aliases; `-a` removes all of them."},
	{Name: "break", shells: every, Synopsis: "break [n]", Doc: "Exits from the innermost `for`, `while` or `until` loop, or from `n` enclosing loops."},
	{Name: "continue", shells: every, Synopsis: "continue [n]", Doc: "Resumes the next iteration of the innermost enclosing loop, or of the `n`th enclosing loop."},
	{Name: "cd", shells: every, Synopsis: "cd [-L|-P] [dir]", Doc: "Changes the working directory to `dir`, or to `$HOME` without an argument. `cd -` returns to `$OLDPWD` and prints it; `CDPATH` is searched for relative names.", Notes: map[dialect.Dialect]string{
		dialect.Zsh:  "`cd old new` substitutes `old` with `new` in `$PWD`; `cd -N`/`cd +N` select entries from the directory stack.",
		dialect.Bash: "`cd -e` with `-P` fails if the resulting directory cannot be determined.",
	}},
	{Name: "pwd", shells: every, Synopsis: "pwd [-L|-P]", Doc: "Prints the current working directory; `-P` resolves symbolic links."},
	{Name: "echo", shells: every, Synopsis: "echo [args ...]", Doc: "Writes its arguments separated by spaces, followed by a newline.", Notes: map[dialect.Dialect]string{
		dialect.Bash:  "Escape sequences are only interpreted with `-e` (or `shopt -s xpg_echo`); `-n` suppresses the newline.",
		dialect.Zsh:   "Interprets escape sequences by default (`-E` disables them); prefer `print -r --` for literal output.",
		dialect.Posix: "Behaviour with options and backslashes is implementation-defined; use `printf` for portable output.",
		dialect.Dash:  "Always interprets backslash escapes and has no `-e`; only `-n` is recognised.",
	}},
	{Name: "printf", shells: every, Synopsis: "printf format [arguments ...]", Doc: "Formats and prints `arguments` under control of `format`, reusing the format until all arguments are consumed. No newline is added.", Notes: map[dialect.Dialect]string{
		dialect.Bash: "`printf -v var` assigns the result to `var` instead of printing it; `%q` quotes for reuse as shell input.",
		dialect.Zsh:  "`printf -v var` assigns to `var`; `%q` quotes for reuse as shell input.",
	}},
	{Name: "print", shells: zshKsh, Synopsis: "print [-rnl] [-u fd] [--] [args ...]", Doc: "Prints its arguments like `echo`. `-r` disables escape processing, `-n` omits the newline, `-l` prints one argument per line, `-u fd` writes to a file descriptor.", Notes: map[dialect.Dialect]string{
		dialect.Zsh: "Also supports `-P` for prompt expansion, `-z` to push onto the editing buffer and `-v var` to assign instead of print.",
	}},
	{Name: "eval", shells: every, Synopsis: "eval [arguments ...]", Doc: "Concatenates its arguments and executes the result as shell input in the current shell."},
	{Name: "exec", shells: every, Synopsis: "exec [command [arguments ...]]", Doc: "Replaces the shell with `command`. Without a command, applies its redirections to the current shell.", Notes: map[dialect.Dialect]string{
		dialect.Bash: "`-a name` sets argv[0], `-c` clears the environment, `-l` prefixes argv[0] with a dash.",
	}},
	{Name: "exit", shells: every, Synopsis: "exit [n]", Doc: "Exits the shell with status `n`, or with the status of the last command."},
	{Name: "return", shells: every, Synopsis: "return [n]", Doc: "Returns from a function or a sourced file with status `n`, or with the status of the last command."},
	{Name: "export", shells: every, Synopsis: "export [-p] name[=value] ...", Doc: "Marks variables for export to the environment of subsequently executed commands, optionally assigning them.", Notes: map[dialect.Dialect]string{
		dialect.Bash: "`-n` removes the export attribute; `-f` exports functions.",
	}},
	{Name: "readonly", shells: every, Synopsis: "readonly [-p] name[=value] ...", Doc: "Marks variables read-only, optionally assigning them first. Later assignments and `unset` fail."},
	{Name: "unset", shells: every, Synopsis: "unset [-f|-v] name ...", Doc: "Removes variables (`-v`, the default) or functions (`-f`)."},
	{Name: "set", shells: every, Synopsis: "set [-+abefhkmnuvxC] [-o option] [--] [args ...]", Doc: "Sets shell options or the positional parameters. Common options: `-e` exit on error, `-u` error on unset variables, `-x` trace commands, `-o pipefail` fail a pipeline when any stage fails.", Notes: map[dialect.Dialect]string{
		dialect.Posix: "`-o pipefail` was only standardised in POSIX.1-2024; older `sh` implementations reject it.",
		dialect.Zsh:   "Prefer `setopt`; `set -A name values` assigns an array.",
	}},
	{Name: "shift", shells: every, Synopsis: "shift [n]", Doc: "Discards the first `n` (default 1) positional parameters and renumbers the rest."},
	{Name: "trap", shells: every, Synopsis: "trap [action] [condition ...]", Doc: "Runs `action` when the shell receives one of the signals, or on `EXIT`. An empty action ignores the signal; `-` restores the default.", Notes: map[dialect.Dialect]string{
		dialect.Bash: "Also accepts the `ERR`, `DEBUG` and `RETURN` pseudo-signals.",
		dialect.Zsh:  "Also accepts `ZERR` and `DEBUG`; `TRAPNAL()` functions are an alternative.",
	}},
	{Name: "times", shells: every, Synopsis: "times", Doc: "Prints the accumulated user and system times of the shell and its children."},
	{Name: "true", shells: every, Synopsis: "true", Doc: "Returns a successful (zero) exit status."},
	{Name: "false", shells: every, Synopsis: "false", Doc: "Returns an unsuccessful (non-zero) exit status."},
	{Name: "test", shells: every, Synopsis: "test expression", Doc: "Evaluates a conditional expression: file tests like `-f file` and `-d dir`, string tests like `-z str` and `a = b`, integer comparisons like `a -lt b`. Returns 0 when true."},
	{Name: "[", shells: every, Synopsis: "[ expression ]", Doc: "Same as `test`, with a mandatory closing `]` argument."},
	{Name: "read", shells: every, Synopsis: "read [-r] [name ...]", Doc: "Reads a line from standard input, splits it on `IFS` and assigns the fields to the names. Always use `-r` to keep backslashes.", Notes: map[dialect.Dialect]string{
		dialect.Bash: "`-a array`, `-p prompt`, `-s` (silent), `-t timeout`, `-n count`, `-d delim`; the line goes to `REPLY` without names.",
		dialect.Zsh:  "`-A array`, `-k count`, `-t timeout`, `-s` (silent); `read 'var?prompt'` prompts.",
		dialect.Ksh:  "`-A array`, `-t timeout`; `read 'var?prompt'` prompts.",
	}},
	{Name: "getopts", shells: every, Synopsis: "getopts optstring name [args ...]", Doc: "Parses positional options one at a time: stores the option letter in `name`, its argument in `OPTARG`, and the next index in `OPTIND`. A colon after a letter marks an option that takes an argument."},
	{Name: "command", shells: every, Synopsis: "command [-pvV] name [arguments ...]", Doc: "Runs `name` bypassing functions and aliases. `-v` prints how `name` would be resolved, which is the portable way to test whether a command exists."},
	{Name: "type", shells: every, Synopsis: "type name ...", Doc: "Describes how each name would be interpreted: alias, keyword, function, builtin or file.", Notes: map[dialect.Dialect]string{
		dialect.Bash: "`-t` prints a single word, `-p` the path, `-a` every match.",
		dialect.Zsh:  "Equivalent to `whence -v`.",
	}},
	{Name: "hash", shells: every, Synopsis: "hash [-r] [name ...]", Doc: "Remembers or reports the full paths of commands; `-r` forgets them all."},
	{Name: "jobs", shells: every, Synopsis: "jobs [-lp] [job ...]", Doc: "Lists the active jobs of the shell."},
	{Name: "fg", shells: every, Synopsis: "fg [job]", Doc: "Moves a job to the foreground."},
	{Name: "bg", shells: every, Synopsis: "bg [job ...]", Doc: "Resumes stopped jobs in the background."},
	{Name: "kill", shells: every, Synopsis: "kill [-s signal | -signal] pid|job ...", Doc: "Sends a signal (default `TERM`) to processes or jobs; `kill -l` lists signal names."},
	{Name: "wait", shells: every, Synopsis: "wait [pid|job ...]", Doc: "Waits for the given background processes, or for all of them, and returns the status of the last one.", Notes: map[dialect.Dialect]string{
		dialect.Bash: "`wait -n` waits for the next job to finish; `-p var` stores its pid.",
	}},
	{Name: "ulimit", shells: every, Synopsis: "ulimit [-HSa] [-option] [limit]", Doc: "Reports or sets resource limits of the shell and its children."},
	{Name: "umask", shells: every, Synopsis: "umask [-S] [mask]", Doc: "Reports or sets the file mode creation mask."},
	{Name: "fc", shells: dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Ksh, dialect.Posix).Shells(), Synopsis: "fc [-e editor] [-lnr] [first [last]]", Doc: "Lists, edits and re-executes commands from the history."},
	{Name: "local", shells: localShell, Synopsis: "local [option] name[=value] ...", Doc: "Declares variables scoped to the enclosing function. Assignments in called functions see the local value (dynamic scoping).", Notes: map[dialect.Dialect]string{
		dialect.Dash: "Supported by dash although POSIX does not specify it; only names and assignments are accepted, no options.",
		dialect.Ksh:  "Only mksh provides `local`; ksh93 uses `typeset` inside `function name { }` definitions.",
		dialect.Bash: "Accepts the same options as `declare`.",
		dialect.Zsh:  "Accepts the same options as `typeset`.",
	}},
	{Name: "declare", shells: bashZsh, Synopsis: "declare [-aAfFgilnrtux] [-p] [name[=value] ...]", Doc: "Declares variables and sets attributes: `-a` indexed array, `-A` associative array, `-i` integer, `-r` read-only, `-x` export, `-n` name reference, `-l`/`-u` case conversion. Inside a function it creates locals unless `-g` is given.", Notes: map[dialect.Dialect]string{
		dialect.Zsh: "Synonym for `typeset`.",
	}},
	{Name: "typeset", shells: bashZshKsh, Synopsis: "typeset [-aAilrux] [name[=value] ...]", Doc: "Declares variables and sets attributes. Inside a function it creates local variables.", Notes: map[dialect.Dialect]string{
		dialect.Bash: "Obsolete synonym for `declare`.",
		dialect.Zsh:  "`-T SCALAR array` ties a colon-separated scalar to an array; `-g` keeps an existing global.",
	}},
	{Name: "integer", shells: zshKsh, Synopsis: "integer name[=value] ...", Doc: "Declares integer variables, like `typeset -i`."},
	{Name: "float", shells: zshOnly, Synopsis: "float name[=value] ...", Doc: "Declares floating point variables, like `typeset -E`."},
	{Name: "nameref", shells: dialect.SetOf(dialect.Ksh).Shells(), Synopsis: "nameref name=target", Doc: "Declares `name` as a reference to the variable `target`, like `typeset -n`."},
	{Name: "let", shells: bashZshKsh, Synopsis: "let expression ...", Doc: "Evaluates arithmetic expressions; returns 1 when the last one evaluates to 0. `(( expression ))` is the more readable form."},
	{Name: "shopt", shells: bashOnly, Synopsis: "shopt [-pqsu] [-o] [optname ...]", Doc: "Sets (`-s`) or unsets (`-u`) bash-specific shell options such as `nullglob`, `globstar`, `extglob` and `lastpipe`."},
	{Name: "setopt", shells: zshOnly, Synopsis: "setopt [option ...]", Doc: "Enables zsh options such as `extended_glob`, `null_glob` or `pipe_fail`; without arguments lists the options that differ from the default."},
	{Name: "unsetopt", shells: zshOnly, Synopsis: "unsetopt [option ...]", Doc: "Disables zsh options."},
	{Name: "emulate", shells: zshOnly, Synopsis: "emulate [-LR] {zsh|sh|ksh|csh} [-c cmd]", Doc: "Switches option settings to emulate another shell; `emulate -L zsh` at the start of a function restores zsh defaults locally."},
	{Name: "autoload", shells: zshKsh, Synopsis: "autoload [-Uz] name ...", Doc: "Marks functions to be loaded from `fpath`/`FPATH` the first time they are called. `-U` suppresses alias expansion, `-z` forces zsh-style loading."},
	{Name: "whence", shells: zshKsh, Synopsis: "whence [-vcwpa] name ...", Doc: "Reports how each name would be interpreted; `-v` verbosely, `-p` searching only `PATH`."},
	{Name: "where", shells: zshOnly, Synopsis: "where name ...", Doc: "Lists every interpretation of each name, like `whence -ca`."},
	{Name: "which", shells: zshOnly, Synopsis: "which name ...", Doc: "Builtin equivalent of `whence -c`."},
	{Name: "functions", shells: zshKsh, Synopsis: "functions [name ...]", Doc: "Prints function definitions, like `typeset -f`."},
	{Name: "bindkey", shells: zshOnly, Synopsis: "bindkey [-M keymap] [key [widget]]", Doc: "Binds key sequences to zle widgets in a keymap."},
	{Name: "zle", shells: zshOnly, Synopsis: "zle [-N widget [function]] [widget]", Doc: "Manipulates the zsh line editor: `-N` defines a widget from a function."},
	{Name: "zstyle", shells: zshOnly, Synopsis: "zstyle [-d] pattern style [values ...]", Doc: "Defines or queries context-sensitive styles, used mostly by the completion system."},
	{Name: "zmodload", shells: zshOnly, Synopsis: "zmodload [-aiu] module ...", Doc: "Loads zsh modules such as `zsh/datetime` or `zsh/complist`."},
	{Name: "vared", shells: zshOnly, Synopsis: "vared [-p prompt] name", Doc: "Edits the value of a variable interactively with zle."},
	{Name: "noglob", shells: zshOnly, Synopsis: "noglob command [arguments ...]", Doc: "Precommand modifier: runs `command` without filename generation on its arguments."},
	{Name: "builtin", shells: bashZshKsh, Synopsis: "builtin name [arguments ...]", Doc: "Runs the shell builtin `name`, bypassing functions with the same name."},
	{Name: "enable", shells: bashOnly, Synopsis: "enable [-n] name ...", Doc: "Enables builtins; `-n` disables them so that a command of the same name in `PATH` runs instead."},
	{Name: "disown", shells: bashZshKsh, Synopsis: "disown [job ...]", Doc: "Removes jobs from the job table so they are not sent `SIGHUP` when the shell exits."},
	{Name: "caller", shells: bashOnly, Synopsis: "caller [frame]", Doc: "Prints the line number and source file of the current subroutine call, or of `frame` levels up."},
	{Name: "mapfile", shells: bashOnly, Synopsis: "mapfile [-t] [-d delim] [-n count] [array]", Doc: "Reads lines from standard input into an indexed array (default `MAPFILE`); `-t` strips the trailing delimiter."},
	{Name: "readarray", shells: bashOnly, Synopsis: "readarray [-t] [array]", Doc: "Synonym for `mapfile`."},
	{Name: "pushd", shells: bashZsh, Synopsis: "pushd [dir | +N | -N]", Doc: "Pushes the current directory onto the directory stack and changes to `dir`."},
	{Name: "popd", shells: bashZsh, Synopsis: "popd [+N | -N]", Doc: "Removes the top entry from the directory stack and changes to the new top."},
	{Name: "dirs", shells: bashZsh, Synopsis: "dirs [-clpv]", Doc: "Prints the directory stack."},
	{Name: "compgen", shells: bashOnly, Synopsis: "compgen [option] [word]", Doc: "Generates completion matches for `word`, as the programmable completion would."},
	{Name: "complete", shells: bashOnly, Synopsis: "complete [options] name ...", Doc: "Specifies how arguments to `name` are completed."},
	{Name: "compdef", shells: zshOnly, Synopsis: "compdef function command ...", Doc: "Registers a completion function for commands (provided by `compinit`)."},
	{Name: "history", shells: bashZsh, Synopsis: "history [n]", Doc: "Lists the command history."},
	{Name: "help", shells: bashOnly, Synopsis: "help [-dms] [pattern]", Doc: "Displays help for bash builtins."},
	{Name: "logout", shells: bashZsh, Synopsis: "logout [n]", Doc: "Exits a login shell."},
	{Name: "suspend", shells: bashZsh, Synopsis: "suspend [-f]", Doc: "Suspends the shell until it receives `SIGCONT`."},
	{Name: "bind", shells: bashOnly, Synopsis: "bind [-m keymap] [-lpsvPSVX] [keyseq:function]", Doc: "Sets readline key bindings and variables."},
	{Name: "rehash", shells: zshOnly, Synopsis: "rehash", Doc: "Rebuilds the command hash table, like `hash -r`."},
}

var keywords = []Entry{
	{Name: "if", shells: every, Synopsis: "if list; then list; [elif list; then list;] ... [else list;] fi", Doc: "Runs the `then` branch of the first condition list that exits with status 0, otherwise the `else` branch."},
	{Name: "then", shells: every, Synopsis: "if list; then list; fi", Doc: "Starts the body run when the preceding `if` or `elif` condition succeeds."},
	{Name: "elif", shells: every, Synopsis: "elif list; then list;", Doc: "Tests another condition when the previous `if` or `elif` condition failed."},
	{Name: "else", shells: every, Synopsis: "else list;", Doc: "Runs when no preceding `if` or `elif` condition succeeded."},
	{Name: "fi", shells: every, Synopsis: "fi", Doc: "Ends an `if` statement."},
	{Name: "case", shells: every, Synopsis: "case word in [(]pattern [| pattern] ...) list ;; ... esac", Doc: "Runs the list of the first pattern that matches `word`. Patterns use glob syntax."},
	{Name: "esac", shells: every, Synopsis: "esac", Doc: "Ends a `case` statement."},
	{Name: "for", shells: every, Synopsis: "for name [in words ...]; do list; done", Doc: "Runs the body once per word, assigning it to `name`. Without `in`, iterates over the positional parameters.", Notes: map[dialect.Dialect]string{
		dialect.Bash: "Also supports the arithmetic form `for (( init; cond; step )); do list; done`.",
		dialect.Zsh:  "Also supports `for (( ... ))`, multiple names per iteration and the short form `for name (words) command`.",
		dialect.Ksh:  "Also supports the arithmetic form `for (( init; cond; step ))`.",
	}},
	{Name: "select", shells: bashZshKsh, Synopsis: "select name [in words ...]; do list; done", Doc: "Prints a numbered menu of the words, reads a choice into `name` (and the raw input into `REPLY`) and runs the body, until `break`."},
	{Name: "while", shells: every, Synopsis: "while list; do list; done", Doc: "Runs the body as long as the condition list exits with status 0."},
	{Name: "until", shells: every, Synopsis: "until list; do list; done", Doc: "Runs the body as long as the condition list exits with a non-zero status."},
	{Name: "do", shells: every, Synopsis: "do list; done", Doc: "Starts the body of a `for`, `select`, `while` or `until` loop."},
	{Name: "done", shells: every, Synopsis: "done", Doc: "Ends the body of a loop."},
	{Name: "in", shells: every, Synopsis: "for name in words; case word in", Doc: "Introduces the word list of a `for`/`select` loop or the patterns of a `case` statement."},
	{Name: "function", shells: bashZshKsh, Synopsis: "function name { list; }", Doc: "Defines a function using the non-POSIX keyword form. `name() { list; }` is the portable form.", Notes: map[dialect.Dialect]string{
		dialect.Ksh: "In ksh, only functions declared with `function` get local `typeset` variables and their own traps.",
	}},
	{Name: "time", shells: bashZshKsh, Synopsis: "time [-p] pipeline", Doc: "Runs the pipeline and reports the elapsed, user and system time it used."},
	{Name: "coproc", shells: bashZsh, Synopsis: "coproc [NAME] command", Doc: "Runs `command` asynchronously with a two-way pipe.", Notes: map[dialect.Dialect]string{
		dialect.Bash: "`${NAME[0]}` is a descriptor reading its output and `${NAME[1]}` one writing to its input; `NAME` defaults to `COPROC`.",
		dialect.Zsh:  "zsh takes no name; talk to the coprocess with `>&p` and `<&p`.",
	}},
	{Name: "!", shells: every, Synopsis: "! pipeline", Doc: "Negates the exit status of the pipeline."},
	{Name: "[[", shells: bashZshKsh, Synopsis: "[[ expression ]]", Doc: "Conditional expression without word splitting or globbing of its operands. Supports `==`/`!=` pattern matching, `=~` regular expressions, `<`/`>` string ordering and `&&`/`||`."},
	{Name: "]]", shells: bashZshKsh, Synopsis: "]]", Doc: "Ends a `[[` conditional expression."},
	{Name: "repeat", shells: zshOnly, Synopsis: "repeat count command", Doc: "Runs `command` `count` times."},
	{Name: "foreach", shells: zshOnly, Synopsis: "foreach name (words) list end", Doc: "csh-style loop: runs `list` once per word."},
	{Name: "end", shells: zshOnly, Synopsis: "end", Doc: "Ends a `foreach` loop."},
}
