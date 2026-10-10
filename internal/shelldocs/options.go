package shelldocs

import "github.com/stubbedev/shell-lsp/internal/dialect"

type OptionSet uint8

const (
	SetOptions OptionSet = iota
	Shopt
	Setopt
)

type ShellOption struct {
	Name     string
	Dialects dialect.Set
	Doc      string
}

var setOptions = []ShellOption{
	{Name: "allexport", Dialects: every, Doc: "Export every variable that is assigned (`-a`)."},
	{Name: "errexit", Dialects: every, Doc: "Exit when a command fails (`-e`)."},
	{Name: "noclobber", Dialects: every, Doc: "Do not overwrite existing files with `>` (`-C`)."},
	{Name: "noglob", Dialects: every, Doc: "Disable filename generation (`-f`)."},
	{Name: "nounset", Dialects: every, Doc: "Treat expansion of unset variables as an error (`-u`)."},
	{Name: "verbose", Dialects: every, Doc: "Print input lines as they are read (`-v`)."},
	{Name: "xtrace", Dialects: every, Doc: "Print each command before executing it (`-x`)."},
	{Name: "noexec", Dialects: every, Doc: "Read commands but do not execute them (`-n`)."},
	{Name: "monitor", Dialects: every, Doc: "Enable job control (`-m`)."},
	{Name: "notify", Dialects: every, Doc: "Report job status changes immediately (`-b`)."},
	{Name: "ignoreeof", Dialects: every, Doc: "Do not exit an interactive shell on end-of-file."},
	{Name: "vi", Dialects: every, Doc: "Use vi-style line editing."},
	{Name: "emacs", Dialects: dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Dash, dialect.Ksh), Doc: "Use emacs-style line editing."},
	{Name: "pipefail", Dialects: every, Doc: "Make a pipeline fail when any of its commands fails, not only the last."},
	{Name: "posix", Dialects: bashOnly, Doc: "Make bash behave as a POSIX shell where the default differs."},
	{Name: "errtrace", Dialects: bashOnly, Doc: "Inherit the `ERR` trap in functions and subshells (`-E`)."},
	{Name: "functrace", Dialects: bashOnly, Doc: "Inherit the `DEBUG` and `RETURN` traps in functions and subshells (`-T`)."},
	{Name: "history", Dialects: bashOnly, Doc: "Enable command history."},
	{Name: "hashall", Dialects: bashOnly, Doc: "Remember the location of commands (`-h`)."},
	{Name: "physical", Dialects: bashOnly, Doc: "Do not follow symbolic links for `cd` and `pwd` (`-P`)."},
	{Name: "privileged", Dialects: bashOnly, Doc: "Do not read startup files or inherit functions from the environment (`-p`)."},
}

var shoptOptions = []ShellOption{
	{Name: "autocd", Dialects: bashOnly, Doc: "A command that names a directory changes into it."},
	{Name: "cdspell", Dialects: bashOnly, Doc: "Correct minor spelling errors in `cd` arguments."},
	{Name: "checkwinsize", Dialects: bashOnly, Doc: "Update `LINES` and `COLUMNS` after each command."},
	{Name: "dotglob", Dialects: bashOnly, Doc: "Include names starting with `.` in glob results."},
	{Name: "expand_aliases", Dialects: bashOnly, Doc: "Expand aliases; off by default in non-interactive shells."},
	{Name: "extdebug", Dialects: bashOnly, Doc: "Enable behaviour intended for debuggers, such as `BASH_ARGV`."},
	{Name: "extglob", Dialects: bashOnly, Doc: "Enable the extended patterns `?(...)`, `*(...)`, `+(...)`, `@(...)` and `!(...)`."},
	{Name: "failglob", Dialects: bashOnly, Doc: "Make a glob that matches nothing an error."},
	{Name: "globstar", Dialects: bashOnly, Doc: "Make `**` match files and directories recursively."},
	{Name: "histappend", Dialects: bashOnly, Doc: "Append to the history file instead of overwriting it."},
	{Name: "inherit_errexit", Dialects: bashOnly, Doc: "Command substitutions inherit `errexit`."},
	{Name: "lastpipe", Dialects: bashOnly, Doc: "Run the last command of a pipeline in the current shell when job control is off."},
	{Name: "nocaseglob", Dialects: bashOnly, Doc: "Match globs case-insensitively."},
	{Name: "nocasematch", Dialects: bashOnly, Doc: "Match `case` and `[[` patterns case-insensitively."},
	{Name: "nullglob", Dialects: bashOnly, Doc: "Make a glob that matches nothing expand to nothing."},
	{Name: "progcomp", Dialects: bashOnly, Doc: "Enable programmable completion."},
	{Name: "sourcepath", Dialects: bashOnly, Doc: "Make `source` search `PATH` for its argument."},
	{Name: "xpg_echo", Dialects: bashOnly, Doc: "Make `echo` expand backslash escapes by default."},
}

