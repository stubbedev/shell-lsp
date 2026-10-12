# Native rules

Besides ShellCheck's checks, which keep their `SC` codes, shell-lsp has rules
of its own. Their codes start with `SL`, and they are configured the same way:
`# shellcheck disable=SL1001` silences one, `lint.exclude` drops one for a
workspace, and optional rules are switched on with `enable=` or
`lint.optional`. [checks.md](checks.md) lists every check, `SC` and `SL`, with
its severity and a one-line summary.

The injection rules follow values that come from outside the script—its
command-line arguments, `getopts` option values (`OPTARG`) and input taken with
`read`, `mapfile` or `readarray`—through assignments, loops and function
arguments. They only fire when such a value is pasted into a larger piece of
code or a path. Running a whole argument (`eval "$1"`, `bash -c "$1"`,
`. "$1"`) is treated as deliberate. A value counts as checked once it has been
tested with `[[ … =~ … ]]`, a `[[ … == pattern ]]` match or a `case` statement
earlier in the same function. Command output (`$(…)`) is not treated as
outside data, and neither is what `printf -v` writes with a fixed format whose
conversions are all numeric, such as `printf -v code '%d' "'$char"`, since that
is only ever a number.

## SL1001

**eval runs untrusted data as code.** A value from outside the script is
pasted into the code `eval` runs, so a value like `x; rm -rf ~` runs commands.

```bash
name=$1
eval "echo hello $name"    # SL1001
```

Pass the value as data instead, for example `printf 'hello %s\n' "$name"`, or
check it against a fixed set of values before it reaches `eval`.

## SL1002

**A shell -c script is built from untrusted data.** A value from outside the
script is pasted into the script that `sh -c`, `bash -c`, `su -c` (or one of
them behind `sudo`, `doas`, `env`, `nohup`, `nice`, `timeout` or `setsid`)
runs.

```bash
sudo sh -c "echo $1 > /etc/motd"    # SL1002
sudo sh -c 'echo "$1" > /etc/motd' _ "$1"    # safe: $1 stays an argument
```

## SL1003

**Untrusted data is evaluated as arithmetic.** The rule is optional
(`arithmetic-injection`). Bash and ksh evaluate array subscripts inside
arithmetic, so a value like `a[$(reboot)]` runs a command in `(( n > 3 ))`,
`$(( n + 1 ))` and `[[ $n -eq 3 ]]`. zsh evaluates the subscript without
running the command, and POSIX sh, dash and busybox sh have no arrays, so the
rule only applies to bash and ksh scripts. `[ "$n" -eq 3 ]` is safe because `test`
only accepts plain integers.

```bash
#!/bin/bash
# shellcheck enable=arithmetic-injection
n=$1
(( n > 3 ))    # SL1003
[[ $n =~ ^[0-9]+$ ]] || exit 1    # validated from here on
```

## SL1004

**A sourced path is built from untrusted data.** A value from outside the
script is pasted into the path that `source` or `.` loads, so `../` or an
absolute path can make it run any file.

```bash
. "/etc/app/$1.conf"    # SL1004: $1=../../tmp/evil
```

## SL1005

**Downloaded code runs unchecked.** A script fetched with `curl` or `wget` is
piped straight into a shell, run with `sh -c "$(curl …)"`, `bash <(curl …)`,
`source <(curl …)` or `. /dev/stdin`, or passed to `eval`, so whatever the
server or anyone on the way sends is executed, and a connection cut halfway
can run half a script. Downloads written to a file (`curl -o file`,
`curl -O`, `wget` without `-O -`) are not reported, because the file can be
checked before it runs.

```bash
curl -fsSL https://example.com/install.sh | sh    # SL1005
curl -fsSLo install.sh https://example.com/install.sh
echo "$expected_sha256  install.sh" | sha256sum -c - && sh install.sh
```

## SL1006

**A sensitive path is made writable by every user.** `chmod`, `mkdir -m` or
`install -m` gives others write permission (`777`, `666`, `0777`, `2777`,
`o+w`, `a+w`, `a=rwx` and the like) on a path that other users must not be
able to change: the root directory, a system directory such as `/etc`,
`/usr`, `/bin`, `/sbin`, `/boot`, `/root` or anything under them, `/var` or
`/home` themselves, a home directory (`~`, `$HOME`, `/home/name`), a shell
startup file in it such as `~/.bashrc`, `~/.ssh` or `~/.gnupg` and their
contents, `authorized_keys`, or an SSH private key such as `id_ed25519`.
Anyone on the machine can then replace a program, add a key that lets them
log in, or put commands in a file that runs as the owner. The command is
also found behind `sudo`, `env` and the other wrappers. Symbolic modes
without `o` or `a`, such as `+w`, depend on the umask and are not reported;
nor are modes that cannot be read, such as `"$mode"`, or a `umask`, which
is not tied to any path.

