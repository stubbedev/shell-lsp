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
scan finds them: by extension (`.sh`, `.bash`, `.zsh`, `.zsh-theme`, `.ksh`,
`.dash`, `.bats`), by rc-file name (`.bashrc`, `.zshrc`, `.envrc`, ...), and by a shell shebang on
executable files without an extension (on Windows, which has no execute bit,
on every file without one). Makefiles (`Makefile`, `makefile`, `GNUmakefile`,
`*.mk`, `*.mak`), justfiles (`justfile`, `Justfile`, `.justfile`, `*.just`),
Dockerfiles (`Dockerfile`, `Containerfile`, `Dockerfile.*`, `*.Dockerfile`,
`*.dockerfile`, `*.Containerfile`) and Nix files (`*.nix`) are found too, and the shell in them is linted
([Shell in other files](hosts.md#shell-in-other-files)); `-s` does not
apply to them, since the file decides their shell. Justfiles, Makefiles and
Dockerfiles are also checked as just, make and Dockerfile files, with the
`SL5`, `SL6` and `SL7` codes of [rules.md](rules.md#sl5001), following their `import`, `mod`
and `include` statements, and the other justfiles and Makefiles among the
paths that include them. Nix files are checked with the `SL8` codes of [rules.md](rules.md#sl8001), with `# shellcheck disable=` honoured in Nix comments. Dockerfiles are checked with Hadolint's `DL` rules
too, configured by the nearest `.hadolint.yaml` ([hadolint.md](hadolint.md)). `.git`, `node_modules`,
`vendor` and similar directories are skipped, and so is whatever git ignores:
the `.gitignore` files from the repository's top down, `.git/info/exclude`,
and the file `core.excludesFile` names ([Ignored files](#ignored-files)). A
file named on the command line is linted even when it is ignored. A `-` reads the script from
standard input, as shell unless `--filename=NAME` names it: the language, the
dialect and the settings then come from that path, as they would for a file
there, whether or not one exists, so `shell-lsp check --filename=pkg.nix -` lints
a Nix buffer and reports findings under that name. `fmt` takes the same flag. A file named more than once, directly or through a directory,
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
| `--profile=PROFILE` | `recommended` (default), `minimal` or `strict`, as described under [Profiles](linting.md#profiles); `--enable`, `--exclude` and directives apply on top |
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
| 4 | An option value, a `.shellcheckrc`, a `.hadolint.yaml` or a `statix.toml` is invalid |

### Ignored files

Inside a git repository, a directory walk, the server's workspace scan and the
search for bake files skip what git ignores, with git's precedence: a
`.gitignore` in a deeper directory wins over one above it, any `.gitignore`
wins over `.git/info/exclude`, and that wins over the file
`core.excludesFile` names. A `!pattern` in a `.gitignore` therefore brings
back what the other two exclude.

The repository of a directory is found as git finds it: the nearest `.git` in
the directory or one of its parents, whether a directory, a symbolic link to
one, or a `gitdir:` file of a linked worktree or submodule. A `.git`
directory counts only when git would take it for a repository, with a valid
`HEAD` and `objects` and `refs` directories; one without them is passed over
and the search goes on upwards. A `gitdir:` file git cannot read, or one that
names no repository, stops the walk with an error, as does a `.git` that is
neither a directory nor a file. The search stops below each directory listed in
`GIT_CEILING_DIRECTORIES` (absolute paths, resolved through symbolic links up
to an empty entry, as git resolves them), and at a filesystem boundary unless
`GIT_DISCOVERY_ACROSS_FILESYSTEM` is true; on Windows, as in Git for Windows,
there is no boundary. Both variables hold for the language server too: they
are settings a user keeps in a shell profile, not variables git sets for one
command.

The repository's `core.worktree`, read from its own `config` (or from
`config.worktree` under `extensions.worktreeConfig`) when
`core.repositoryFormatVersion` is set, moves the top of the work tree,
relative to the git directory; a linked worktree ignores the shared
`core.worktree` and `core.bare` unless `extensions.worktreeConfig` is set. A directory outside that work tree, or in a repository with
`core.bare` set, is in no repository, and a relative `core.worktree` that names
no directory stops the walk with an error.

`check` and `fmt` also honour the variables git sets for hooks, relative to
the working directory: `GIT_DIR` names the git directory (a `gitdir:` file
too), `GIT_WORK_TREE` the top of the work tree (else `core.worktree`, else the
working directory when `GIT_DIR` is set, or the top of the repository found
from the working directory), and `GIT_COMMON_DIR` the directory that holds the
repository's `config` and `info/exclude`. With any of them set, that one
repository is the repository of every walked directory inside its work tree,
and directories outside it are in none, as git sees them; an empty variable or
a `GIT_DIR` that is not a repository stops the walk with an error. The language
server ignores these three variables: its folders come from the editor, and an
editor started from a git hook or `git commit` would otherwise pass git's view
of that one command's repository to every folder. A repository whose
`core.repositoryFormatVersion` is above 1, which git refuses to work in, stops
the walk with an error.

`core.excludesFile` is read the way git reads its configuration, the last
setting winning:

1. the system configuration: the file `GIT_CONFIG_SYSTEM` names, or else
   `/etc/gitconfig`, git's default as Linux distributions and Nix build it,
   unless `GIT_CONFIG_NOSYSTEM` is true. For a git built with another
   default, and on Windows, where the default depends on where git is
   installed, set `GIT_CONFIG_SYSTEM` to its system file;
2. the global configuration: the file `GIT_CONFIG_GLOBAL` names, or else
   `$XDG_CONFIG_HOME/git/config` (`~/.config/git/config`) and then
   `~/.gitconfig`;
3. the repository's `.git/config`, shared by its linked worktrees;
4. the worktree's `config.worktree`, when the repository config sets
   `extensions.worktreeConfig`;
5. the settings in `GIT_CONFIG_COUNT`, `GIT_CONFIG_KEY_<n>` and
   `GIT_CONFIG_VALUE_<n>`, then those in `GIT_CONFIG_PARAMETERS` (where
   `git -c` passes its settings to hooks and aliases).

When none sets it, the file is `$XDG_CONFIG_HOME/git/ignore`, or
`~/.config/git/ignore` when `XDG_CONFIG_HOME` is unset or empty. As in git, a
relative path is relative to the top of the work tree (also for
`GIT_CONFIG_SYSTEM` and `GIT_CONFIG_GLOBAL`), an empty value names no file, a
missing file excludes nothing, and a `:(optional)` path that is missing falls
back to the default. `~/` is the home directory, `~user/` that user's home
directory, and `%(prefix)/` git's installation prefix, taken as the parent of
the `bin` directory that holds the `git` found on `PATH` once symbolic links
are resolved; a `git` that is not in a `bin` directory, or none on `PATH`,
makes a `%(prefix)/` path an error.

`[include]` paths are followed, and `[includeIf]` paths when their condition
holds: `gitdir:` and `gitdir/i:` match the repository's git directory (for a
linked worktree, its directory under `.git/worktrees`) with git's rules for
`~/`, `./`, a leading `**/` and a trailing `/`; `onbranch:` matches the branch
`HEAD` names, following a branch that is itself a symbolic reference to
another branch as git does, into the main worktree (`main-worktree/HEAD`) or
another one (`worktrees/<id>/HEAD`) too; and `hasconfig:remote.*.url:` matches the remote URLs of every
configuration file above, refusing, as git does, a remote URL set in a file an
`includeIf` includes. Other conditions never hold.

A git config file git itself would reject, a setting git cannot expand, and an
`onbranch:` condition in a repository that keeps its references in a reftable
stop the walk with an error naming the setting, rather than guessing.

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
Nix files also honour the nearest `statix.toml`; see [rules.md](rules.md#statix-and-deadnix-compatibility).


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
Justfiles are formatted like `just --fmt` formats them; one whose comment
`just --fmt` would drop, such as a comment after a recipe header, is reported
and left alone. Makefiles are formatted without changing what make reads from
them: spacing around operators and rule colons, trailing whitespace make
ignores and runs of blank lines, while recipes and `define` bodies stay as they
are. Nix files are formatted like `nixfmt` 1.5.0 formats them, at its default
width of 100 columns and indent of 2, with the formatter built in: no `nixfmt`
binary is needed. `.editorconfig` settings and the printer flags do not affect
them, shell inside their strings is left as written, and a file with a syntax
error is refused with its position and never partly rewritten. The other files
that embed shell are skipped when walking a directory and refused when named,
since `fmt` formats only shell scripts, justfiles, Makefiles and Nix files. A file named more
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
    rev: v0.5.27
    hooks:
      - id: shell-lsp-check
      - id: shell-lsp-fmt
```

`shell-lsp-check` lints the staged shell scripts, and `shell-lsp-fmt`
formats them in place, which fails the commit when anything changed. On
Windows pre-commit builds `shell-lsp.exe` and finds it through `PATHEXT`, so
the same hooks work there.
