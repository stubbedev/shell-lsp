package shelldocs

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

type shellOption struct {
	name       string
	shells     dialect.Shells
	doc        string
	letters    []optionLetter
	dashSince  string
	debianDash bool
}

type optionLetter struct {
	letter byte
	shells dialect.Shells
}

const (
	globstarDoc    = "Make `**` match files and directories recursively."
	nullglobDoc    = "Make a glob that matches nothing expand to nothing."
	shwordsplitDoc = "Split unquoted parameter expansions on `IFS`, like sh."
	onecmdDoc      = "Exit after reading and executing one command."
	restrictedDoc  = "Enter restricted mode: forbid `cd`, changing `PATH` and other escapes; it cannot be turned off again."
)

var (
	notZsh      = dialect.SetOf(dialect.Posix, dialect.Bash, dialect.Dash, dialect.Ksh, dialect.Busybox).Shells()
	dashBusybox = dialect.SetOf(dialect.Dash, dialect.Busybox).Shells()
	dashOnly    = dialect.SetOf(dialect.Dash).Shells()
	ksh93       = dialect.ShellsOf(dialect.Ksh.Shell())
	mksh        = dialect.ShellsOf(dialect.FlavorMksh.Shell())
)

var shellOptions = []shellOption{
	{name: "allexport", shells: every, doc: "Export every variable that is assigned.", letters: []optionLetter{{letter: 'a', shells: every}}},
	{name: "errexit", shells: every, doc: "Exit when a command fails outside a condition.", letters: []optionLetter{{letter: 'e', shells: every}}},
	{name: "noclobber", shells: every, doc: "Refuse to overwrite existing files with `>`.", letters: []optionLetter{{letter: 'C', shells: every}}},
	{name: "noglob", shells: every, doc: "Disable filename generation.", letters: []optionLetter{{letter: 'f', shells: notZsh}, {letter: 'F', shells: zshOnly}}},
	{name: "nounset", shells: every, doc: "Make the expansion of an unset variable an error.", letters: []optionLetter{{letter: 'u', shells: every}}},
	{name: "verbose", shells: every, doc: "Print input lines as they are read.", letters: []optionLetter{{letter: 'v', shells: every}}},
	{name: "xtrace", shells: every, doc: "Print each command, prefixed with `PS4`, before running it.", letters: []optionLetter{{letter: 'x', shells: every}}},
	{name: "noexec", shells: every, doc: "Read commands without running them, to check the syntax.", letters: []optionLetter{{letter: 'n', shells: every}}},
	{name: "monitor", shells: every, doc: "Enable job control.", letters: []optionLetter{{letter: 'm', shells: every}}},
	{name: "notify", shells: every, doc: "Report job status changes immediately.", letters: []optionLetter{{letter: 'b', shells: notZsh}, {letter: '5', shells: zshOnly}}},
	{name: "ignoreeof", shells: every, doc: "Do not exit an interactive shell on end-of-file.", letters: []optionLetter{{letter: 'I', shells: dashBusybox}, {letter: '7', shells: zshOnly}}},
	{name: "vi", shells: every, doc: "Use vi-style line editing.", letters: []optionLetter{{letter: 'V', shells: dashOnly}}},
	{name: "emacs", shells: dialect.SetOf(dialect.Bash, dialect.Zsh, dialect.Dash, dialect.Ksh).Shells(), doc: "Use emacs-style line editing.", letters: []optionLetter{{letter: 'E', shells: dashOnly}}},
	{name: "nolog", shells: dialect.SetOf(dialect.Posix, dialect.Bash, dialect.Zsh, dialect.Dash, dialect.Ksh).Shells(), doc: "Do not enter function definitions into the command history; most shells accept it and ignore it."},
	{name: "pipefail", shells: notPosix, doc: "Make a pipeline fail when any of its commands fails, not only the last.", dashSince: "0.5.13"},
	{name: "posix", shells: bashKsh, doc: "Make the shell behave as a POSIX shell where the default differs."},
	{name: "errtrace", shells: bashBusybox, doc: "Let functions, command substitutions and subshells inherit the `ERR` trap.", letters: []optionLetter{{letter: 'E', shells: bashBusybox}}},
	{name: "functrace", shells: bashOnly | ksh93, doc: "Let functions inherit the `DEBUG` and `RETURN` traps.", letters: []optionLetter{{letter: 'T', shells: bashOnly}}},
	{name: "history", shells: bashOnly, doc: "Enable command history."},
	{name: "hashall", shells: bashZsh, doc: "Remember the location of commands as they are looked up.", letters: []optionLetter{{letter: 'h', shells: bashOnly}}},
	{name: "trackall", shells: zshKsh, doc: "Remember the location of commands as they are looked up.", letters: []optionLetter{{letter: 'h', shells: kshOnly}}},
	{name: "physical", shells: bashZsh | mksh, doc: "Resolve symbolic links in `cd` and `pwd` instead of keeping the logical path.", letters: []optionLetter{{letter: 'P', shells: bashOnly}, {letter: 'w', shells: zshOnly}}},
	{name: "privileged", shells: bashZshKsh, doc: "Do not read startup files or inherit functions from the environment.", letters: []optionLetter{{letter: 'p', shells: bashZshKsh}}, debianDash: true},
	{name: "braceexpand", shells: bashZshKsh, doc: "Enable brace expansion; on by default.", letters: []optionLetter{{letter: 'B', shells: bashOnly | ksh93}}},
	{name: "histexpand", shells: bashZsh | ksh93, doc: "Enable `!` history expansion.", letters: []optionLetter{{letter: 'H', shells: bashOnly | ksh93}}},
	{name: "keyword", shells: bashKsh, doc: "Treat assignments anywhere on the command line as environment assignments.", letters: []optionLetter{{letter: 'k', shells: bashKsh}}},
	{name: "globstar", shells: ksh93, doc: globstarDoc, letters: []optionLetter{{letter: 'G', shells: ksh93}}},
	{name: "nullglob", shells: zshOnly, doc: nullglobDoc, letters: []optionLetter{{letter: 'G', shells: zshOnly}}},
	{name: "shwordsplit", shells: zshOnly, doc: shwordsplitDoc, letters: []optionLetter{{letter: 'y', shells: zshOnly}}},
	{name: "onecmd", shells: bashZsh, doc: onecmdDoc, letters: []optionLetter{{letter: 't', shells: bashZsh}}},
	{name: "restricted", shells: zshKsh, doc: restrictedDoc, letters: []optionLetter{{letter: 'r', shells: zshOnly | ksh93}}},
	{name: "markdirs", shells: zshKsh, doc: "Append `/` to the directory names that filename generation produces.", letters: []optionLetter{{letter: '8', shells: zshOnly}, {letter: 'X', shells: mksh}}},
	{name: "utf8-mode", shells: mksh, doc: "Treat input and strings as UTF-8.", letters: []optionLetter{{letter: 'U', shells: mksh}}},
}

