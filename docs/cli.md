# Command line

Besides serving LSP, the `shell-lsp` binary lints and formats scripts from
the command line, so the checks an editor shows can gate CI and commits.
Both commands run the code paths the server runs: `check` reports what the
editor's diagnostics report, and `fmt` writes what the editor's formatting
writes.

```
shell-lsp                 # serve LSP over stdio (also: shell-lsp serve, --stdio)
shell-lsp check [OPTIONS] FILE|DIR|- ...
shell-lsp fmt [FLAGS] [PATH ...]
shell-lsp version
shell-lsp help [check|fmt]
```

## check

`shell-lsp check` is a drop-in for the `shellcheck` command line: same
options, same `.shellcheckrc` lookup, same output formats and same exit codes.
Directories are searched for shell scripts the way the editor's workspace
scan finds them: by extension (`.sh`, `.bash`, `.zsh`, `.ksh`, `.dash`,
`.bats`), by rc-file name (`.bashrc`, `.zshrc`, ...), and by a shell shebang on
executable files without an extension (on Windows, which has no execute bit,
on every file without one). Makefiles (`Makefile`, `makefile`, `GNUmakefile`,
`*.mk`, `*.mak`), justfiles (`justfile`, `Justfile`, `.justfile`, `*.just`)
and Nix files (`*.nix`) are found too, and the shell in them is linted
([Shell in other files](../README.md#shell-in-other-files)); `-s` does not
apply to them, since the file decides their shell. `.git`, `node_modules`,
`vendor` and similar directories are skipped, and so is whatever git ignores:
the `.gitignore` files from the repository's top down, and
`.git/info/exclude`. A file named on the command line is linted even when it
is ignored. A `-` reads the script from
standard input. A file named more than once, directly or through a directory,
is linted once.
Files are linted in parallel, and the report is ordered by path, then by
position, so it is the same on every run.

| Option | Meaning |
| --- | --- |
| `-f`, `--format=FORMAT` | `tty` (default), `gcc`, `json`, `json1`, `checkstyle`, `diff`, `quiet` or `sarif` |
| `-s`, `--shell=SHELL` | Lint every file as `sh`, `bash`, `dash`, `ksh`, `busybox` or `zsh` |
| `-S`, `--severity=LEVEL` | Report only findings at least this severe: `error`, `warning`, `info` or `style` |
| `-e`, `--exclude=CODES` | Drop these codes, such as `SC2034,2086` or `SL1001`, or a range such as `SC2000-SC2100` that leaves out its end; repeatable |
| `-i`, `--include=CODES` | Report only these codes; repeatable |
| `-o`, `--enable=CHECKS` | Enable optional checks by name, or `all`; `--list-optional` lists them with the checks each turns on |
| `--profile=PROFILE` | `recommended` (default), `minimal` or `strict`, as described under [Profiles](../README.md#profiles); `--enable`, `--exclude` and directives apply on top |
| `--extended-analysis=BOOL` | Turn the data-flow checks off with `false` |
| `-P`, `--source-path=DIRS` | Also look for sourced files in these directories (`SCRIPTDIR` is the script's own) |
| `-a`, `--check-sourced` | Also report the findings in the files the scripts source |
| `-x`, `--external-sources` | Accepted for compatibility; sourced files are always followed |
| `-C`, `--color[=WHEN]` | Color the `tty` report: `auto` (default, when stdout is a terminal), `always` or `never` |
| `-W`, `--wiki-link-count=N` | Number of reference links at the end of the `tty` report (default 3) |
| `--norc` | Do not read a `.shellcheckrc` |
| `--rcfile=FILE` | Read this file instead of searching for a `.shellcheckrc` |
| `-V`, `--version` | Print the version |

Short options combine (`-xaf gcc`), take attached values (`-Swarning`), and
long options may be abbreviated while they stay unambiguous (`--sev=error`).

### Exit status

| Status | Meaning |
| --- | --- |
| 0 | Every file was read and nothing was reported |
| 1 | Findings were reported |
| 2 | A file could not be read; the others were still checked |
| 3 | The command line is malformed, such as an unknown option or no files |
| 4 | An option value or a `.shellcheckrc` is invalid |

### .shellcheckrc

Unless `--norc` is given, the nearest `.shellcheckrc` or `shellcheckrc` in the
script's directory or one of its parents applies to it, and failing that
`$XDG_CONFIG_HOME/shellcheckrc` (`~/.config/shellcheckrc`) or
`~/.shellcheckrc`. Its lines are directive settings without the
`# shellcheck` prefix, and blank lines and `#` comments are skipped:

```
# Team defaults
disable=SC2034,SC1090-SC1092
enable=require-variable-braces
shell=bash
source-path=SCRIPTDIR:lib
external-sources=true
extended-analysis=false
```

`disable` takes codes, ranges and `all`; as in ShellCheck, a range leaves out
its end, so `SC1090-SC1092` covers SC1090 and SC1091; `source-path` entries are relative to
the rc file and, like `--source-path`, separated by `:` (`;` on Windows, as in
`PATH`). On Windows the fallbacks are `%USERPROFILE%\.config\shellcheckrc` and
`%USERPROFILE%\.shellcheckrc` unless `XDG_CONFIG_HOME` is set. Options on the command line add to the rc file's settings, and
`--shell` and `--extended-analysis` win over it. A line that is not a
`key=value` setting, an unknown key or an invalid value stops the run with
status 4 and names the file and line; ShellCheck would instead report
SC1134 in every script.

A `.shellcheckrc` has no profile setting, so the same file keeps working with
ShellCheck; choose a profile with `--profile` or the editor's
`lint.profile` setting instead.

### Output formats

`tty`, `gcc`, `checkstyle`, `json`, `json1` and `diff` follow ShellCheck's
layout, so tools that parse ShellCheck's output read these too:

- `json` and `json1` carry each finding's safe fix, the one fix-all would
  apply, as ShellCheck's `replacements`.
  Codes are numbers as in ShellCheck; shell-lsp's own `SL` codes appear as
  10000 plus their number, so SL1001 is `11001` and never collides with an
  `SC` code.
- `diff` prints a unified diff of every safe fix, the same edits the
  editor's "Apply all automatic fixes" action makes; pipe it to `git apply`
  or `patch -p1`. Fixes that change what a script does, such as quoting
  `$1` or adding `|| exit`, are left out; the editor offers them as quick
  fixes.
- `sarif` writes SARIF 2.1.0, with a rule per code that links to its
  documentation, columns in Unicode code points, and every fix of a finding,
  safe or not, as a SARIF fix titled by what it changes.
  GitHub code scanning reads it directly.

A script that fails to parse is reported as SC1072, ShellCheck's code for a
parse failure, with the parser's message.

### Differences from ShellCheck

- The messages are shell-lsp's own, and the set of checks is the one in
  [rules.md](rules.md), including the native `SL` checks and zsh.
- A shell is named by a shebang, a `shell=` directive, the file name
  (`.sh` is POSIX sh, `.zsh` is zsh), `.editorconfig`'s `shell_variant`, or
  `--shell`. ShellCheck ignores `.sh` and does not lint zsh, so it reports
  SC2148 for scripts that shell-lsp lints as sh or zsh.
- Sourced files are always followed (`-x` is implied), from the script's
  directory, the working directory and the `-P` directories.
- Directories on the command line are searched instead of rejected.
- `--profile` has no ShellCheck counterpart.
- A broken `.shellcheckrc`, an unreadable `--rcfile` and an unknown optional
  check name are reported instead of ignored; unknown optional names only
  warn, so an rc file written for ShellCheck keeps working.

## fmt

`shell-lsp fmt` takes shfmt's flags and exit codes and formats with the same
printer, so `shfmt` can be swapped for it in scripts and hooks. With no paths,
or a single `-`, it formats standard input.

| Flag | Meaning |
| --- | --- |
| `-l[=0]`, `--list[=0]` | List the files whose formatting differs, separated by newlines or, with `=0`, null bytes |
| `-w`, `--write` | Write the result back to the file, through a temporary file and a rename |
| `-d`, `--diff` | Print a unified diff of the changes |
| `--apply-ignore` | Skip files that `.editorconfig` marks `ignore = true` even when named explicitly |
| `--filename=NAME` | Name standard input, for `.editorconfig` and dialect detection |
| `-ln`, `--language-dialect=LANG` | `bash`, `posix`, `mksh`, `bats`, `zsh`, `ksh`, `dash` or `auto` (default) |
| `-p`, `--posix` | Short for `-ln=posix` |
| `-s`, `--simplify` | Simplify the code |
| `-i`, `--indent=N` | Indent with N spaces; 0 (default) indents with tabs |
| `-bn`, `--binary-next-line` | Start continuation lines with `&&`, `\|\|` and `\|` |
| `-ci`, `--case-indent` | Indent `case` arms |
| `-sr`, `--space-redirects` | Write `> file` instead of `>file` |
| `-kp`, `--keep-padding` | Keep column-alignment padding |
| `-fn`, `--func-next-line` | Put the opening brace of a function on its own line |
| `-mn`, `--minify` | Minify the code (implies `-s`) |
| `-f[=0]`, `--find[=0]` | Print the shell files found under the paths instead of formatting |
| `--version` | Print the version |

As with shfmt, `.editorconfig` settings apply unless a parser or printer flag
is given, and files found by walking a directory honour `ignore = true`.
Makefiles, justfiles and Nix files are skipped when walking a directory and
refused when named, since `fmt` formats only shell scripts. A file named more
than once, directly or through a directory, is formatted once.
Exit status 0 means success, 1 means a file could not be read or parsed or,
with `-l` (without `-w`) or `-d`, that some formatting differs, and 2 means
the command line is malformed.

## Windows

The binary is `shell-lsp.exe`, and both commands take Windows paths and print
file names with the platform's `\` separators (SARIF always uses `/`). Paths
compare case-insensitively, so a file named twice in different case is
checked or formatted once. `fmt -w` writes a temporary file next to the script and
renames it over the original, as on other platforms; it fails, leaving the
script untouched, if the script is read-only or another program holds it open
without sharing it.

## In CI

GitHub Actions, with findings uploaded to code scanning:

```yaml
- uses: actions/setup-go@v7
  with:
    go-version: stable
- run: go install github.com/stubbedev/shell-lsp@latest
- run: shell-lsp fmt -d .
- run: shell-lsp check -f sarif . > shell-lsp.sarif
- uses: github/codeql-action/upload-sarif@v4
  if: ${{ !cancelled() }}
  with:
    sarif_file: shell-lsp.sarif
```

Any CI that understands `gcc`-style lines or checkstyle XML can use
`-f gcc` or `-f checkstyle` instead. On a Windows runner `go install` puts
`shell-lsp.exe` in `%USERPROFILE%\go\bin`, and the same commands work from
any shell.

## pre-commit

The repository ships [pre-commit](https://pre-commit.com) hooks that build
shell-lsp from source:

```yaml
repos:
  - repo: https://github.com/stubbedev/shell-lsp
    rev: v0.5.6
    hooks:
      - id: shell-lsp-check
      - id: shell-lsp-fmt
```

`shell-lsp-check` lints the staged shell scripts, and `shell-lsp-fmt`
formats them in place, which fails the commit when anything changed. On
Windows pre-commit builds `shell-lsp.exe` and finds it through `PATHEXT`, so
the same hooks work there.
