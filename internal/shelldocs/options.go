package shelldocs

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

type shellOption struct {
	name     string
	dialects dialect.Set
	doc      string
	letters  []optionLetter
}

type optionLetter struct {
	letter   byte
	dialects dialect.Set
}

var (
	notZsh      = dialect.SetOf(dialect.Posix, dialect.Bash, dialect.Dash, dialect.Ksh, dialect.Busybox)
	dashBusybox = dialect.SetOf(dialect.Dash, dialect.Busybox)
	dashOnly    = dialect.SetOf(dialect.Dash)
)

var shellOptions = []shellOption{
	{name: "allexport", dialects: every, doc: "Export every variable that is assigned.", letters: []optionLetter{{'a', every}}},
	{name: "errexit", dialects: every, doc: "Exit when a command fails outside a condition.", letters: []optionLetter{{'e', every}}},
	{name: "noclobber", dialects: every, doc: "Refuse to overwrite existing files with `>`.", letters: []optionLetter{{'C', every}}},
	{name: "noglob", dialects: every, doc: "Disable filename generation.", letters: []optionLetter{{'f', notZsh}, {'F', zshOnly}}},
	{name: "nounset", dialects: every, doc: "Make the expansion of an unset variable an error.", letters: []optionLetter{{'u', every}}},
	{name: "verbose", dialects: every, doc: "Print input lines as they are read.", letters: []optionLetter{{'v', every}}},
	{name: "xtrace", dialects: every, doc: "Print each command, prefixed with `PS4`, before running it.", letters: []optionLetter{{'x', every}}},
	{name: "noexec", dialects: every, doc: "Read commands without running them, to check the syntax.", letters: []optionLetter{{'n', every}}},
	{name: "monitor", dialects: every, doc: "Enable job control.", letters: []optionLetter{{'m', every}}},
	{name: "notify", dialects: every, doc: "Report job status changes immediately.", letters: []optionLetter{{'b', notZsh}}},
	{name: "ignoreeof", dialects: every, doc: "Do not exit an interactive shell on end-of-file.", letters: []optionLetter{{'I', dashBusybox}}},
	{name: "vi", dialects: every, doc: "Use vi-style line editing.", letters: []optionLetter{{'V', dashOnly}}},
	{name: "emacs", dialects: dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Dash, dialect.Ksh, dialect.Busybox), doc: "Use emacs-style line editing.", letters: []optionLetter{{'E', dashOnly}}},
	{name: "nolog", dialects: dialect.SetOf(dialect.Posix, dialect.Bash, dialect.Zsh, dialect.Dash, dialect.Ksh), doc: "Do not enter function definitions into the command history; most shells accept it and ignore it."},
	{name: "pipefail", dialects: notPosix, doc: "Make a pipeline fail when any of its commands fails, not only the last."},
	{name: "posix", dialects: bashOnly, doc: "Make bash behave as a POSIX shell where the default differs."},
	{name: "errtrace", dialects: bashBusybox, doc: "Let functions, command substitutions and subshells inherit the `ERR` trap.", letters: []optionLetter{{'E', bashBusybox}}},
	{name: "functrace", dialects: bashOnly, doc: "Let functions inherit the `DEBUG` and `RETURN` traps.", letters: []optionLetter{{'T', bashOnly}}},
	{name: "history", dialects: bashOnly, doc: "Enable command history."},
	{name: "hashall", dialects: bashOnly, doc: "Remember the location of commands as they are looked up.", letters: []optionLetter{{'h', bashOnly}}},
	{name: "trackall", dialects: kshOnly, doc: "Remember the location of commands as they are looked up.", letters: []optionLetter{{'h', kshOnly}}},
	{name: "physical", dialects: bashOnly, doc: "Resolve symbolic links in `cd` and `pwd` instead of keeping the logical path.", letters: []optionLetter{{'P', bashOnly}}},
	{name: "privileged", dialects: bashOnly, doc: "Do not read startup files or inherit functions from the environment.", letters: []optionLetter{{'p', bashOnly}}},
	{name: "braceexpand", dialects: bashKsh, doc: "Enable brace expansion; on by default.", letters: []optionLetter{{'B', bashKsh}}},
	{name: "histexpand", dialects: bashKsh, doc: "Enable `!` history expansion.", letters: []optionLetter{{'H', bashKsh}}},
	{name: "keyword", dialects: bashKsh, doc: "Treat assignments anywhere on the command line as environment assignments.", letters: []optionLetter{{'k', bashKsh}}},
}

var unnamedLetters = []Entry{
	{Name: "h", Dialects: dialect.SetOf(dialect.Posix), Doc: "Locate and remember the utilities a function invokes when the function is defined."},
}

func SetLetter(letter byte, d dialect.Dialect) (string, bool) {
	for _, o := range shellOptions {
		for _, l := range o.letters {
			if l.letter == letter && l.dialects.Has(d) {
				return o.name, true
			}
		}
	}
	return "", false
}

var setOptions = func() []Entry {
	var out []Entry
	for _, o := range shellOptions {
		unlettered := o.dialects
		for _, l := range o.letters {
			if shared := o.dialects & l.dialects; shared != 0 {
				out = append(out, Entry{Name: o.name, Dialects: shared, Doc: fmt.Sprintf("%s (`-%c`)", o.doc, l.letter)})
			}
			unlettered &^= l.dialects
		}
		if unlettered != 0 {
			out = append(out, Entry{Name: o.name, Dialects: unlettered, Doc: o.doc})
		}
	}
	return out
}()

func letterEntries(name func(letter string) string) []Entry {
	var out []Entry
	for _, o := range shellOptions {
		for _, l := range o.letters {
			out = append(out, Entry{Name: name(string(l.letter)), Dialects: l.dialects, Doc: fmt.Sprintf("`%s`: %s", o.name, lowerFirst(o.doc))})
		}
	}
	for _, e := range unnamedLetters {
		out = append(out, Entry{Name: name(e.Name), Dialects: e.Dialects, Doc: e.Doc})
	}
	return out
}

func lowerFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[size:]
}

var shoptOptions = []Entry{
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

var setoptOptions = []Entry{
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