var unnamedLetters = []Entry{
	{Name: "h", shells: dialect.SetOf(dialect.Posix).Shells(), Doc: "Locate and remember the utilities a function invokes when the function is defined."},
	{Name: "r", shells: bashOnly, Doc: restrictedDoc},
	{Name: "t", shells: ksh93, Doc: onecmdDoc},
}

func SetOptionDashSince(name string) string {
	for _, o := range shellOptions {
		if o.name == name {
			return o.dashSince
		}
	}
	return ""
}

func SetLetter(letter byte, s dialect.Shell) (string, bool) {
	for _, o := range shellOptions {
		for _, l := range o.letters {
			if l.letter == letter && l.shells.Has(s) {
				return o.name, true
			}
		}
	}
	return "", false
}

var setOptions = func() []Entry {
	var out []Entry
	for _, o := range shellOptions {
		unlettered := o.shells
		for _, l := range o.letters {
			if shared := o.shells & l.shells; shared != 0 {
				out = append(out, Entry{Name: o.name, shells: shared, Doc: fmt.Sprintf("%s (`-%c`)", o.doc, l.letter)})
			}
			unlettered &^= l.shells
		}
		if unlettered != 0 {
			out = append(out, Entry{Name: o.name, shells: unlettered, Doc: o.doc})
		}
	}
	return out
}()

func letterEntries(name func(letter string) string) []Entry {
	var out []Entry
	for _, o := range shellOptions {
		for _, l := range o.letters {
			out = append(out, Entry{Name: name(string(l.letter)), shells: l.shells, Doc: fmt.Sprintf("`%s`: %s", o.name, lowerFirst(o.doc))})
		}
	}
	for _, e := range unnamedLetters {
		out = append(out, Entry{Name: name(e.Name), shells: e.shells, Doc: e.Doc})
	}
	return out
}

func lowerFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[size:]
}

