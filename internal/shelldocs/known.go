package shelldocs

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

type ParameterTraits uint8

const (
	PromptParameter ParameterTraits = 1 << iota
	AssociativeParameter
)

type knownGroup struct {
	traits ParameterTraits
	names  []string
}

type numberedName struct{ prefix, suffix string }

type ParameterTable struct {
	names    map[string]ParameterTraits
	numbered []numberedName
}

func newKnownParameters(numbered []numberedName, groups ...knownGroup) ParameterTable {
	t := ParameterTable{names: map[string]ParameterTraits{}, numbered: numbered}
	for _, g := range groups {
		for _, name := range g.names {
			t.names[name] |= g.traits
		}
	}
	return t
}

func (t ParameterTable) Has(name string) bool {
	if _, ok := t.names[name]; ok {
		return true
	}
	for _, n := range t.numbered {
		rest, ok := strings.CutPrefix(name, n.prefix)
		if !ok {
			continue
		}
		if digits, ok := strings.CutSuffix(rest, n.suffix); ok && parser.IsDigits(digits) {
			return true
		}
	}
	return false
}

func (t ParameterTable) Is(name string, traits ParameterTraits) bool {
	return t.names[name]&traits != 0
}

var knownShellGroups = []knownGroup{
	{names: []string{
		"_", "BASH", "BASHOPTS", "BASHPID", "BASH_ALIASES", "BASH_ARGC", "BASH_ARGV", "BASH_ARGV0", "BASH_CMDS",
		"BASH_COMMAND", "BASH_COMPAT", "BASH_ENV", "BASH_EXECUTION_STRING", "BASH_LINENO", "BASH_LOADABLES_PATH",
		"BASH_MONOSECONDS", "BASH_REMATCH", "BASH_SOURCE", "BASH_SUBSHELL", "BASH_TRAPSIG", "BASH_VERSINFO",
		"BASH_VERSION", "BASH_XTRACEFD", "CDPATH", "CHILD_MAX", "COLUMNS", "COMP_CWORD", "COMP_KEY", "COMP_LINE",
		"COMP_POINT", "COMP_TYPE", "COMP_WORDBREAKS", "COMP_WORDS", "COMPREPLY", "COPROC", "DIRSTACK", "DISPLAY",
		"EMACS", "ENV", "EPOCHREALTIME", "EPOCHSECONDS", "EUID", "EXECIGNORE", "FCEDIT", "FIGNORE", "FUNCNAME",
		"FUNCNEST", "GLOBIGNORE", "GLOBSORT", "GROUPS", "HISTCMD", "HISTCONTROL", "HISTFILE", "HISTFILESIZE",
		"HISTIGNORE", "HISTSIZE", "HISTTIMEFORMAT", "HOME", "HOSTFILE", "HOSTNAME", "HOSTTYPE", "IFS", "IGNOREEOF",
		"INPUTRC", "INSIDE_EMACS", "LANG", "LANGUAGE", "LC_ALL", "LC_COLLATE", "LC_CTYPE", "LC_MESSAGES",
		"LC_MONETARY", "LC_NUMERIC", "LC_TIME", "LINENO", "LINES", "LOGNAME", "MACHTYPE", "MAIL", "MAILCHECK",
		"MAILPATH", "MAPFILE", "OLDPWD", "OPTARG", "OPTERR", "OPTIND", "OSTYPE", "PATH", "PIPESTATUS",
		"POSIXLY_CORRECT", "PPID", "PROMPT_DIRTRIM", "PS0", "PWD",
		"RANDOM", "READLINE_ARGUMENT", "READLINE_LINE", "READLINE_MARK", "READLINE_POINT", "REPLY", "SECONDS",
		"SHELL", "SHELLOPTS", "SHLVL", "SRANDOM", "TERM", "TIMEFORMAT", "TMOUT", "TMPDIR", "TZ", "UID", "USER",
		"XAUTHORITY", "auto_resume", "histchars", "rest", "REST", "EDITOR", "VISUAL", "PAGER",
		"FLAGS_ARGV", "FLAGS_ERROR", "FLAGS_FALSE", "FLAGS_HELP", "FLAGS_PARENT", "FLAGS_RESERVED", "FLAGS_TRUE",
		"FLAGS_VERSION",
	}},
	{traits: PromptParameter, names: []string{"PS1", "PS2", "PS3", "PS4", "PROMPT_COMMAND"}},
}

var knownShell = newKnownParameters(nil, knownShellGroups...)

