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
`arithmetic-injection`). Bash evaluates array subscripts inside arithmetic,
so a value like `a[$(reboot)]` runs a command in `(( n > 3 ))`,
`$(( n + 1 ))` and `[[ $n -eq 3 ]]`. `[ "$n" -eq 3 ]` is safe because `test`
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

## SL2001

**A function in a script is never called.** Nothing in the script, in the
files it sources, or in functions those call ever runs the function, so it is
dead code. Only executed scripts are checked: files with a `#!` line whose top
level does real work. Libraries that only define functions and settings, and
scripts that detect being sourced (through `BASH_SOURCE`, `$0` or a top-level
`return`), are left alone because their functions are meant for other files.
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