var shoptOptions = []Entry{
	{Name: "autocd", shells: bashOnly, Doc: "A command that names a directory changes into it."},
	{Name: "cdspell", shells: bashOnly, Doc: "Correct minor spelling errors in `cd` arguments."},
	{Name: "checkwinsize", shells: bashOnly, Doc: "Update `LINES` and `COLUMNS` after each command."},
	{Name: "dotglob", shells: bashOnly, Doc: "Include names starting with `.` in glob results."},
	{Name: "expand_aliases", shells: bashOnly, Doc: "Expand aliases; off by default in non-interactive shells."},
	{Name: "extdebug", shells: bashOnly, Doc: "Enable behaviour intended for debuggers, such as `BASH_ARGV`."},
	{Name: "extglob", shells: bashOnly, Doc: "Enable the extended patterns `?(...)`, `*(...)`, `+(...)`, `@(...)` and `!(...)`."},
	{Name: "failglob", shells: bashOnly, Doc: "Make a glob that matches nothing an error."},
	{Name: "globstar", shells: bashOnly, Doc: globstarDoc},
	{Name: "histappend", shells: bashOnly, Doc: "Append to the history file instead of overwriting it."},
	{Name: "inherit_errexit", shells: bashOnly, Doc: "Command substitutions inherit `errexit`."},
	{Name: "lastpipe", shells: bashOnly, Doc: "Run the last command of a pipeline in the current shell when job control is off."},
	{Name: "nocaseglob", shells: bashOnly, Doc: "Match globs case-insensitively."},
	{Name: "nocasematch", shells: bashOnly, Doc: "Match `case` and `[[` patterns case-insensitively."},
	{Name: "nullglob", shells: bashOnly, Doc: nullglobDoc},
	{Name: "progcomp", shells: bashOnly, Doc: "Enable programmable completion."},
	{Name: "sourcepath", shells: bashOnly, Doc: "Make `source` search `PATH` for its argument."},
	{Name: "xpg_echo", shells: bashOnly, Doc: "Make `echo` expand backslash escapes by default."},
}

var setoptOptions = []Entry{
	{Name: "auto_cd", shells: zshOnly, Doc: "A command that names a directory changes into it."},
	{Name: "auto_pushd", shells: zshOnly, Doc: "Make `cd` push the old directory onto the stack."},
	{Name: "bash_rematch", shells: zshOnly, Doc: "Fill `BASH_REMATCH` instead of `MATCH`/`match` for `=~`."},
	{Name: "correct", shells: zshOnly, Doc: "Offer spelling correction for commands."},
	{Name: "err_exit", shells: zshOnly, Doc: "Exit when a command fails."},
	{Name: "err_return", shells: zshOnly, Doc: "Return from a function when a command fails."},
	{Name: "extended_glob", shells: zshOnly, Doc: "Enable `#`, `~` and `^` as glob operators."},
	{Name: "glob_dots", shells: zshOnly, Doc: "Include names starting with `.` in glob results."},
	{Name: "hist_ignore_all_dups", shells: zshOnly, Doc: "Remove older duplicates of a command from the history."},
	{Name: "hist_ignore_space", shells: zshOnly, Doc: "Do not record commands starting with a space."},
	{Name: "inc_append_history", shells: zshOnly, Doc: "Append commands to the history file as they are entered."},
	{Name: "interactive_comments", shells: zshOnly, Doc: "Allow comments in interactive shells."},
	{Name: "ksh_arrays", shells: zshOnly, Doc: "Index arrays from 0 and require braces, like ksh and bash."},
	{Name: "local_options", shells: zshOnly, Doc: "Restore option settings when the function returns."},
	{Name: "local_traps", shells: zshOnly, Doc: "Restore traps when the function returns."},
	{Name: "no_unset", shells: zshOnly, Doc: "Treat expansion of unset variables as an error."},
	{Name: "null_glob", shells: zshOnly, Doc: nullglobDoc},
	{Name: "pipe_fail", shells: zshOnly, Doc: "Make a pipeline fail when any of its commands fails."},
	{Name: "prompt_subst", shells: zshOnly, Doc: "Perform parameter, command and arithmetic expansion in prompts."},
	{Name: "rc_quotes", shells: zshOnly, Doc: "Allow `''` inside single quotes to mean one quote."},
	{Name: "sh_word_split", shells: zshOnly, Doc: shwordsplitDoc},
	{Name: "share_history", shells: zshOnly, Doc: "Share the history between running shells."},
	{Name: "warn_create_global", shells: zshOnly, Doc: "Warn when a function creates a global variable."},
	{Name: "xtrace", shells: zshOnly, Doc: "Print each command before executing it."},
}