var setoptOptions = []ShellOption{
	{Name: "auto_cd", Dialects: zshOnly, Doc: "A command that names a directory changes into it."},
	{Name: "auto_pushd", Dialects: zshOnly, Doc: "Make `cd` push the old directory onto the stack."},
	{Name: "bash_rematch", Dialects: zshOnly, Doc: "Fill `BASH_REMATCH` instead of `MATCH`/`match` for `=~`."},
	{Name: "correct", Dialects: zshOnly, Doc: "Offer spelling correction for commands."},
	{Name: "err_exit", Dialects: zshOnly, Doc: "Exit when a command fails."},
	{Name: "err_return", Dialects: zshOnly, Doc: "Return from a function when a command fails."},
	{Name: "extended_glob", Dialects: zshOnly, Doc: "Enable `#`, `~` and `^` as glob operators."},
	{Name: "glob_dots", Dialects: zshOnly, Doc: "Include names starting with `.` in glob results."},
	{Name: "hist_ignore_all_dups", Dialects: zshOnly, Doc: "Remove older duplicates of a command from the history."},
	{Name: "hist_ignore_space", Dialects: zshOnly, Doc: "Do not record commands starting with a space."},
	{Name: "inc_append_history", Dialects: zshOnly, Doc: "Append commands to the history file as they are entered."},
	{Name: "interactive_comments", Dialects: zshOnly, Doc: "Allow comments in interactive shells."},
	{Name: "ksh_arrays", Dialects: zshOnly, Doc: "Index arrays from 0 and require braces, like ksh and bash."},
	{Name: "local_options", Dialects: zshOnly, Doc: "Restore option settings when the function returns."},
	{Name: "local_traps", Dialects: zshOnly, Doc: "Restore traps when the function returns."},
	{Name: "no_unset", Dialects: zshOnly, Doc: "Treat expansion of unset variables as an error."},
	{Name: "null_glob", Dialects: zshOnly, Doc: "Make a glob that matches nothing expand to nothing."},
	{Name: "pipe_fail", Dialects: zshOnly, Doc: "Make a pipeline fail when any of its commands fails."},
	{Name: "prompt_subst", Dialects: zshOnly, Doc: "Perform parameter, command and arithmetic expansion in prompts."},
	{Name: "rc_quotes", Dialects: zshOnly, Doc: "Allow `''` inside single quotes to mean one quote."},
	{Name: "sh_word_split", Dialects: zshOnly, Doc: "Split unquoted parameter expansions on `IFS`, like sh."},
	{Name: "share_history", Dialects: zshOnly, Doc: "Share the history between running shells."},
	{Name: "warn_create_global", Dialects: zshOnly, Doc: "Warn when a function creates a global variable."},
	{Name: "xtrace", Dialects: zshOnly, Doc: "Print each command before executing it."},
}

func Options(set OptionSet, d dialect.Dialect) []ShellOption {
	var source []ShellOption
	switch set {
	case SetOptions:
		source = setOptions
	case Shopt:
		source = shoptOptions
	case Setopt:
		source = setoptOptions
	}
	out := make([]ShellOption, 0, len(source))
	for _, o := range source {
		if o.Dialects.Has(d) {
			out = append(out, o)
		}
	}
	return out
}
