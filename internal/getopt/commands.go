package getopt

import "maps"

func longs(arity Arity, names ...string) map[string]Arity {
	out := make(map[string]Arity, len(names))
	for _, name := range names {
		out[name] = arity
	}
	return out
}

func merged(tables ...map[string]Arity) map[string]Arity {
	out := map[string]Arity{}
	for _, table := range tables {
		maps.Copy(out, table)
	}
	return out
}

var standardLongs = longs(Flag, "help", "version")

var mapfile = Grammar{Short: "d:n:O:s:tu:C:c:", StopAtOperand: true}

var grep = Grammar{Short: "0123456789A:B:C:D:EFGHIPTUVX:abcd:e:f:hiLlm:noqRrsuvwxyZz", Long: merged(standardLongs,
	longs(Flag, "extended-regexp", "fixed-strings", "basic-regexp", "perl-regexp", "ignore-case", "no-ignore-case",
		"word-regexp", "line-regexp", "null-data", "no-messages", "invert-match", "byte-offset", "line-number",
		"line-buffered", "with-filename", "no-filename", "only-matching", "quiet", "silent", "text", "recursive",
		"dereference-recursive", "files-without-match", "files-with-matches", "count", "initial-tab", "null",
		"no-group-separator", "binary"),
	longs(Valued, "regexp", "file", "max-count", "label", "binary-files", "directories", "devices", "include", "exclude",
		"exclude-from", "exclude-dir", "before-context", "after-context", "context", "group-separator"),
	longs(Optional, "color", "colour"),
)}

var Commands = map[string]Grammar{
	"builtin": {StopAtOperand: true},
	"busybox": {StopAtOperand: true},
	"command": {Short: "pVv", StopAtOperand: true},
	"exec":    {Short: "cla:", StopAtOperand: true},
	"read":    {Short: "Eersa:d:i:n:N:p:t:u:", StopAtOperand: true},
	"mapfile": mapfile, "readarray": mapfile,
	"printf": {Short: "v:", StopAtOperand: true},
	"wait":   {Short: "fnp:", StopAtOperand: true},

	"time": {Short: "af:o:pqvhV", StopAtOperand: true, Long: merged(standardLongs,
		longs(Flag, "append", "portability", "quiet", "verbose"),
		longs(Valued, "format", "output"),
	)},
	"sudo": {Short: "ABbEeHiKklNnPSsVva:C:c:D:g:h:p:R:r:T:t:U:u:", StopAtOperand: true, Long: merged(
		longs(Flag, "askpass", "background", "bell", "edit", "set-home", "help", "login", "remove-timestamp", "reset-timestamp",
			"list", "no-update", "non-interactive", "preserve-groups", "stdin", "shell", "version", "validate"),
		longs(Valued, "auth-type", "close-from", "login-class", "chdir", "group", "host", "prompt", "chroot", "role", "type",
			"other-user", "command-timeout", "user"),
		longs(Optional, "preserve-env"),
	)},
	"doas": {Short: "Lnsa:C:u:", StopAtOperand: true},
	"run0": {Short: "hVig:u:D:", StopAtOperand: true, Long: merged(
		longs(Flag, "help", "version", "no-ask-password", "slice-inherit", "via-shell", "pty", "pty-late", "pipe", "empower"),
		longs(Valued, "machine", "unit", "property", "description", "slice", "user", "group", "nice", "chdir", "setenv",
			"background", "shell-prompt-prefix", "lightweight", "area"),
	)},
	"env": {Short: "0ivL:P:S:U:u:a:C:", StopAtOperand: true, Long: merged(standardLongs,
		longs(Flag, "ignore-environment", "null", "list-signal-handling", "debug"),
		longs(Valued, "argv0", "unset", "chdir", "split-string"),
		longs(Optional, "block-signal", "default-signal", "ignore-signal"),
	)},
	"nice":   {Short: "n:", StopAtOperand: true, Long: merged(standardLongs, longs(Valued, "adjustment"))},
	"nohup":  {StopAtOperand: true, Long: standardLongs},
	"setsid": {Short: "cfw", StopAtOperand: true, Long: merged(standardLongs, longs(Flag, "ctty", "fork", "wait"))},
	"timeout": {Short: "fk:ps:v", StopAtOperand: true, Long: merged(standardLongs,
		longs(Flag, "foreground", "preserve-status", "verbose"),
		longs(Valued, "kill-after", "signal"),
	)},
	"watch": {Short: "bcCd::efgq:n:prs:twxhv", StopAtOperand: true, Long: merged(standardLongs,
		longs(Flag, "beep", "color", "no-color", "errexit", "follow", "chgexit", "precise", "no-rerun", "no-title", "no-wrap", "exec"),
		longs(Valued, "equexit", "interval", "shotsdir"),
		longs(Optional, "differences"),
	)},
	"xargs": {Short: "0a:d:E:e::I:i::J:L:l::n:oP:prR:S:s:tx", StopAtOperand: true, Long: merged(standardLongs,
		longs(Flag, "null", "open-tty", "interactive", "no-run-if-empty", "show-limits", "verbose", "exit"),
		longs(Valued, "arg-file", "delimiter", "max-lines", "max-args", "max-procs", "process-slot-var", "max-chars"),
		longs(Optional, "eof", "replace"),
	)},

	"grep": grep, "egrep": grep, "fgrep": grep,
	"mkdir": {Short: "m:pvZ", Long: merged(standardLongs,
		longs(Flag, "parents", "verbose"),
		longs(Valued, "mode"),
		longs(Optional, "context"),
	)},
	"chmod": {Short: "cfvR", Long: merged(standardLongs,
		longs(Flag, "changes", "silent", "quiet", "verbose", "recursive", "preserve-root", "no-preserve-root"),
		longs(Valued, "reference"),
	)},
	"install": {Short: "bcCdDg:m:o:psS:t:TvZ", Long: merged(standardLongs,
		longs(Flag, "compare", "directory", "preserve-timestamps", "strip", "no-target-directory", "verbose",
			"preserve-context", "debug"),
		longs(Valued, "group", "mode", "owner", "strip-program", "suffix", "target-directory"),
		longs(Optional, "backup", "context"),
	)},
	"curl": {Short: "aqfGgI0k46jlLMnNZ#pJORSis231BvVhA:b:c:C:d:D:e:E:F:H:K:m:o:P:Q:r:t:T:u:U:w:x:X:Y:y:z:", Long: merged(
		longs(Flag, "remote-name", "remote-name-all"),
		longs(Valued, "output", "output-dir"),
	)},
	"wget": {Short: "VhbdqvFcNS46xErkKmpHLe:o:a:i:B:t:O:T:w:Q:P:U:l:A:R:D:I:X:", Long: longs(Valued, "output-document")},
	"su": {Short: "mpw:g:G:lc:fs:PThV", Long: merged(standardLongs,
		longs(Flag, "preserve-environment", "login", "fast", "pty", "no-pty"),
		longs(Valued, "whitelist-environment", "group", "supp-group", "command", "session-command", "shell"),
	)},
	"ssh": {Short: "46AaCfGgKkMNnqsTtVvXxYyB:b:c:D:E:e:F:I:i:J:L:l:m:O:o:P:p:Q:R:S:W:w:"},
}