```bash
sudo chmod -R 777 /etc/nginx    # SL1006
chmod 666 ~/.ssh/authorized_keys    # SL1006
chmod 600 ~/.ssh/authorized_keys
```

## SL2001

**A function in a script is never called.** Nothing in the script, in the
files it sources, or in functions those call ever runs the function, so it is
dead code. Only executed scripts are checked: files with a `#!` line whose top
level does real work. Libraries that only define functions and settings, and
scripts that detect being sourced (through `BASH_SOURCE`, `ZSH_EVAL_CONTEXT`,
zsh's `${(%):-%x}`, `$0` or a top-level `return`), are left alone because
their functions are meant for other files. So are startup files such as
`.zshrc` or `.bashrc` and files that set up an interactive shell (with
`compinit`, `promptinit`, `compdef`, `add-zsh-hook`, `bindkey`, `bind` or
`complete`), whose functions are commands for the user to type.
The name counts as used wherever it appears as a word, including inside
strings such as `trap 'cleanup' EXIT`. A command name built at run time, like
`"cmd_$1"`, counts as a use of every function that starts with `cmd_`; running
`"$@"` as a command, `eval`, or listing functions with `compgen -A function`
or `declare -F` turns the check off for the file. Hook functions the shell
calls itself, such as `command_not_found_handle`, `precmd` or `TRAPINT`, are
never reported.

```bash
#!/bin/bash
helper() { echo help; }    # SL2001
main() { echo "$1"; }
main "$@"
```

## SL2002

**An `@` transformation the shell does not know.** `${name@op}` applies one
of a fixed set of transformations: `U`, `u`, `L`, `Q`, `E`, `P`, `A`, `K`, `a`
and `k` in bash, `Q` and `#` in mksh. Bash only checks the operator when it
expands the parameter, so the script parses and an unset variable expands to
nothing, but as soon as the variable has a value the expansion fails with
"bad substitution" and the command does not run. The usual cause is a typo,
or a template placeholder such as `${PREFIX_@suffix@}` left in a script
meant to be filled in first.

```bash
#!/bin/bash
name=world
echo "${name@q}"    # SL2002: the operator is Q
```

## SL2003

**A declaration assigns a name that is not a variable name.** `export`,
`declare`, `local`, `readonly` and `typeset` take `name=value` arguments, but
the shell parses an argument like `my-var=1` or `1st=a` as an ordinary word
and the command fails at run time with "not a valid identifier", so nothing
is assigned and, under `set -e`, the script stops. Arguments whose name comes
from an expansion (`export "$name=value"`) or contains a backslash are left
alone, since the shell expands them before checking.

```bash
#!/bin/bash
export my-var=1    # SL2003: write my_var
```

## SL2004

**A trap is set inside a subshell.** A command substitution and every
pipeline stage that runs in a subshell (all but the last in bash without
`lastpipe`) get their own copy of the traps, so a `trap` set there only fires
for that subshell and is gone once it ends. Saving traps with `$(trap -p)`,
resetting one with `trap - SIG`, and traps inside an explicit `( … )` subshell,
which are usually meant for that subshell, are not reported.

```bash
#!/bin/bash
seq 3 | while read -r n; do trap 'echo bye' EXIT; done    # SL2004
```

## SL2005

**exit only leaves a subshell and nobody checks its status.** `exit` inside a
command substitution or a pipeline stage ends that subshell, not the script.
The status it leaves behind still reaches the script when the substitution is
the value of a plain assignment or the stage is the last of the pipeline (or
`pipefail` is on), so the rule stays quiet when that status is checked: by
`set -e`, `!`, `&&`, `||`, an `if` or `while` condition, a following `$?` or
`PIPESTATUS`, or because the statement is the last one in its block.

```bash
#!/bin/bash
dir=$(cd "$1" || exit 1; pwd)    # SL2005: the script goes on with an empty $dir
dir=$(cd "$1" && pwd) || exit 1  # checked
```

## SL2006

**A temporary file or directory is never removed.** `mktemp` creates the file
or directory, but nothing passes the variable that holds it to `rm`, `rmdir`,
`mv` or a cleanup function, and no `trap` names it, so every run leaves it
behind. Only executed scripts are checked, as for SL2001. The variable counts
as handled once it is copied into another variable or array, reassigned,
printed or exported, and `mktemp -u`, `-p DIR` and templates outside the
temporary directory are left alone. The fix adds an `EXIT` trap that removes
it, when the script has no `EXIT` trap yet.

```bash
#!/bin/bash
work=$(mktemp -d)    # SL2006
cp -R src "$work"
```

The fix inserts `trap 'rm -rf -- "$work"' EXIT` after the assignment.

## SL2007

**A function runs pushd but never popd.** Every call leaves its caller in
another directory with one more entry on the directory stack. `pushd` inside a
subshell, `pushd` without a directory, `pushd -n`, stack rotations, and
scripts that pop the stack outside the function (a `leave` function or a
`popd` at the call site) are not reported.

```bash
#!/bin/bash
build() {
  pushd "$1" >/dev/null || return    # SL2007
  make
}
```

## SL2008

**A `${…}` the shell rejects when it runs it.** Bash, sh and mksh accept some
malformed expansions when they read the script, such as `${}` or a template
placeholder like `${@name@}`, and only fail with "bad substitution" when the
line runs, so the script stops there. Forms with a more specific explanation
are reported under their own codes instead: SC2082 for `${$x}`, SC2296 for a
name that starts with a character no parameter can start with.

```bash
#!/bin/bash
echo "${}"    # SL2008
```

## SL2009

**A disabled check that nothing here reports.** A `# shellcheck disable=`
directive lists a code, or a range of codes, that no finding in the lines it
covers has, so it silences nothing; often the problem was fixed and the
directive stayed behind. Codes the linter does not have at all are reported
too. `disable=all`, optional checks that are not turned on, and the checks
whose findings depend on whether sourced files can be found (SC1090, SC1091,
SC1094 and SC2154) are left alone. The quick fix removes the code, or the
whole directive when nothing in it is used.

```bash
#!/bin/bash
# shellcheck enable=unused-directives
# shellcheck disable=SC2086    # SL2009: the argument below is already quoted
echo "$1"
```

## SL2010

**A NUL byte cuts a `$'...'` string short.** Shell variables and command
arguments are C strings, so an escape that produces a NUL byte (`\0`,
`\x00`, `\u0000`, `\c@`) ends the string there and everything after it is
silently dropped: `$'a\x00b'` is just `a`. zsh can hold NUL bytes and is not
checked.

```bash
#!/bin/bash
marker=$'\xd8\x00'    # SL2010: marker holds only \xd8
printf 'a\0b' > record    # a NUL byte written to a file survives
```

## SL2011

**Two bounds on the same value leave no number in between.** In `(( ))`,
`$(( ))` and the other arithmetic contexts, an `&&` chain that requires a
variable to be at least one constant and at most a smaller one can never be
true, which usually means a typo in one of the bounds. Constants are read the
way the shell reads them, in decimal, octal (`010`), hexadecimal (`0x1f`) or
an explicit base (`2#1010`).

```bash
#!/bin/bash
(( cp >= 0xd800 && cp <= 0xdff )) && return 1    # SL2011: 0xdff < 0xd800; meant 0xdfff
```

## SL2012

**`&&` and `||` are mixed in `[[ ]]` without parentheses.** The rule is
optional (`require-test-grouping`). Inside `[[ ]]`,
`&&` binds tighter than `||`, so `[[ $a && $b || $c ]]` means
`[[ ( $a && $b ) || $c ]]` and `[[ $a || $b && $c ]]` means
`[[ $a || ( $b && $c ) ]]`. That is well defined, but readers often take the
operators to apply left to right, and among shell commands `a && b || c`
does exactly that. The quick fix adds the parentheses the shell already
implies, so it never changes what the test does. It is optional because
such tests are usually written on purpose, like the common version check
`[[ $major -eq 4 && $minor -ge 4 || $major -gt 4 ]]`.

```bash
#!/bin/bash
# shellcheck enable=require-test-grouping
[[ -f $conf && -r $conf || -n $default ]]    # SL2012
[[ ( -f $conf && -r $conf ) || -n $default ]]
```

## SL2013

**A sourced file is found relative to where the script is run from.** The
rule is optional (`check-relative-sources`). `. ./lib.sh` and
`source lib/common.sh` resolve the path against the working directory of
whoever runs the script, so run from another directory the script loads a
different file or none. A name without a slash, such as `. lib.sh`, is
looked up in `PATH`; bash, and zsh's `source`, then also try the working
directory, while sh, dash, busybox, ksh and zsh's `.` never look next to the
script. The path is worked out the way following sourced files works it
out, so a path built from the script's own location, such as
`"$(dirname "$0")/lib.sh"`, `"${BASH_SOURCE%/*}/lib.sh"` or `"$here/lib.sh"`
with `here=$(cd "$(dirname "$0")" && pwd)`, is fine, and so is a path only
known at run time, one with a default the caller can override
(`"${srcdir:-.}/lib.sh"`) and a build-time placeholder such as `@out@`.
Nothing is reported after the script changes directory with `cd` or
`pushd`, for a sourced file named with a `# shellcheck source=` directive,
in shell startup files such as `.bashrc`, which the shell reads from a
fixed place, or in Bats files, whose tests run from the directory the test
runner is started in. The quick fix anchors the path with `dirname`.

The rule is optional because many test suites, such as the Linux kernel
selftests, are always run from their own directory and source their
helpers by bare name.

```bash
#!/bin/bash
# shellcheck enable=check-relative-sources
. ./lib.sh    # SL2013
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
```

The embedded-language rules check awk, sed and jq programs written inside the
script with built-in parsers; no awk, sed or jq binary is run. A program is
checked when it is a literal argument (`awk '...'`, `sed -e '...'`,
`jq -r '...'`) or a quoted here-document fed to `awk -f -` or `sed -f -`,
also when the command runs behind `sudo`, `env`, `nice`, `timeout`, `xargs`,
`find -exec` and the other wrappers. Programs that contain shell expansions,
come from files, or contain the `{}` or `xargs -I` placeholder substituted
at run time are skipped rather than guessed at.

Each error is reported at its exact position inside the shell string. After
an error the parser resumes at the next line of a sed script, the next
statement of an awk program or the next top-level `|` stage of a jq filter,
so up to ten errors are reported per program; where it cannot tell how the
program continues, such as after a string that is never closed, it stops
rather than report follow-on errors.

## SL3001

**An awk program does not parse.** The program given to `awk`, `gawk`,
`mawk`, `nawk` or `original-awk` has a syntax error, so awk refuses to run
it. Common gawk extensions (`BEGINFILE`, `switch`, `@include`, `**`, `|&`)
are accepted.

Regular expression constants (`/re/`) and string literals on the right of
`~` or `!~` are read as extended regular expressions after awk's own string
escapes, and only mistakes every awk rejects are reported: a group or
bracket expression that is never closed, an unknown character class, an
empty range such as `[z-a]` and a trailing lone `\`. Constructs that awks
read differently, such as a leading `*`, an unmatched `)` or braces, are
left alone.

```bash
awk '{ print $1 ' file.txt    # SL3001: `{` is never closed
awk '$2 ~ /^(GET|POST/' log    # SL3001: `(` is never closed
```

## SL3002

**A sed script does not parse.** The script given to `sed` or `gsed` has a
syntax error, so sed refuses to run it. GNU extensions such as `0,/re/`,
`first~step`, `addr,+N`, `T`, `e`, `F` and `z` are accepted.

The regular expressions of addresses and `s` commands are read as POSIX
basic expressions, or extended ones under `-E`, `-r` or `--regexp-extended`,
and only definite mistakes are reported: a group, bracket expression or
`\{` interval that is never closed, a `\)` without its `\(`, an unknown
character class, an empty range such as `[z-a]`, an interval whose minimum
is above its maximum, a quantifier with nothing to repeat in an extended
expression, and a back-reference such as `\2` to a group the expression
does not have, in the expression or in the replacement of `s`.

```bash
sed 's/a/b/x' file.txt    # SL3002: unknown option to `s`
sed 's/\(.*\)=\(.*\)/\2=\3/' conf    # SL3002: there is no group 3
```

## SL3003

**A jq filter does not parse.** The filter given to `jq`, `gojq` or `jaq`
has a syntax error, so jq refuses to run it.

The pattern of `test`, `match`, `capture`, `scan`, `split/2`, `splits`, `sub`
and `gsub` is checked when it is a single string literal and the flags, if
given, are a literal without `x`. jq compiles patterns with Oniguruma only
when the filter runs, so these are run-time errors; only clear ones are
reported: a group or bracket expression that is never closed, a `)` without
its `(`, a quantifier with nothing to repeat, an empty range such as
`[z-a]`, an interval whose minimum is above its maximum and a trailing lone
`\`.

```bash
jq '.items[] | | .name' data.json    # SL3003: unexpected `|`
jq '.[] | select(.id | test("^(a|b"))' data.json    # SL3003: `(` is never closed
```
