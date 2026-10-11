# Native rules

Besides ShellCheck's checks, which keep their `SC` codes, shell-lsp has rules
of its own. Their codes start with `SL`, and they are configured the same way:
`# shellcheck disable=SL1001` silences one, `lint.exclude` drops one for a
workspace, and optional rules are switched on with `enable=` or
`lint.optional`.

The injection rules follow values that come from outside the script—its
command-line arguments, `getopts` option values (`OPTARG`) and input taken with
`read`, `mapfile` or `readarray`—through assignments, loops and function
arguments. They only fire when such a value is pasted into a larger piece of
code or a path. Running a whole argument (`eval "$1"`, `bash -c "$1"`,
`. "$1"`) is treated as deliberate. A value counts as checked once it has been
tested with `[[ … =~ … ]]`, a `[[ … == pattern ]]` match or a `case` statement
earlier in the same function. Command output (`$(…)`) is not treated as
outside data.

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

**Untrusted data is evaluated as arithmetic** (optional:
`arithmetic-injection`). Bash and ksh evaluate array subscripts inside
arithmetic, so a value like `a[$(reboot)]` runs a command in `(( n > 3 ))`,
`$(( n + 1 ))` and `[[ $n -eq 3 ]]`. zsh evaluates the subscript without
running the command, and POSIX sh, dash and busybox sh have no arrays, so the
rule only applies to bash and ksh scripts. `[ "$n" -eq 3 ]` is safe because `test`
only accepts plain integers.

```bash
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
trap 'rm -rf -- "$work"' EXIT
```

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