var knownZsh = newKnownParameters([]numberedName{{prefix: "vcs_info_msg_", suffix: "_"}}, slices.Concat(knownShellGroups, []knownGroup{
	{names: []string{
		"ARGC", "argv", "status", "pipestatus", "CPUTYPE", "EGID", "ERRNO", "GID", "HOST", "signals",
		"TRY_BLOCK_ERROR", "TRY_BLOCK_INTERRUPT", "TTY", "TTYIDLE", "USERNAME", "VENDOR", "zsh_eval_context",
		"ZSH_EVAL_CONTEXT", "ZSH_ARGZERO", "ZSH_EXECUTION_STRING", "ZSH_NAME", "ZSH_PATCHLEVEL", "ZSH_SCRIPT",
		"ZSH_SUBSHELL", "ZSH_VERSION",
		"ARGV0", "BAUD", "cdpath", "CORRECT_IGNORE", "CORRECT_IGNORE_FILE", "DIRSTACKSIZE", "fignore", "fpath",
		"FPATH", "HISTCHARS", "HISTORY_IGNORE", "KEYBOARD_HACK", "KEYTIMEOUT", "LISTMAX", "mailpath", "manpath",
		"MANPATH", "match", "mbegin", "mend", "MATCH", "MBEGIN", "MEND", "module_path", "MODULE_PATH", "NULLCMD",
		"path", "PERIOD", "POSTEDIT", "psvar", "PSVAR", "READNULLCMD", "REPORTMEMORY", "REPORTTIME", "reply",
		"SAVEHIST", "STTY", "TERMINFO", "TERMINFO_DIRS", "TIMEFMT", "TMPPREFIX", "TMPSUFFIX", "WORDCHARS", "ZBEEP",
		"ZDOTDIR", "zle_bracketed_paste", "zle_highlight", "ZLE_LINE_ABORTED", "ZLE_REMOVE_SUFFIX_CHARS",
		"ZLE_SPACE_SUFFIX_CHARS", "ZLE_RPROMPT_INDENT",
		"chpwd_functions", "periodic_functions", "precmd_functions", "preexec_functions", "zshaddhistory_functions",
		"zshexit_functions", "zsh_directory_name_functions",
		"BUFFER", "BUFFERLINES", "CONTEXT", "CURSOR", "CUTBUFFER", "HISTNO", "ISEARCHMATCH_ACTIVE",
		"ISEARCHMATCH_START", "ISEARCHMATCH_END", "KEYMAP", "KEYS", "KEYS_QUEUED_COUNT", "killring",
		"LASTABORTEDSEARCH", "LASTSEARCH", "LASTWIDGET", "LBUFFER", "MARK", "NUMERIC", "PENDING", "PREBUFFER",
		"PREDISPLAY", "POSTDISPLAY", "RBUFFER", "REGION_ACTIVE", "region_highlight", "SUFFIX_ACTIVE",
		"SUFFIX_START", "SUFFIX_END", "UNDO_CHANGE_NO", "UNDO_LIMIT_NO", "WIDGET", "WIDGETFUNC", "WIDGETSTYLE",
		"YANK_ACTIVE", "YANK_START", "YANK_END", "ZLE_RECURSIVE", "ZLE_STATE",
		"CURRENT", "IPREFIX", "ISUFFIX", "PREFIX", "QIPREFIX", "QISUFFIX", "SUFFIX", "words",
		"context", "curcontext", "state", "state_descr", "line", "service", "NORMARG", "compcontext",
		"compprefuncs", "comppostfuncs", "_comp_caller_options", "_comp_priv_prefix", "_compdir", "_compskip",
		"ZLS_COLORS", "ZLS_COLOURS", "LISTPROMPT", "MENUPROMPT", "MENUSCROLL", "MENUSELECT",
		"dirstack", "reswords", "dis_reswords", "patchars", "dis_patchars", "historywords", "funcfiletrace",
		"funcsourcetrace", "funcstack", "functrace", "keymaps", "errnos", "epochtime", "zsh_scheduled_events",
		"ZCURSES_COLORS", "ZCURSES_COLOR_PAIRS", "zcurses_attrs", "zcurses_colors", "zcurses_keycodes",
		"zcurses_windows", "zgdbm_tied", "watch", "WATCH", "WATCHFMT", "LOGCHECK",
		"ZFTP_TMOUT", "ZFTP_IP", "ZFTP_HOST", "ZFTP_PORT", "ZFTP_SYSTEM", "ZFTP_TYPE", "ZFTP_USER", "ZFTP_ACCOUNT",
		"ZFTP_PWD", "ZFTP_CODE", "ZFTP_REPLY", "ZFTP_SESSION", "ZFTP_PREFS", "ZFTP_VERBOSE",
		"reset_color", "bold_color", "prompt_opts",
	}},
	{traits: PromptParameter, names: []string{
		"PROMPT", "PROMPT2", "PROMPT3", "PROMPT4", "prompt", "RPROMPT", "RPS1", "RPROMPT2", "RPS2", "SPROMPT",
		"PROMPT_EOL_MARK",
	}},
	{traits: AssociativeParameter, names: []string{
		"registers", "compstate", "opt_args",
		"options", "commands", "functions", "dis_functions", "functions_source", "dis_functions_source",
		"builtins", "dis_builtins", "aliases", "dis_aliases", "galiases", "dis_galiases", "saliases",
		"dis_saliases", "parameters", "modules", "history", "jobdirs", "jobtexts", "jobstates", "nameddirs",
		"userdirs", "usergroups", "widgets", "terminfo", "termcap", "sysparams", "mapfile", "langinfo",
		"color", "colour", "fg", "fg_bold", "fg_no_bold", "bg", "bg_bold", "bg_no_bold",
		"hook_com", "backend_misc", "user_data",
	}},
})...)

var knownKsh = newKnownParameters(nil, slices.Concat(knownShellGroups, []knownGroup{
	{names: []string{
		"KSH_VERSION", "FPATH", "HISTEDIT", "ENV", "PS4", "SECONDS", "RANDOM", "REPLY", "TMOUT",
		"PATHSEP", "USER_ID", "KSHEGID", "KSHGID", "KSHUID", "EPOCHREALTIME", "BASHPID", "PIPESTATUS",
		".sh.command", ".sh.dollar", ".sh.edchar", ".sh.edcol", ".sh.edmode", ".sh.edtext", ".sh.file",
		".sh.fun", ".sh.level", ".sh.lineno", ".sh.match", ".sh.math", ".sh.name", ".sh.pid", ".sh.pool",
		".sh.ppid", ".sh.pwdfd", ".sh.sig", ".sh.stats", ".sh.subscript", ".sh.subshell", ".sh.value",
		".sh.version",
	}},
})...)

var knownByDialect = map[dialect.Dialect]ParameterTable{dialect.Zsh: knownZsh, dialect.Ksh: knownKsh}

func KnownParameters(d dialect.Dialect) ParameterTable {
	if t, ok := knownByDialect[d]; ok {
		return t
	}
	return knownShell
}
