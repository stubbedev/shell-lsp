# shell-lsp

shell-lsp is a language server and linter for shell scripts: bash, zsh,
POSIX sh, dash and ksh, with the dialect detected per file. It gives any
editor that speaks the Language Server Protocol diagnostics, completion,
hover, navigation, refactorings and formatting for shell, and the same
binary lints and formats scripts from the command line for CI and
pre-commit.

It is one static binary with no runtime dependencies. The linter, the
formatter, the manual-page reader behind hover and completion, and the
`.editorconfig` reader are all built in, so neither shellcheck, shfmt, man
nor groff needs to be installed.

- An error-tolerant parser for every dialect, with incremental document
  sync, that knows the whole zsh and ksh93 grammar.
- Diagnostics from a built-in linter that uses ShellCheck's check codes and
  directives, lints zsh, follows sourced files, and adds checks of its own,
  such as untrusted data reaching `eval`, `sh -c` or `source`
  ([docs/rules.md](docs/rules.md)). Every check is listed with its severity
  in [docs/checks.md](docs/checks.md), and hovering a diagnostic explains it
  with a preview of its fix ([Linting](#linting)).
- Completion and hover for builtins, keywords, functions, variables, paths,
  and the options and subcommands of installed commands, read from their
  manual pages ([Completion and hover](#completion-and-hover)).
- Navigation, references, rename, call hierarchy, symbols, semantic tokens,
  inlay hints, signature help, folding, selection ranges and code lenses,
  across the files a script sources ([Sourced files](#sourced-files)).
- Formatting with the printer shfmt is built on, honouring `.editorconfig`
  ([Configuration](#configuration)).
- Quick fixes, [refactorings](#refactorings) and a fix-all source action.
- Checks for awk, sed and jq programs embedded in scripts
  ([Embedded languages](#embedded-languages)).
- The shell inside Makefile and justfile recipes, Dockerfile `RUN`
  instructions and Nix strings, linted and navigated in place, with fixes written back in their syntax
  ([Shell in other files](#shell-in-other-files)).
- Justfiles and Makefiles as languages of their own: diagnostics, navigation,
  rename, completion, hover, outline and "Run" lenses for recipes, targets
  and variables across `import`, `mod` and `include`, also from the shell of
  a recipe to the just or make variable it reads, justfiles formatted
  like `just --fmt` and Makefiles formatted without changing what make reads,
  and Dockerfiles checked the way BuildKit checks them and with Hadolint's
  rules, pragmas and `.hadolint.yaml`, with stages and build arguments to
  navigate
  ([Justfiles, Makefiles and Dockerfiles natively](#justfiles-makefiles-and-dockerfiles-natively)).
- [Bats](#bats) support: tests in the outline, and "Run" lenses that report
  failures as diagnostics.
- `shell-lsp check` and `shell-lsp fmt` for CI and pre-commit, compatible
  with the `shellcheck` and `shfmt` command lines ([docs/cli.md](docs/cli.md)).

Every LSP method, in both directions, is listed with its status in
[docs/lsp-methods.md](docs/lsp-methods.md).

## Installation

Pick one:

- **Release binaries** for Linux and Windows (amd64 and arm64) and macOS
  (arm64) are attached to every
  [GitHub release](https://github.com/stubbedev/shell-lsp/releases),
  together with a `SHA256SUMS` file. Download the one for your platform,
  check it with `sha256sum --check --ignore-missing SHA256SUMS` (on Windows,
  compare `Get-FileHash <file>` in PowerShell with its line), make it
  executable and put it on `PATH` as `shell-lsp` (`shell-lsp.exe` on
  Windows). [docs/editors.md](docs/editors.md#windows) lists what differs on
  Windows.
- **Go**: `go install github.com/stubbedev/shell-lsp@latest`
- **Nix**: `nix profile install github:stubbedev/shell-lsp`, or try it with
  `nix run github:stubbedev/shell-lsp -- --version`
- **VS Code**: the [Shell LSP extension](editors/vscode)
  (`stubbedev.shell-lsp` on the Visual Studio Marketplace and Open VSX, and
  a `.vsix` on every release) runs the server, exposes its settings, and
  offers to download and verify the release binary when none is installed.

`shell-lsp --version` prints the installed version.

## Usage

```
shell-lsp                        # serve LSP over stdio
shell-lsp check [OPTIONS] PATH   # lint, like shellcheck
shell-lsp fmt [FLAGS] [PATH]     # format, like shfmt
shell-lsp --version
```

### In an editor

The server speaks LSP over stdio and needs no arguments: point your editor
at `shell-lsp`. [docs/editors.md](docs/editors.md) has the setup for Neovim,
Helix, Zed, VS Code and any other client. Settings are described under
[Configuration](#configuration).

### On the command line

`shell-lsp check` takes ShellCheck's options, `.shellcheckrc` files, output
formats (plus SARIF for code scanning) and exit codes, and `shell-lsp fmt`
takes shfmt's flags, so both drop into CI and pre-commit hooks where
`shellcheck` and `shfmt` ran before, and report exactly what the editor
shows:

```sh
shell-lsp check .                        # lint every shell script below .
shell-lsp check -f sarif . > out.sarif   # for GitHub code scanning
shell-lsp fmt -d .                       # show what formatting would change
shell-lsp fmt -w script.sh               # format in place
```

Checking the 2579 scripts of a Nixpkgs corpus takes 0.6 s on 12 cores; one
`shellcheck` process over the same files takes about two minutes. The rule
sets differ, so read that as an order of magnitude, not a like-for-like
benchmark. Flags, formats, CI and pre-commit recipes are in
[docs/cli.md](docs/cli.md).

## Dialects

Each document is linted as one shell. The first of these that applies
decides which:

1. the `dialect` setting;
2. `shell=` in the nearest `.shellcheckrc`;
3. `shell_variant` in `.editorconfig`;
4. a `# shellcheck shell=` directive in the comment header before the first
   command;
5. the shebang, also behind `env`, `busybox` and `nix-shell -i`, and with
   the interpreter's version in its name (`bash5`, `zsh-5.9`);
6. a zsh completion tag (`#compdef`, `#autoload`) on the first line;
7. the file name (`.bashrc`, `.zshrc`, `.profile` and the other rc files) or
   its extension (`.bash`, `.bats`, `.zsh`, `.dash`, `.sh`, `.ksh`);
8. the editor's language identifier, unless it is the generic `shellscript`;
9. a vim modeline (`# vim: ft=zsh`);
10. otherwise bash.

An extension or the bash default is only a guess, and a wrong guess is the
usual cause of findings that make no sense for the shell you use. When
nothing names the shell at all, SC2148 says so, in the editor as in
`shell-lsp check`. The VS Code
extension shows the shell in the status bar with what decided it, and
clicking it records your choice as a `# shellcheck shell=` directive, which
every editor and `shell-lsp check` then follow. Other clients can use two
requests:

| Request | Params | Result |
| --- | --- | --- |
| `shellLsp/documentDialect` | `{textDocument, position?}`; with a position in a justfile, Makefile or Nix file, the shell of the recipe or string there | `{selectable, dialect, source, evidence, guessed, description}`, plus `{directive, dialects}` when `selectable` is `true`: whether a directive can choose the shell (`false` for [shell in other files](#shell-in-other-files), whose host decides it), the shell, what decided it (`default`, `override`, `shellcheckrc`, `editorconfig`, `directive`, `shebang`, `completionTag`, `fileName`, `extension`, `languageId`, `modeline`, or for shell in other files `hostSetting` and `hostDefault`), the text that did, whether that is a guess, a sentence to show, whether a header shell directive exists, and the shells that can be chosen |
| `shellLsp/setDialect` | `{textDocument, dialect}` | The `WorkspaceEdit` that adds or updates the header directive for `dialect`, or removes it when `dialect` is `null`; `null` when nothing changes; an error for shell in other files |

The parser understands the whole grammar of each shell, including zsh's
short and brace forms of loops and `if`, `foreach`, `repeat`,
`{ } always { }`, anonymous functions with arguments and glob qualifiers,
and ksh93's compound variables and types, `namespace` and dotted names. A
ksh script is parsed as ksh93 unless its shebang, shell directive or file
name says mksh.

## Configuration

Settings arrive as `initializationOptions`, as the `shellLsp` section of
`workspace/didChangeConfiguration`, and, when the client supports
`workspace/configuration`, are pulled for the whole workspace and for each
workspace folder; a document uses the settings of the innermost folder that
contains it. Unknown keys and invalid values are rejected with an error naming
the offending field.

```json
{
  "dialect": "bash",
  "lint": {
    "enable": true,
    "exclude": ["SC2034"]
  }
}
```

| Key | Default | Meaning |
| --- | --- | --- |
| `dialect` | detected per file | Force `bash`, `zsh`, `sh`, `dash`, `ksh` or `busybox` for every document |
| `lint.enable` | `true` | Report lint findings alongside syntax errors |
| `lint.profile` | `recommended` | How much to report: `recommended`, `minimal` or `strict` |
| `lint.exclude` | `[]` | Codes to suppress, as `SC2034` or `2034` |
| `lint.optional` | `[]` | Opt-in checks to enable by name, such as `require-variable-braces`, or `all` |
| `format.enable` | `true` | Answer formatting requests |
| `format.indent` | editor setting | Spaces per level; `0` indents with tabs |
| `format.binaryNextLine` | `false` | Put `&&`, `\|\|` and `\|` at the start of continuation lines |
| `format.switchCaseIndent` | `false` | Indent `case` arms inside `case ... esac` |
| `format.spaceRedirects` | `false` | Write `> file` instead of `>file` |
| `format.functionNextLine` | `false` | Put the opening brace of a function on its own line |
| `format.simplify` | `false` | Apply shfmt's simplifications (like `shfmt -s`) |
| `format.minify` | `false` | Minify the script (like `shfmt -mn`) |
| `nix.evaluate` | `false` | Evaluate flakes with `nix` to complete and describe NixOS, home-manager and flake-parts options and nixpkgs attributes |

Formatting runs the same printer shfmt is built on, in-process, so no shfmt
binary is needed. Documents with syntax errors are not formatted, and range
formatting only changes the lines that intersect the range.

Projects formatted with shfmt keep working unchanged: `.editorconfig` files are
read the way shfmt reads them (`indent_style`, `indent_size`, `binary_next_line`,
`switch_case_indent`, `space_redirects`, `keep_padding`, `function_next_line`,
`simplify`, `minify`, `ignore`, and the `[[shell]]`, `[[bash]]` and `[[zsh]]`
sections), and `shell_variant` selects the dialect. A `format.*` setting wins
over `.editorconfig`, which wins over the editor's tab size; an explicit
`dialect` setting wins over `shell_variant`. A malformed `.editorconfig` fails
formatting with an error naming the key, and changes to it apply as soon as the
editor reports them.

## Completion and hover

Besides commands, variables and paths, completion follows the shell's own
vocabulary, filtered by the dialect of the document:

- Builtin flags (`read -`, `declare -`, `kill -`, `mapfile -`...) from a table
  per shell, so zsh offers `read -A` and dash offers only `read -r`.
- Test expressions in `[[ ]]`, `[ ]` and `test`: unary operators at the start,
  binary ones after an operand (`=~` and `<` only inside `[[ ]]`), variable
  names after `-v` and option names after `-o`.
- Option names after `set -o`, `shopt` (the `set -o` names after
  `shopt -o`), `setopt` and `unsetopt`, `emulate` modes, trap conditions and
  signal names after `trap`, and signal names alone after `kill`.
- Expansion operators after `${name` (`:-`, `##`, `//`, `@Q`...), zsh
  parameter flags after `${(`, glob qualifiers after `*(` and `(#q`,
  globbing flags after `(#`, associative array keys inside `${name[`, and
  `printf` conversions in the format.
- `case` patterns: the letters of the matching `getopts` optstring, or the
  values the subject is compared against elsewhere in the file.
- Snippets, for clients that support them: keyword skeletons, and idioms
  such as a `getopts` loop, a cleanup trap, `mktemp` with cleanup, strict
  mode and the script's own directory, written for the document's dialect.

Documentation for external commands comes from the installed manual pages,
which the server locates through `MANPATH` (where it is unset or has an
empty entry, in the `share/man` and `man` directories next to every `PATH`
entry and in `/usr/share/man`, `/usr/local/share/man`, `/usr/local/man` and
`/opt/homebrew/share/man`), decompresses and renders itself; neither `man`
nor groff is involved. Subcommands come from the same pages: after `git`,
`systemctl` or `docker` completion offers the documented subcommands, and
the flags completed and hovered after `git checkout` are those of
git-checkout(1), falling back to the command's own page. Nested subcommands
such as `git remote add`, `kubectl config set-context` or `ip link set`
resolve as deep as the pages document them, global options before a
subcommand are skipped with their values (`git -C dir checkout`,
`kubectl -n ns get`), and commands behind the wrappers `builtin`, `busybox`,
`command`, `doas`, `env`, `exec`, `nice`, `nohup`, `setsid`, `sudo`, `time`,
`timeout`, `watch` and `xargs` complete and hover as if typed directly.

Completing inside a word can replace the rest of it (for clients with
insert-replace support). Hover explains the same vocabulary where it is
used, reading each argument by its position exactly as completion does:
builtin flags (also combined, as in `read -rs`), test, arithmetic,
list and `case` operators, redirections and whether a here-document body is
expanded, signals, option names, expansion operators, and every letter that
`$-` can contain.

### Patterns and expansions

Scripts often use the shell's own patterns and parameter expansions instead
of running sed, grep, awk, basename or tr, and those are the hardest part of
a script to read. Hovering one explains it:

- A glob pattern is taken apart piece by piece, wherever it appears: in
  `${var#…}`, `${var%%…}`, `${var//…/…}`, zsh's `${var:#…}` and subscript
  searches, `[[ … == … ]]`, `case` items and file names. That covers POSIX
  wildcards and bracket expressions, bash and ksh extended globs (`@(…)`,
  `!(…)`, ksh93's `{2,3}(…)`, non-greedy `*-(…)` and `~(i)`, `~(E)` and
  `~(F)` options), and zsh's groups, numeric ranges (`<1-10>`),
  `^`, `~`, `#` and `##`, globbing flags such as `(#i)`, `(#b)` and `(#s)`,
  and glob qualifiers such as `(.om[1,3]N)`. Captured groups are numbered
  as they land in `$match`, and an operator whose shell option the file
  never enables (`extendedglob`, `extglob`, `globstar`) is pointed out.
- The regular expression of `[[ … =~ … ]]` is taken apart the same way,
  following each shell's quoting rules for what reaches the regex engine,
  with its groups numbered as they land in `BASH_REMATCH` or `$match`.
- When the tested value is known, `[[ … == … ]]`, `case` items and `=~`
  say whether they match, and `=~` shows what `BASH_REMATCH` or `$match`
  then holds.
- A parameter expansion shows its result when the values it reads are known
  from literal assignments earlier in the file, naming the lines it took
  them from: `${f:t:r}` with `f=archive.tar.gz` gives `archive.tar`, and
  `${line//(#b)([^= ]##)=([^ ]#)/$match[2]:$match[1]}` swaps every key and
  value. The evaluator is tested against bash and zsh themselves and covers zsh's flags
  (`(U)`, `(s:,:)`, `(j:,:)`, `(o)`, `(u)`, `(M)`...), modifiers (`:h`,
  `:t`, `:r`, `:e`, `:gs/x/y/`...), nested expansions and `$match`, and
  bash's `${x^^}`, `${x@U}`, slices and the `&` of `patsub_replacement`.
  Where a value comes from a command, a loop or a computation, or a result
  would depend on the locale or a shell option the file does not settle, it
  shows nothing rather than a guess.

Pattern operators, bracket expressions, numeric ranges, globbing flags,
glob qualifiers and the parts of a regular expression are also highlighted
as semantic tokens. The linter reports pattern operators whose option is
never turned on, capture variables read without a capturing match, bash's
`&` in a replacement, zsh patterns that zsh rejects
([SL2016](docs/rules.md#sl2016) to [SL2019](docs/rules.md#sl2019)) and
regular expression escapes such as `\d` that `=~` does not understand
([SL2022](docs/rules.md#sl2022)). It suggests the shell's own way when
`basename`, `dirname` (in zsh), `pwd`, `tr`, `cut`, `grep -q`, sed (SC2001)
or, as the optional `useless-use-of-cat` check, `cat` start a process for
what the shell can do itself ([SL2020](docs/rules.md#sl2020),
[SL2021](docs/rules.md#sl2021)), with a fix that rewrites it.

## Sourced files

Files pulled in with `source` or `.` act as imports: their functions and
globals are completed, hovered, highlighted and navigated to as if they were
defined in the document, open editor buffers win over the copy on disk, and
the linter follows them. Paths built from `$0`, `${BASH_SOURCE[0]}`,
`dirname`, `cd ... && pwd` and earlier assignments are resolved, as are
`# shellcheck source=` directives. Renaming or moving a script in the editor
rewrites the `source` paths that point at it, and a rename that changes its
extension from one shell to another (`.sh` to `.bash`) updates its shebang
to match.

## Linting

The linter is built into the server; nothing else needs to be installed. It
uses shellcheck's check codes and honours `# shellcheck disable=...`,
`source=` and `shell=` directives, so existing scripts and annotations carry
over unchanged. It also lints zsh, which shellcheck does not support, by
zsh's own rules: unquoted parameters are neither split nor globbed unless
`SH_WORD_SPLIT` or `GLOB_SUBST` is set, glob qualifiers, parameter and
subscript flags are understood, and the parameters zsh, its line editor,
completion system and modules set or read count as assigned and used. It
follows sourced files without extra flags.

Quick fixes are offered where a check has an unambiguous repair, each titled
by what it does (`Double-quote $1`, `Replace -a with -e`, `Add || return`),
alongside actions that insert a `disable` directive for the command or the
whole file, for `SC` and `SL` codes alike. A fix is either safe, meaning it
keeps the script's behaviour (adding `-n`, bracing `$var`, quoting a value
known to hold no spaces or globs), or a suggestion that changes behaviour the
way the finding asks for (quoting `$1`, adding `|| exit`). The first fix of a
finding is the preferred one; the fix-all source action and the
`shellLsp.fixAll` command apply only safe fixes, so running them on save never
changes what a script does.

On a diagnostic, the actions come in this order: its fixes; `Fix all SC2086
in this file (3)`, which applies the preferred fix of every finding of that
code and is titled `(3, review)` when any of those fixes changes behaviour;
`Ignore SC2086 for this command`; `Ignore SC2086 in this file`, which extends
a `disable` directive in the file's leading `# shellcheck` comments or adds
one after the shebang and those comments; and `Ignore SC2086 in this
workspace`, which adds the code to the `disable=` line of the nearest
`.shellcheckrc` in the workspace folder. When there is none, it creates one at
the folder root, carrying over the settings of a `.shellcheckrc` that applied
from outside the folder, and only for clients that can create files. Edits
to `.shellcheckrc` ask the editor for confirmation where it supports change
annotations. An ignore action is not offered for a code that is already
disabled there.

Hovering a diagnostic explains it in place: its one-line summary and
severity, whether it is optional, a before-and-after preview of its preferred
fix marked safe or for review, an example for `SL` checks, the `disable`
directive that silences it, and links to [docs/checks.md](docs/checks.md) or
[docs/rules.md](docs/rules.md) and the ShellCheck wiki. Clients that support
it also get the documentation link on the diagnostic's code.

### Profiles

`lint.profile` (or `--profile` for `shell-lsp check`) sets how much is
reported:

- `recommended`, the default, runs every check that is on by default.
- `minimal` keeps errors, warnings, and the informational checks that point at
  a real bug: the script can print the wrong thing, lose data, run the wrong
  command or fail on input it can plausibly meet, such as an unquoted `$var`
  (`SC2086`) or `A && B || C` used as if-then-else (`SC2015`). Style notes,
  portability notes below warning level, deprecations and redundancies are
  left out. [docs/checks.md](docs/checks.md) marks every check `minimal` keeps.
- `strict` also turns on every optional check except
  `check-unassigned-uppercase`, which reports every all-uppercase variable a
  script reads without assigning, so it flags ordinary environment inputs
  such as `$HOME` or `$CI` rather than mistakes, and
  `avoid-global-reassignment` and the `google-` checks, which follow one
  style guide's preferences rather than point at mistakes.

The profile is the starting point, and the other settings apply on top:
optional checks named in `lint.optional` or turned on with a `# shellcheck
enable=` directive always run and are reported in every profile, codes in
`lint.exclude` are never reported, and `disable=` directives silence findings
as usual. `.shellcheckrc` files have no profile key, so they stay compatible
with ShellCheck.

Checks that go beyond ShellCheck use `SL` codes and are documented in
[docs/rules.md](docs/rules.md), among them untrusted data pasted into code
run by `eval`, `sh -c` or `source`, downloaded code run unchecked, system
directories, home directories and SSH keys made writable by everyone, functions
a script never calls, temporary files it never removes, `trap` and `exit`
that only affect a subshell, syntax errors in embedded awk, sed and jq, and
optional rules from the Google Shell Style Guide.
They are configured exactly like the `SC` checks.

`# shellcheck` directives are edited like code:

- Completion offers the keys after `# shellcheck `, every `SC` and `SL` code
  with its severity after `disable=` (also after a comma, after the dash of a
  range and for a bare number such as `2086`, never a code already listed),
  optional check names after `enable=`, shell names after `shell=`, and paths
  after `source=` and `source-path=`. Typing `=` or `,` opens it, since
  editors do not suggest inside comments on their own.
- Hovering a code shows its one-line summary, severity, whether it is
  optional and how to turn it on, a link to its documentation, and which
  findings it silences in the file; keys and optional check names explain
  themselves too.
- The optional `unused-directives` check (`SL2009`) reports disabled codes
  that silence nothing, with a quick fix that removes them.
- The path in `source=` opens with go-to-definition and is a document link,
  and directive keys and values are highlighted.

The nearest `.shellcheckrc` or `shellcheckrc` above a script, or the one in
`$XDG_CONFIG_HOME` or the home directory, applies in the editor as it does to
`shell-lsp check`: its `disable`, `enable` and `extended-analysis` settings
add to `lint.exclude` and `lint.optional`, and its `shell` chooses the
dialect unless the `dialect` setting does. Its `source-path` setting is only
read by `shell-lsp check`. Completion and hover inside a
`.shellcheckrc` file are not available: editors do not send that file to a
shell language server.

## Refactorings

Code actions offered at the cursor or selection, each only where it applies
and keeps the script's meaning; their edits are computed on
`codeAction/resolve` when the client supports it, and an action that edits
another file as well carries a change annotation asking the client to
confirm it, so editors that support annotations show a preview first.

| Action | Kind | What it does |
| --- | --- | --- |
| Extract to variable | `refactor.extract` | Moves the selected word or part of a word into a variable |
| Extract to function | `refactor.extract` | Moves the selected statements into a new function |
| Toggle quotes | `refactor.rewrite` | Converts between single and double quotes |
| Remove the quotes | `refactor.rewrite` | Unquotes a quoted literal made only of letters, digits and `_ . / : @ % + -`, which the shell reads the same either way; not offered for a command name, a here-document delimiter or inside arithmetic |
| Declare as local | `refactor.rewrite` | Adds `local` to an assignment inside a function |
| Convert backticks | `refactor.rewrite` | Rewrites `` `cmd` `` as `$(cmd)`, undoing the backquote escaping |
| Convert to `[[ ]]` | `refactor.rewrite` | Rewrites `[ ... ]` or `test ...` in bash, zsh and ksh, turning `-a`, `-o`, `\(`, `\)`, `\<` and `\>` into `&&`, `\|\|`, `(`, `)`, `<` and `>`; skipped when globs or unquoted expansions would be read differently |
| Inline variable | `refactor.inline` | Replaces the reads of a variable assigned once to a literal with the value, quoted for each place it is read, and removes the assignment; only when nothing else writes, exports, unsets or could read the variable before the assignment or from another scope. A word spelling the name counts as harmless only as an argument of `echo`, `cd`, `pwd`, `type`, `true`, `false`, `:` or (in bash and sh) an external command, or as a redirection target; `eval`, `trap` and `alias` block it unless all their arguments are literals that never mention the name; and `source`/`.` blocks it only when a sourced file, followed transitively, cannot be resolved or touches the variable outside its own `local`s |
| Toggle the function keyword | `refactor.rewrite` | Switches between `function f { ... }` and `f() { ... }` when the cursor or selection is on the keyword, the name or its `()`; the keyword form is only offered in bash and zsh, and ksh is left alone because there the two forms scope variables differently |
| Add strict mode | `source.strictMode` | Adds whatever part of `set -euo pipefail` (`set -eu` in sh, dash and busybox) a script does not already enable, counting shebang flags, `set -o` names and zsh's `setopt err_exit`, `no_unset` and `pipe_fail` in any case or underscore spelling; it extends the first top-level `set` or `setopt` line that enables some of them, or else inserts a `set` line after the shebang and leading comments |
| Add shebang | `source.shebang` | Inserts the interpreter line for the detected shell (`#!/usr/bin/env bash`, `#!/bin/sh`, ...) into a script that has none; not offered for Bats files or zsh completion functions, whose first line must stay `#compdef` or `#autoload` |
| Convert to case | `refactor.rewrite` | Rewrites an `if`/`elif` chain whose every condition compares the same expansion for equality (`[ "$x" = lit ]`, `test`, `[[ $x == pattern ]]`, joined with `\|\|` or `-o`) into `case`, keeping each body and the `else` branch; one-line chains and `then`, `else` or `fi` sharing a line with commands are split onto their own lines, and the arms follow the document's tab or space indentation (falling back to the configured `indent`) and the `switchCaseIndent` / `switch_case_indent` setting |
| Move function to a sourced file | `refactor.move` | Moves a top-level function, with the comment block directly above it, to the end of a workspace file this script sources before defining the function, as one edit across both files; not offered when the target file is written for another shell that would not parse the function or that the portability checks (`SC3xxx`) flag it for, such as bash syntax moving into an sh, dash or busybox file |

## Embedded languages

awk, sed and jq programs passed as literal arguments (`awk '...'`, `sed -e
'...'`, `jq -r '...'`) or fed as a quoted here-document to `awk -f -` and
`sed -f -` are parsed by built-in parsers; no awk, sed or jq binary is run.
The command is also found behind the same wrappers completion looks
through and as the command of
`find ... -exec`, `-execdir`, `-ok` or `-okdir`; a program containing the
`{}` or `xargs -I` placeholder those substitute at run time is skipped, and
`xargs` does not hand its command the statement's here-document. Syntax
errors, and definite mistakes in their regular expressions (sed addresses and
`s`, awk `/re/` and `~ "re"`, the patterns of jq's `test`, `match`, `sub` and
friends), are reported as lint findings at their exact position inside the
shell string with the native codes [`SL3001`](docs/rules.md#sl3001) (awk),
[`SL3002`](docs/rules.md#sl3002) (sed) and [`SL3003`](docs/rules.md#sl3003)
(jq), up to ten per program, so `lint.enable`, `lint.exclude` and
`# shellcheck disable=` apply to them like to any other check. The programs
are highlighted through semantic tokens, and hovering an awk or jq builtin, or
a sed command or flag, shows its documentation. Programs that contain shell
expansions, or come from files, are skipped rather than guessed at, and
common GNU and gawk extensions are accepted. The one exception is a value no one
can know that the file around the shell puts there, such as a Nix `${x}` or a
just `{{x}}`, and a shell's `$$`: it stands for one word of the program, and a
finding next to it is dropped.

A variable bound on the command line (`jq --arg name "$value"`, `--argjson`,
`--slurpfile`, `--rawfile`, `awk -v name="$value"`, `--assign`) is known in the
program: hovering `$name` in jq or `name` in awk says which flag sets it and to
what. In a file with a host language this goes on through the chain, see
[Nix files](#nix-files). A program variable that the program also declares (`as
$name`, a function parameter) is left alone.

## Shell in other files

Some files that are not shell scripts hold shell that something else runs.
shell-lsp lints and navigates that shell where it stands: it extracts it into
a script with a map back to the file, analyses the script like any other, and
answers in positions of the file. Every edit, from a quick fix to a rename,
is written back in the syntax of the file; a fix that cannot be written there,
such as one that would need a new line in a Makefile recipe, is not offered.
The shell cannot be switched with a `# shellcheck shell=` directive, because
the file decides it. A `# shellcheck disable=` directive in the shell works as
usual. Nix files, justfiles, Makefiles and Dockerfiles are also understood as
languages of their own
([Nix files](#nix-files),
[Justfiles, Makefiles and Dockerfiles natively](#justfiles-makefiles-and-dockerfiles-natively)).

An editor sends these files when the server is attached to their file type
([docs/editors.md](docs/editors.md)); `shell-lsp check` and the workspace scan
find them by name.

### Makefiles

A document is a Makefile when it is named `Makefile`, `makefile` or
`GNUmakefile` or ends in `.mk` or `.mak`, whatever language identifier the
editor gives it, or else when its language identifier is `makefile` or
`make`.

- The recipes are the shell: the lines that start with the recipe prefix
  (a tab, or `.RECIPEPREFIX`) after a rule, and the recipe after `;` on the
  rule line, with their backslash continuations. Make syntax around them
  (variables, conditionals, `define` bodies, comments) is not shell and is
  skipped.
- The `@`, `-` and `+` prefixes are dropped, `$$` is the shell's `$`, and
  every make expansion (`$(CC)`, `${FLAGS}`, `$@`, `$(call f,x)`) stands for
  text the shell sees but no one can know in advance, so it is neither a
  constant nor split into words. Fixes write a `$` back as `$$`.
- Each recipe line runs in a shell of its own, as make runs it, so a variable
  set or a `cd` on one line does not reach the next (`SC2030`, `SC2031`), and
  a syntax error stays on its line. With `.ONESHELL`, each recipe is one
  script.
- The shell is the one `SHELL` names, also behind `env`; without `SHELL` it
  is `sh`. The options in `.SHELLFLAGS` (`-e`, `-u`, `-o pipefail`), or the
  `-e` a `.POSIX` target implies, are set in every recipe. A target-specific
  `SHELL` (`test: SHELL := /bin/bash`) decides the shell of that target's
  recipe, which is checked as its own shell. When `SHELL` names something
  that is not a shell, or is computed (`$(shell ...)`, `!=`), the recipes it
  runs are not analysed.
- A directive goes on a recipe line of its own, indented like the recipe
  (`# shellcheck disable=SC2086`), and applies to the next line.
- A make expansion inside single quotes (`'$(VAR)'`) is still text the shell
  cannot know, not a literal `$`.

### justfiles

A document is a justfile when it is named `justfile`, `Justfile`, `JUSTFILE`
or `.justfile` or ends in `.just`, whatever language identifier the editor
gives it, or else when its language identifier is `just`.

- The recipe bodies are the shell. The settings, variables, aliases,
  attributes and recipe headers around them are not.
- A recipe without a shebang runs each line in a shell of its own, as just
  runs it, after dropping the `@` and `-` sigils and joining lines that end in
  `\`. A recipe that starts with a shebang, or has the `[script]` attribute,
  is one script.
- `{{expression}}` stands for text no one can know in advance, also inside
  quotes, and `{{{{` is a literal `{{`. Fixes write `{{` back as `{{{{`, and
  only a script recipe takes a fix that needs a new line, indented like the
  recipe.
- Exported variables (`export name := ...`, `set export`) and `$`
  parameters are set when a recipe runs, so reading them is not reading an
  unset variable.
- A recipe's shell is the one its shebang names, else the one its
  `[script(...)]` attribute or `set script-interpreter` names for a script
  recipe, else the one `set shell` names, else `sh`. Recipes run by something
  that is not a shell, and `[windows]` recipes when `set windows-shell` or
  `set windows-powershell` is given, are not analysed. Each recipe is parsed
  and checked as its own shell, so a bashism in a plain recipe is reported
  even when another recipe is bash, and hover, completion and the dialect
  status follow the recipe under the cursor.

### Dockerfiles

A document is a Dockerfile when it is named `Dockerfile` or `Containerfile`,
starts with `Dockerfile.` or `Containerfile.` or ends in `.Dockerfile`,
`.dockerfile` or `.Containerfile`, whatever language identifier the editor
gives it, or else when its language identifier is `dockerfile`. A file ending
in `.dockerignore`, such as `Dockerfile.dockerignore`, is the ignore file of a
Dockerfile and not one itself.

- The shell form of `RUN`, and the heredocs a `RUN` feeds to its shell, are
  the shell; so are the shell forms of `CMD`, `ENTRYPOINT`, a
  `HEALTHCHECK CMD` and the instructions `ONBUILD` triggers. JSON forms (`RUN ["make"]`) run no shell and are skipped,
  as is a heredoc whose first line is a shebang of something that is not a
  shell.
- Each instruction runs in a shell of its own, as BuildKit runs it, with the
  continuation lines joined and the comments between them dropped.
- The shell is the one the last `SHELL` or `# hadolint shell=` pragma before
  the instruction names, in the stage or the stages it is built `FROM`, else
  `sh`; a heredoc with a shebang runs in that one. Each shell is checked as its
  own, and a `RUN` in a shell that is not one, such as PowerShell, is left alone.
- The `ENV` variables of the stage and its base stages, and the `ARG`s it
  declares, are set when the shell runs. As in any script, reading an
  all-uppercase variable that is never set is not reported (`SC2154`), which
  leaves alone what the base image sets, such as `PATH` or `GOPATH`.

### Justfiles, Makefiles and Dockerfiles natively

Beyond the shell in their recipes, shell-lsp reads the just, make and
Dockerfile syntax around it with parsers of its own, so it is the only server
an editor needs for these files.

- **Diagnostics.** Syntax errors, unknown recipes, targets, variables,
  functions, settings and attributes, wrong argument counts, duplicates,
  circular dependencies and recursive variables, unused parameters and
  variables, missing `import`, `mod` and `include` files, and more; every
  check is listed in [rules.md](docs/rules.md#sl5001) with
  `SL5` codes for justfiles, `SL6` codes for Makefiles and `SL7` codes for
  Dockerfiles. Each justfile check was compared with what just 1.58 rejects,
  each Makefile check with GNU Make 4.4, and each Dockerfile check with the
  parser and the build checks of BuildKit 0.34, including the
  `# check=skip=` and `# check=error=true` directives. Missing names are reported only when every file that could define
  them was found.
- **Hadolint's rules for Dockerfiles**, under its `DL` codes: package
  versions and caches, image tags and trusted registries, `USER`, `COPY`,
  pipes without `pipefail`, label schemas and the rest, with `# hadolint
  ignore=` pragmas and the nearest `.hadolint.yaml`, so no Hadolint needs to
  run beside the server. Each rule was compared with Hadolint 2.14, and
  [hadolint.md](docs/hadolint.md) lists what it reads and where it
  deliberately differs.
- **Navigation across files.** Definition, references, document highlight and
  rename for recipes, aliases, variables, parameters, modules, functions and
  targets. A justfile shares its names with the files it imports, a `mod`
  starts a module of its own reached with `module::recipe`, and a Makefile
  shares its names with the files it includes, also through simple variables
  such as `include $(DIR)/rules.mk`, and with what a `$(eval ...)` of fixed
  text defines. Opening an imported or included file
  finds the files that include it in the workspace.
- **Across the language boundary.** In a recipe, `$NAME` of an exported just
  variable or `$` parameter, or of a make variable passed with `export`, leads
  to its definition, shows its hover and is renamed with it; a rename that
  would give the shell a name it cannot read is refused. `just recipe`,
  `{{just_executable()}} recipe`, `make target` and `$(MAKE) target` lines
  are references to the recipe or target they run, unless they name another
  file with `-f`, `-C` or `-d`.
- **Call hierarchy.** From a recipe or target, incoming calls are the recipes
  and targets that depend on it or run it with `just` or `$(MAKE)` in their
  body, and outgoing calls are its dependencies and the recipes and targets
  its body runs.
- **Signature help** for just and make functions, user-defined just
  functions, `(recipe arguments)` dependencies, attributes, and `just recipe
  arguments` lines in recipe bodies, with the argument being typed marked.
- **Inlay hints.** Parameter names on the arguments of functions,
  `(recipe arguments)` dependencies and `just recipe arguments` lines, and
  the value of a variable whose value is a constant, such as `= "1.0"` after
  `{{version}}` or `= gcc` after `$(CC)` when the Makefile sets it.
- **Quick fixes.** An unknown recipe, target, variable, function, module,
  setting or attribute can be changed to the closest known name; an unknown
  recipe dependency or `.PHONY` target can be added as an empty one; a missing
  `import`, `mod` or `include` file can be created; a Makefile recipe line
  indented with spaces can be made to start with a tab; and a standard target
  such as `clean` can be added to `.PHONY`.
- **Completion and hover** for recipe and target names, variables,
  parameters, settings, attributes, built-in functions, constants, automatic
  variables and special targets, with what each one does.
- **Outline and structure.** Document and workspace symbols, folding for
  recipes, rules, conditionals, `define` blocks and comment runs, semantic
  tokens for the just and make syntax around the shell, links on `import`,
  `mod` and `include` paths and on Makefile prerequisites that are files,
  found the way make finds them through `vpath` and `VPATH`, and selection ranges that grow from a name
  through its expression and statement to the recipe, also from inside the
  shell of a recipe.
- **Run lenses.** A recipe or target that runs without arguments gets a
  "▶ Run" lens; it runs `just` or `make` for that file, reports the exit
  status and logs the output. A client that declares the experimental
  capability `shellLsp.promptsForArguments` also gets a "▶ Run…" lens on
  recipes with required parameters, listing them so it can ask for values;
  the VS Code extension does, and also offers every recipe and target that
  runs without arguments as a task.
- **Formatting.** Justfiles are formatted the way `just --fmt` formats them,
  checked against just itself. When `just --fmt` would drop a comment, such as
  one after a recipe header, formatting stops and says which line to move
  instead of losing it. Makefiles get one space around assignment operators
  and after rule colons, single spaces between prerequisites, no trailing
  whitespace where make ignores it, and at most two blank lines in a row.
  Recipes, `define` bodies, continued lines and computed rules stay as they
  are, and every change is checked to leave the database `make -p` prints
  unchanged.

### Nix files

A document is a Nix file when it ends in `.nix`, whatever language
identifier the editor gives it, or else when its language identifier is
`nix`. A string literal there is shell, with no marker needed, when it stands
in one of these places. The table of them was read from the option declarations
of a pinned nixpkgs and home-manager, and an attribute counts wherever it is
written: as one long path, as nested sets, inside `flake.modules.x.y`,
`home-manager.users.x` or any other prefix, and through a function such as
`lib.mapAttrs`.

- **Derivations:** a phase or hook (`buildPhase`, `installPhase`, `preConfigure`,
  `postInstall` and the other `pre`/`post` hooks, `buildCommand`), a `shellHook`,
  the script of `runCommand`, `runCommandLocal`, `runCommandCC`,
  `runCommandNoCC`, `runCommandNoCCLocal` and `runCommandWith`.
- **Writers:** `writeShellScript`, `writeShellScriptBin`, `writeShellApplication`
  (`text`), `writeBash`, `writeBashBin` (also with their options), `writeDash` and
  `writeDashBin` (as `dash`), and `writeScript` and `writeScriptBin`, which run
  what their shebang names.
- **Images and environments:** `runAsRoot` and `extraCommands` of
  `dockerTools.buildImage`, `extraCommands` and `fakeRootCommands` of
  `buildLayeredImage` and `streamLayeredImage`, and `runScript`, `profile`,
  `extraBuildCommands`, `extraInstallCommands` and `extraPreBwrapCmds` of
  `buildFHSEnv`.
- **Units:** `script`, `preStart`, `postStart`, `preStop`, `postStop` and `reload`
  of `systemd.services.*` and `systemd.user.services.*`, also under
  `boot.initrd`. The `Exec*` strings of `serviceConfig` and home-manager's
  `Service`, also in a list, are command lines with their `-`, `+`, `@`, `:` and
  `!` prefixes dropped.
- **NixOS:** `system.activationScripts.*` and `system.userActivationScripts.*`,
  as a string or `.text`, `environment.shellInit`, `loginShellInit`,
  `interactiveShellInit`, `extraInit` and `shellAliases.*`, `programs.bash.*Init`,
  `logout` and `shellAliases.*`, `programs.zsh.*Init` and `shellAliases.*` (as
  zsh), `boot.initrd.pre*Commands` and `post*Commands` (as BusyBox),
  `boot.postBootCommands`, `extraInstallCommands` of the boot loaders,
  `networking.localCommands`, `firewall.extraCommands` and
  `extraStopCommands`, `nat.extraCommands` and `extraStopCommands`,
  `dhcpcd.runHook`, the hooks of `networking.wireguard.interfaces.*` and
  `networking.wg-quick.interfaces.*`, `powerManagement.*Commands` and
  `services.xserver.displayManager.sessionCommands` and `setupCommands`.
- **home-manager:** `home.activation.*`, `home.extraProfileCommands`,
  `home.extraBuilderCommands`, `home.sessionVariablesExtra`,
  `home.shellAliases.*`, `onChange` of the files below, `programs.bash.bashrcExtra`,
  `initExtra`, `profileExtra` and `logoutExtra`, `programs.zsh.initContent`,
  `initExtra`, `initExtraFirst`, `initExtraBeforeCompInit`, `completionInit`,
  `envExtra`, `profileExtra`, `loginExtra`, `logoutExtra`, `shellGlobalAliases.*`
  and `siteFunctions.*` (as zsh), and a value of `programs.git.settings.alias` or
  `programs.git.aliases` that starts with `!`.
- **Named files:** the text of `writeText`, `writeTextDir`, `builtins.toFile`,
  `writeTextFile`, `home.file.*`, `xdg.configFile.*`, `xdg.dataFile.*`,
  `xdg.stateFile.*` and `environment.etc.*`, when its shebang names a shell, or else
  when its file name does (`.sh`, `.bash`, `.zsh`, `.dash`, `.ksh`, a shell's rc
  file). The name is `destination`, then `target`, then the attribute or argument
  that names the file.
- **By hand:** a `/* bash */` (or `sh`, `dash`, `zsh`, `ksh`, `shell`) comment right
  before it.
- **Through a function in the same file:** static following shows it hands the
  string to one of the above: its formal is the `text` of `writeShellApplication`,
  the script argument of a writer or of `runCommand`, a phase of a derivation, a
  `${text}` on a line of its own in a shell string, or the argument after `-c`
  there, also through `lib.escapeShellArg`, through further functions and through
  an alias of a writer. Such a function may also write `writeTextFile` with a
  shell's shebang.

Strings known to be in another language are not read as shell: `writeFish`,
`writeNu`, `writePython3`, `writePyPy2`, `writePyPy3`, `writePerl`, `writeRuby`,
`writeLua`, `writeJS`, `writeRust`, `writeHaskell`, `writeNim`, `writeGuile`,
`writeBabashka`, `writeFSharp`, `writeRscript` and `writeCBin`, `testScript` of a
NixOS test (Python), `programs.fish.*`, `programs.nushell.*` and
`services.udev.extraRules`. A `${name}` of such a writer still hovers with the head
of what it writes, in that language. `security.wrappers` and `makeSetupHook` take
paths, so they hold no string to read, and only the writer behind them counts.

A string in a named file whose name is that of a justfile, Makefile or Dockerfile
gets everything those languages have in a file of their own, as far as it can be
reached from a Nix document: the syntax errors and checks of the language
(`SL5`, `SL6` and `SL7`) at their place in the `.nix` file, the shell in its recipes
and `RUN` lines with all its linting, highlighting and navigation in the shell the
file decides, semantic tokens, and definition, references and hover for its recipes,
targets and variables. `${...}` stands for text no one can know, and a finding that
touches one is dropped. Those names are not renamed. Checks that need the files
around it (`import`, `include`) and that a name is unused are left out, and Hadolint's
`DL` rules, completion and signature help do not reach into such a string.

The binding counts through `lib.optionalString`, `lib.mkIf`, `mkBefore`,
`mkAfter`, `mkForce`, `mkDefault`, `mkOverride`, `mkOrder`, `lib.hm.dag.entryAfter`,
`entryBefore`, `entryBetween`, `entryAnywhere`, `lib.stringAfter`, `noDepEntry`,
`fullDepEntry`, `if ... then ... else` and `or`, the items of `lib.mkMerge`,
`concatStringsSep`, `concatStrings` and `concatLines`, the bodies of `concatMapStrings`,
`concatMapStringsSep`, `map` and `mapAttrsToList`, and the strings joined with `+` in
one binding are one script, so an `if` may open in one and close in another.

An antiquotation in a shell string is read for what it is:

- A string it holds is shell in that place: `${lib.optionalString c "..."}`, `${if c
  then "..." else ""}`, the items of `concatStringsSep`, `concatStrings` and
  `concatLines`, and the bodies of `concatMapStrings`, `concatMapStringsSep`, `map`
  and `mapAttrsToList`. Of an `if` with text in both branches the first is read. Inside
  shell single quotes it stays one word.
- A shell writer in it (`${writeShellScript "x" ''...''}`) is read as a script of its
  own that the outer one runs, as `$(...)`.
- As a command word, `${pkg}/bin/foo` and `${lib.getExe' pkg "foo"}` are the program
  `foo`, and `${lib.getExe pkgs.foo}` is `foo` or the `mainProgram` of well-known
  packages (`gnused` is `sed`, `ripgrep` is `rg`), also behind `sudo`, `env` and the
  other wrappers. Every check, hover and embedded `jq`, `awk` or `sed` program treats
  it as that command. Its name is not edited, since the antiquotation decides it.
- `lib.escapeShellArg`, `toString` and the program getters lead to what they read.
- Anything else stands for text the shell cannot know. Where its value is not
  provably safe, `SL8014` says that it is a quoting risk.

- Indentation is removed as Nix removes it, `'''`, `''$` and `''\x` (or `\"`,
  `\$` and the other escapes of a `"` string) are what Nix makes of them, and
  every other `${...}` stands for text the shell cannot know. Fixes are escaped for
  the string they land in, and new lines get its indentation.
- A phase runs as stdenv runs it: inside a function, with `set -eu -o
  pipefail`, in the same shell as the other phases, and with every attribute
  of the derivation and of its setup hooks in the environment, so reading a
  variable the phase never sets is not reported (`SC2154`). The same goes for
  a `shellHook` and for a script with a non-empty `environment`, `runtimeEnv`
  or wrapper `env`. The scripts of the writers and of systemd units otherwise
  run on their own, and `writeShellApplication` adds its strict mode and
  honours its `excludeShellChecks`.
- Each string is checked as the shell its place decides: phases, the bash writers,
  units and activation scripts are bash, `writeDash` is dash, zsh options are zsh, and
  a file or shebang says for itself. A string read into a shell someone else set up,
  such as an init option or alias, does not report variables it never sets or reads
  (`SC2154`, `SC2034`).

Inside the shell, a variable that the Nix around it puts in the environment
leads to the attribute that sets it, with definition, references, document
highlight and hover showing the binding and where it comes from. These are:

- in a phase or `shellHook`, the attributes of its derivation, also
  `inherit`ed ones (where definition goes on to what the `inherit` reads) and
  those under `env`, but not `meta`, `passthru` or `__`-prefixed ones, so
  `$pname` and `''${version}` reach `pname = ...;` and `version = ...;`. A set
  merged with `//` or an `env` that is a value from another file counts too;
- `$xPath` for a name `x` in `passAsFile`, which leads to `x`, and a name after
  `--set` in `makeWrapperArgs`, whose hover says that it reaches the wrapped
  program and not the phases;
- in a `runCommand` script, the attributes it is given;
- in a systemd unit's scripts, its `environment`, also spelled as one long path;
- in a `writeShellApplication`'s `text`, its `runtimeEnv`;
- in a wrapper's string, the keys of the argument that wrapper passes on as the
  environment, so `env.x` reaches `$x`;
- when nothing nearer sets it, `home.sessionVariables`,
  `environment.variables` and `environment.sessionVariables` of the project's
  modules, of any class.

In a phase, `$out`, `$src`, `$pname`, `$version`, `$name`, `$system`, `$builder`,
`$stdenv`, `$srcs`, `$outputs`, `$prefix`, `$sourceRoot`, `$phases`, `$curPhase`,
`$NIX_BUILD_TOP`, `$NIX_BUILD_CORES`, `$NIX_STORE`, `$NIX_LOG_FD`, `$TMPDIR`,
`$TMP`, `$TEMP`, `$TEMPDIR`, `$HOME`, `$PATH`, `$PWD`, `$SHELL`, `$CONFIG_SHELL`,
`$SOURCE_DATE_EPOCH` and `$TZ` are known to come from the builder or `setup.sh`:
hover says so unless an attribute sets them, and they are never reported.

An attribute that sets a variable is not renamed, from the shell or from itself,
since other Nix code may read it by its name.

The shell and the Nix around it answer for each other:

- `${...}` in a shell string is Nix: every feature of the Nix language answers
  inside it.
- A `$name` in a jq or awk program follows its flag: `jq --arg name "$value" '$name'`
  and `awk -v name="$value" '...'` lead from the variable to the flag, on to the shell
  variable, an assignment `value=${x}` and the Nix binding or attribute behind it, also
  in another file. Definition lists each of them, references hold them all and hover
  shows the chain. Renaming `$name` renames the flag and the reads in that program and
  leaves the rest, which keep their own names; it is refused when the program also reads
  `$ARGS` or when an antiquotation or escape splits the name.- A command word `${pkg}/bin/foo`, `${lib.getExe pkg}` or `${pkg}` is that
  program. On the shell text next to the antiquotation, hover says what it runs,
  and definition goes to the binding of `pkg`.
- A name bound to `writeShellScript`, `writeShellApplication`, the other writers
  or a wrapper is a command where it is interpolated. Definition and references
  reach it, hover shows the first lines of the script, and call hierarchy
  crosses the link. Its caller is the script that holds the string.
- A name bound to `writeText`, `writeTextFile`, `builtins.toFile` or a writer of
  another language is a file whose content is known: hover on `${cfg}` shows the head of
  what it writes, also when that is an expression such as `builtins.toJSON { ... }`.- A command in `text`, a phase or a unit that matches an item of `runtimeInputs`,
  `buildInputs`, `nativeBuildInputs`, `packages` or `path` leads to that item, or
  to the script the item names.
- The phases of one derivation share a shell, so an assignment, `export` or
  function reaches later phases in the order `setup.sh` runs them: `shellHook`,
  then `pre`, the phase and `post` of unpack, patch, configure, build, check,
  install, fixup, installCheck and dist. Definition, references and rename follow
  it. A read before the phase that sets it, or in another derivation, leads
  nowhere and hover says why.
- In a shell string, `nix build .#x` and the other uses of `nix` with a `.#` word
  lead to `x` of the flake that holds the file, looked up directly and under
  `packages`, `legacyPackages`, `apps`, `checks`, `devShells`,
  `nixosConfigurations` and `homeConfigurations`. This assumes that the command
  runs in that flake. `nix-shell -p x` leads to `x` where one of the project's
  overlays sets it; anything else in nixpkgs leads nowhere.
- A here-document inside a shell string is Nix when its delimiter is `NIX`, `nix`
  or `EOF_NIX`, when `# nix` follows it on its line, or when its line names a file
  ending in `.nix`. Its syntax errors are reported and its bindings navigate, but
  are not renamed. A body with `${...}` or an escape in it is left as text, since
  only evaluating it would show the Nix, and so is a Nix expression given to
  `nix eval --expr`.

A string passed to a wrapper defined in another file is proven by following the
project, and a keystroke never does that. Navigation, hover, references and call
hierarchy answer in it as soon as a request reads the project. Its shell is linted
and highlighted once a background pass has proven it: that pass follows the
project from each open Nix file that has such a call, one at a time, after 750 ms
without a change to an open Nix file or after a watched file or a setting changes,
then asks the client to refresh diagnostics and tokens if the proven strings moved.
Until then the string reads as plain Nix, and a diagnostics or tokens request on
its own builds nothing. Between passes a proven string follows edits around it and
inside it, and one whose opening was edited stops being shell until the next pass.
These strings report syntax errors and shell findings for open files only, so
workspace diagnostics of a closed file leave them out. Their findings offer no quick
fixes, and `SC2154` stays off in them, since the environment a wrapper gives its
string is not known.

Outside the shell, the Nix language answers too, within a file and across a project:

- Syntax errors and checks are reported, as they are in every Nix file.
- Definition, references, document highlight, hover and rename work for every
  binding: `let`, `rec` attributes, lambda arguments, formals, `@` aliases and
  `inherit`, also through `${...}` in a string. A cursor on `inherit x` means
  the `x` it reads.
- An attribute of a set is followed where the file itself proves it: through
  `s.a.b`, `s ? a`, `inherit (s) a` and `with s;` when `s` is bound in the file
  to an attribute set written there, and through the `@` alias of a function
  for its formals.
- Across files, the project is the nearest `flake.nix` with everything it
  reaches, plus the files that import the one at hand. Nothing is evaluated;
  what follows is read from the source:
  - `import ./x`, `import ./dir` (its `default.nix`), `callPackage ./x { }`,
    `pkgs.callPackage`, `lib.callPackageWith` and `imports = [ ./x ]`, also with
    the path in a `let` of the file. Definition on the path opens the file,
    `(import ./x).a` reaches `a`, and the keys of a call reach the formals of the
    file it calls.
  - In a flake, `inputs.x`, the `outputs` formal `x`, an `@` alias and the string
    of `follows` are all the declaration of the input `x`, and `self.y` reaches
    the output `y`. Where `flake.lock` locks the input and its source is in the
    Nix store, definition also opens its `flake.nix` or `default.nix`, and reads
    such as `inputs.x.lib.y` and `inputs'.x.packages.y` go on into the files
    there, up to 256 of them; nothing is downloaded, and nothing in the store is
    renamed.
  - `"${inputs.x}/y.nix"`, `"${./dir}/y.nix"` and `./dir + "/y.nix"` are paths,
    `~/x` is one below the home directory, and `import ./x` opens a file `x`
    without an extension before it tries `x/default.nix`.
  - `pkgs` and the `final` and `prev` of an overlay read what the project's
    overlays set: `final: prev:` functions under an attribute named `overlays`,
    such as `flake.overlays.x` and `nixpkgs.overlays`. So `pkgs.x.y` follows
    `lib.mapAttrs`, `lib.fix`, `makeExtensible` and `lib.extend`. Every overlay
    counts for every `pkgs` of the project, since which of them apply is only
    known by evaluating.
  - Modules are found from `mkFlake`, `nixosSystem`, `homeManagerConfiguration`,
    `darwinSystem`, `nixOnDroidConfiguration`, `evalModules`, `imports`,
    `nixosModules`-style outputs, `perSystem`, `withSystem`, `moduleWithSystem`,
    `home-manager.users.<name>`, `home-manager.sharedModules`,
    `flake.modules.<class>.<name>` and `import-tree`, which takes every `.nix`
    file below a directory unless its path below that directory has a segment
    starting with `_`. Modules of one class share one namespace: `options.a`,
    `config.a` and a plain `a` are the same name, so a read of `config.a` leads to
    every declaration and definition of it in every file. `_module.args.a`,
    `specialArgs` and `extraSpecialArgs` make `a` the same name as the formal `a`
    of the modules of that class, whose definition is the binding. The options of
    a `types.submodule` and `submoduleWith`, also under `attrsOf`, `lazyAttrsOf`,
    `listOf` and `nullOr`, and the names an option set gets from `mapAttrs`, are
    names below their option. A file in two classes joins them for the names it
    holds, so a rename reaches both.
  - A set built with `//` holds both sides, the right one first, when both are
    known.

  Nothing else is guessed, so `pkgs.hello` without an overlay that sets it leads
  nowhere, and the namespaces of two flakes never meet.
- The outline holds attribute sets, `let` blocks and functions with their
  parameters, nested as they are written, with the shell's functions inside
  the binding that holds them; workspace symbols list every attribute and
  `let` binding.
- Folding covers attribute sets, lists, `let` blocks, strings, lambdas and runs
  of comments, and selection ranges grow from the shell in a string through the
  Nix around it.

A rename refuses, with the reason, to rename to anything but a plain name, or
when another binding would capture a read of the new name or be shadowed by it.
It also refuses names the evaluator itself reads, such as `__overrides` and the
`body` of a legacy `let`, and an attribute of a non-recursive set unless every
read of that set is proven, since anything else could read it by its name. What
it does:

- `inherit x` stays an attribute named `x`: renaming the binding it reads writes
  `x = new;`, and renaming a binding made by `inherit x` writes `new = x;`, also
  taking it out of an `inherit` with several names.
- A formal is part of the function's interface. Renaming one also renames its
  reads through the `@` alias and the keys passed at calls of that function, in
  this file and in the files that reach it by `import` or `callPackage`. It is
  refused for a formal of a `callPackage` target that some caller leaves for
  `callPackage` to fill by name, and for a formal of a module or of `outputs`,
  which are passed by name, unless it is an input or a `_module.args` name the
  project binds itself.
- A `rec` attribute is renamed with its reads in the project.
- An attribute of the value of a file is renamed with its reads when every
  `import` of the file selects from it directly or through a `let` that is only
  selected from.
- An option is renamed everywhere in its class when the project declares it, or
  a set above it, in `options`, and no read passes a set above it on as a value or
  selects from one with a computed name. Options declared elsewhere, such as
  `services.nginx.enable` and `flake`, keep their names.
- An input is renamed with its declaration, formals, reads and `follows`, unless
  `inputs` is read with a computed name. `flake.lock` follows on the next
  `nix flake lock`.
- A project of more than 8192 files is not followed past that, and a workspace
  scan stops at 20000 files. Following also stops at 32 levels of a set that
  holds itself, at 32768 attribute paths of modules, after 2^24 steps and after 8
  rounds that still find something new. In each of these cases a rename that has
  to know every reader is refused with the reason.

Diagnostics stay within the file, so typing never walks the project; it is read
when a request needs another file, and only files that changed are parsed again.

Beyond that, Nix files get these:

- Inlay hints name the parameter of a positional argument and the file a directory
  path opens, links open paths and web addresses, and call hierarchy follows
  functions across files.
- Completion, hover and signature help cover the builtins, keywords, bindings,
  attributes and paths the file can see.
- A "▶ Run" lens on a flake output runs `nix build`, `nix run`, `nix develop` or
  `nix flake check`.
- The refactorings are extract to a `let` binding, inline a `let` binding, spell
  `x = x;` as `inherit x;`, and nest or flatten an attribute path.
- Each finding offers "Ignore here" and "Ignore in this file" fixes, a malformed
  `disable` list reports `SC1125`, and with `enable=unused-directives` a directive
  that suppresses nothing reports `SL2009`.

Shell that Nix assembles across `${...}`, such as a `case` whose patterns come
from `lib.optionalString`, cannot be checked, since only evaluating the Nix
code would show it, and may be reported as a syntax error.

## Commands

`workspace/executeCommand` accepts:

| Command | Arguments | Effect |
| --- | --- | --- |
| `shellLsp.fixAll` | document URI | Applies every non-overlapping safe lint fix in the document via `workspace/applyEdit` |
| `shellLsp.applyEdit` | `WorkspaceEdit` | Applies the edit via `workspace/applyEdit`; code actions use it for clients without code action literal support |
| `shellLsp.showReferences` | `{uri, position}` | Returns the reference locations a reference-count code lens stands for |
| `shellLsp.runBats` | `{uri, test?}` | Runs the Bats file, or only the named test, and reports the results; an error for a document that is not a Bats file |
| `shellLsp.runRecipe` | `{uri, name, arguments?}` | Runs the justfile recipe or Makefile target with the given arguments, reports the exit status and returns `{exitCode, output, truncated}`; an error when the arguments do not fit its parameters |
| `shellLsp.listRecipes` | none | Lists every recipe and target in the workspace as `{uri, path, name, tool, argv, cwd, parameters}`, where `argv` runs it |

## Bats

In files with a `bats` shebang, every `@test` appears in the outline and in
workspace symbols, and `setup`, `teardown`, `setup_file` and `teardown_file`
are marked as hooks. Code lenses offer "Run file" at the top of the file and
"Run test" above each test. A test whose name contains an expansion gets
"Run matching tests" instead: the literal parts of its name, with anything
in between, select it (and any sibling that happens to match).

Running uses the project's own bats: `test/bats/bin/bats`,
`node_modules/.bin/bats` or `test/libs/bats/bin/bats` under the workspace
folder, then `bats` on `PATH`. It runs with `--tap` (and an anchored
`--filter` for a single test) from the workspace folder, reports progress per
test, shows a passed/failed/skipped summary, and turns each failure into an
error diagnostic with source `bats` on the failing line, or on the test when
the output names no line in the file. Those diagnostics stay until the file
or that test runs again, and they move with their test (or helper function)
as the buffer is edited, even when bats ran an older saved copy. A run is stopped when cancelled and after 30
minutes, and stopping it ends every process the suite started, not only
bats itself.

## Development

Every dev tool (Go toolchain pinned to the version go.mod declares, gopls,
golangci-lint, delve, just, shellcheck, shfmt, git, gh, Node.js for the
VS Code extension, bats, bash-language-server and Neovim for the Bats,
`crosscheck` and `smoke` tests, and dash, zsh, ksh93u+m, mksh and busybox,
the real shells the parser conformance tests compare with) comes from the
flake: `direnv allow` on entry, or `nix develop` explicitly. Outside it, the
tests that compare with a reference tool skip.

| Recipe | What it does |
| --- | --- |
| `just check` | Every release gate in order: vet, lint, test, build |
| `just vet` | Static analysis with `go vet`, also over the forked parser |
| `just lint` | `golangci-lint run` against the strict config in `.golangci.yml` |
| `just test` | `go test ./...`, also over the forked parser |
| `just build` | Compile-check every package |
| `just fmt` | Format every Go source in place |
| `just smoke` | Drive the server from headless Neovim |
| `just crosscheck` | Compare with bash-language-server into `docs/crosscheck.md` |
| `just fixtures` | Label new lint fixtures with shellcheck, then check every fixture |
| `just lintcheck PATH...` | Per-code precision and recall of the linter against shellcheck over real scripts |
| `just corpus-fetch` | Fetch the pinned corpus of public shell repositories into the cache |
| `just corpus-check` | Compare the linter with shellcheck, the parser with the real shells and the justfile and Makefile analysis with just over the corpus, against the committed baselines |
| `just corpus-shellcheck` | Only the shellcheck comparison, against `corpus/shellcheck.md` |
| `just corpus-shells` | Only the comparison with bash, dash, zsh, ksh, mksh and BusyBox `-n`, against `corpus/shells.txt` |
| `just corpus-hosts` | Only the analysis of real justfiles and Makefiles, checked with just, against `corpus/hosts.txt` |
| `just corpus-baseline` | Rewrite the corpus baselines after a deliberate change |
| `just fuzz-targets` | List every fuzz target with its package |
| `just fuzz PACKAGE TARGET [TIME]` | Fuzz one target, one minute by default; failing inputs land in the package's `testdata/fuzz` |
| `just bench-compare [BASE] [COUNT]` | Compare every benchmark between `origin/main` (or BASE) and the working tree with benchstat, alternating six runs (or COUNT) of each |
| `just dist` | Build static release binaries for Linux, macOS and Windows into `dist/` |

The corpus checks, the manifest and how to update the baselines are
described in [docs/corpus.md](docs/corpus.md).

With Nix:

- `nix build` builds the package, stamped with the commit date, and runs the
  test suite; update `vendorHash` in `nix/package.nix` whenever go.mod
  dependencies change
- `nix run` runs it
- `nix fmt` formats the nix files with alejandra

Building and publishing the VS Code extension is described in
[docs/vscode-publishing.md](docs/vscode-publishing.md).

## Inspiration

shell-lsp exists because of two projects, and owes its Dockerfile rules to a
third:

- [bash-language-server](https://github.com/bash-lsp/bash-language-server)
  set the baseline for what a shell language server offers: diagnostics,
  completion, hover, definition, symbols and formatting. shell-lsp covers
  that ground for every dialect, not only bash, and goes on to the rest of
  the LSP specification.
- [ShellCheck](https://github.com/koalaman/shellcheck) defined what linting
  a shell script means: its check codes, `# shellcheck` directives,
  `.shellcheckrc` files and command line are what scripts and CI pipelines
  already rely on.
- [Hadolint](https://github.com/hadolint/hadolint) did the same for
  Dockerfiles: its `DL` codes, `# hadolint` pragmas and `.hadolint.yaml` are
  what Dockerfile pipelines rely on.

All three were reimplemented clean-room, without reference to their source code.
Each was treated as a black box whose observable behaviour served as the
specification; [CLEAN_ROOM.md](CLEAN_ROOM.md) states what may and may not be
used:

- ShellCheck is only a test oracle. The lint fixtures under
  `internal/lint/testdata/fixtures` are original scripts whose expected
  findings were recorded by running it (`just fixtures`), with deliberate
  differences marked and explained in place, and `just lintcheck` measures
  the linter's precision and recall against it, code by code, over real
  scripts.
- bash-language-server is compared from the outside: `just crosscheck` runs
  both servers over the same corpus and records their diagnostics and
  completion recall in [docs/crosscheck.md](docs/crosscheck.md).
- Hadolint is only a test oracle too: the Dockerfiles under
  `internal/hostnav/testdata/hadolint` were written for the purpose, their
  expected findings recorded by running it, with deliberate differences
  explained in place ([docs/hadolint.md](docs/hadolint.md)).

What carries over is the interface, so existing scripts, annotations and
configuration keep working: the `SC` check codes and their numbering, the
directive and `.shellcheckrc` syntax, and the options, output formats and
exit codes of the `shellcheck` command line. Every message, summary, fix
and line of the implementation is shell-lsp's own.

Parsing and formatting build on code that is reused, not reimplemented: a
fork of [mvdan.cc/sh](https://github.com/mvdan/sh) v3.14.1, the library
shfmt is built on, lives in [third_party/mvdan-sh](third_party/mvdan-sh)
under its own BSD license. The fork adds the rest of the zsh grammar and a
ksh93 variant; every change is a patch with a reproducer, listed in its
[README](third_party/mvdan-sh/README.md) so it can be sent upstream.

## How it was built

AI agents wrote the code, the tests and the documentation. The author chose
what to build and how far to take it, set the design rules the code has to
follow, and decided what landed. Correctness rests mainly on checks that
anyone can rerun, rather than on line-by-line review of the code:

- **Tests came before the implementation, from the reference tools.** The
  agents that implemented a feature worked against tests derived from what
  ShellCheck, bash-language-server and shfmt observably do, not from their
  source, as described under [Inspiration](#inspiration). The lint fixtures
  record ShellCheck's own findings, and each place where shell-lsp
  deliberately differs is marked in the fixture with its reason. Beyond those
  tools, the scope follows what gopls and rust-analyzer offer, such as
  refactorings and completion that depends on context. Parsing and
  formatting are the exception: they reuse shfmt's library through the fork
  in `third_party/mvdan-sh` rather than reimplementing it.
- **Real shells settle disagreements.** When the documentation, ShellCheck
  and shell-lsp disagree about a shell, the shell itself decides: the flags
  each builtin accepts and the option each `set` letter turns on, for
  instance, were checked by running bash, dash, busybox, zsh, ksh93 and
  mksh.
- **Measurement against the references.** `just lintcheck` reports the
  linter's precision and recall against ShellCheck, code by code, over real
  scripts, and `just crosscheck` records how its diagnostics and completion
  compare with bash-language-server's in
  [docs/crosscheck.md](docs/crosscheck.md). `just corpus-check` holds the
  linter to ShellCheck and the parser to the shells' own `-n` over a pinned
  corpus of public repositories and the Oils spec tests, against committed
  baselines that CI enforces ([docs/corpus.md](docs/corpus.md)).
- **The same gates for every change.** `go vet`, a strict golangci-lint
  configuration, the test suite on Linux, macOS and Windows (with the seed
  corpora of its fuzz tests, whose targets are also fuzzed every night),
  headless Neovim and VS Code integration tests, a check that release
  binaries match the names the VS Code extension downloads, the Nix build,
  the test suite again in the Nix dev shell, where
  the lint fixtures are checked against ShellCheck and the parser against
  bash, dash, mksh, zsh and ksh93, and CodeQL, govulncheck and Grype scans.

Maintenance follows the same pattern. Refactoring, for instance, starts with
agents auditing the code for facts stated in more than one place and
invariants that only comments enforce; other agents then fix each finding as
its own commit, with a regression test for every bug it uncovers, and each
commit has to build and pass the tests on its own before it lands.

## License

MIT
