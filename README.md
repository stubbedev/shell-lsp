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
- The shell inside Makefile and justfile recipes and Nix strings, linted and
  navigated in place, with fixes written back in their syntax
  ([Shell in other files](#shell-in-other-files)).
- [Bats](#bats) support: tests in the outline, and "Run" lenses that report
  failures as diagnostics.
- `shell-lsp check` and `shell-lsp fmt` for CI and pre-commit, compatible
  with the `shellcheck` and `shfmt` command lines ([docs/cli.md](docs/cli.md)).

Every LSP method, in both directions, is listed with its status in
[docs/lsp-methods.md](docs/lsp-methods.md).

## Installation

Pick one:

- **Release binaries** for Linux, macOS and Windows (amd64 and arm64) are
  attached to every [GitHub release](https://github.com/stubbedev/shell-lsp/releases),
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
2. `shell_variant` in `.editorconfig`;
3. a `# shellcheck shell=` directive in the comment header before the first
   command;
4. the shebang, also behind `env`, `busybox` and `nix-shell -i`;
5. a zsh completion tag (`#compdef`, `#autoload`) on the first line;
6. the file name (`.bashrc`, `.zshrc`, `.profile` and the other rc files) or
   its extension (`.bash`, `.bats`, `.zsh`, `.dash`, `.sh`, `.ksh`);
7. the editor's language identifier, unless it is the generic `shellscript`;
8. a vim modeline (`# vim: ft=zsh`);
9. otherwise bash.

An extension or the bash default is only a guess, and a wrong guess is the
usual cause of findings that make no sense for the shell you use. The VS Code
extension shows the shell in the status bar with what decided it, and
clicking it records your choice as a `# shellcheck shell=` directive, which
every editor and `shell-lsp check` then follow. Other clients can use two
requests:

| Request | Params | Result |
| --- | --- | --- |
| `shellLsp/documentDialect` | `{textDocument}` | `{selectable, dialect, source, evidence, guessed, description}`, plus `{directive, dialects}` when `selectable` is `true`: whether a directive can choose the shell (`false` for [shell in other files](#shell-in-other-files), whose host decides it), the shell, what decided it (`default`, `override`, `editorconfig`, `directive`, `shebang`, `completionTag`, `fileName`, `extension`, `languageId`, `modeline`, or for shell in other files `hostSetting` and `hostDefault`), the text that did, whether that is a guess, a sentence to show, whether a header shell directive exists, and the shells that can be chosen |
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
- Option names after `set -o`, `shopt`, `setopt` and `unsetopt`, `emulate`
  modes, and trap conditions and signal names after `trap` and `kill`.
- Expansion operators after `${name` (`:-`, `##`, `//`, `@Q`...), zsh
  parameter flags after `${(` and glob qualifiers after `*(`, associative
  array keys inside `${name[`, and `printf` conversions in the format.
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
used: builtin flags (also combined, as in `read -rs`), test, arithmetic,
list and `case` operators, redirections and whether a here-document body is
expanded, signals, option names, expansion operators, and every letter that
`$-` can contain.

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
  such as `$HOME` or `$CI` rather than mistakes.

The profile is the starting point, and the other settings apply on top:
optional checks named in `lint.optional` or turned on with a `# shellcheck
enable=` directive always run and are reported in every profile, codes in
`lint.exclude` are never reported, and `disable=` directives silence findings
as usual. `.shellcheckrc` files have no profile key, so they stay compatible
with ShellCheck.

Checks that go beyond ShellCheck use `SL` codes and are documented in
[docs/rules.md](docs/rules.md), among them untrusted data pasted into code
run by `eval`, `sh -c` or `source`, downloaded code run unchecked, functions
a script never calls, temporary files it never removes, `trap` and `exit`
that only affect a subshell, and syntax errors in embedded awk, sed and jq.
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
add to `lint.exclude` and `lint.optional`. Its `shell` and `source-path`
settings are only read by `shell-lsp check`. Completion and hover inside a
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
common GNU and gawk extensions are accepted.

## Shell in other files

Some files that are not shell scripts hold shell that something else runs.
shell-lsp lints and navigates that shell where it stands: it extracts it into
a script with a map back to the file, analyses the script like any other, and
answers in positions of the file. Requests at positions outside the shell
find nothing, and every edit, from a quick fix to a rename, is written back in
the syntax of the file; a fix that cannot be written there, such as one that
would need a new line in a Makefile recipe, is not offered. The file's own
syntax is left alone: these files are not formatted, and their shell cannot be
switched with a `# shellcheck shell=` directive, because the file decides it.
A `# shellcheck disable=` directive in the shell works as usual.

An editor sends these files when the server is attached to their file type
([docs/editors.md](docs/editors.md)); `shell-lsp check` and the workspace scan
find them by name.

### Makefiles

A document is a Makefile when its language identifier is `makefile` or
`make`, or, without one, when it is named `Makefile`, `makefile` or
`GNUmakefile` or ends in `.mk` or `.mak`.

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
  `-e` a `.POSIX` target implies, are set in every recipe. When `SHELL` names something that is not a shell, or is computed
  (`$(shell ...)`, `!=`), the recipes are not analysed.
- A directive goes on a recipe line of its own, indented like the recipe
  (`# shellcheck disable=SC2086`), and applies to the next line.
- A make expansion inside single quotes (`'$(VAR)'`) is still text the shell
  cannot know, not a literal `$`.

### justfiles

A document is a justfile when its language identifier is `just`, or, without
one, when it is named `justfile`, `Justfile`, `JUSTFILE` or `.justfile`, or
ends in `.just`.

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
  `set windows-powershell` is given, are not analysed. The file is checked as
  one shell: the family most recipes use (sh, dash, busybox and bash are one
  family, checked as the widest of them that a recipe uses), and recipes in
  another family are not analysed.

### Nix files

A document is a Nix file when its language identifier is `nix`, or, without
one, when it ends in `.nix`. A string literal there is shell when:

- it is a derivation phase or hook (`buildPhase`, `installPhase`,
  `preConfigure`, `postInstall` and the other `pre`/`post` hooks,
  `buildCommand`), or the script of `runCommand`, `runCommandLocal`,
  `runCommandCC`;
- it is a `shellHook`, or the `script`, `preStart`, `postStart`, `preStop`,
  `postStop` or `reload` of a systemd unit (an attribute under `systemd`);
- it is the script of `writeShellScript`, `writeShellScriptBin`,
  `writeShellApplication` (`text`), `writeBash`, `writeBashBin`, `writeDash`,
  `writeDashBin`, or of `writeScript` and `writeScriptBin` when its shebang
  names a shell;
- a `/* bash */` (or `sh`, `dash`, `zsh`, `ksh`, `shell`) comment comes right
  before it.

The binding counts through `lib.optionalString`, `lib.mkIf`, `mkBefore`,
`mkAfter`, `mkForce`, `mkDefault`, `if ... then ... else` and `or`, and the
strings joined with `+` in one binding are one script, so an `if` may open in
one and close in another.

- Indentation is removed as Nix removes it, `'''`, `''$` and `''\x` (or `\"`,
  `\$` and the other escapes of a `"` string) are what Nix makes of them, and
  every `${...}` stands for text the shell cannot know. Fixes are escaped for
  the string they land in, and new lines get its indentation.
- A phase runs as stdenv runs it: inside a function, with `set -eu -o
  pipefail`, in the same shell as the other phases, and with every attribute
  of the derivation and of its setup hooks in the environment, so reading a
  variable the phase never sets is not reported (`SC2154`). The scripts of
  the writers and of systemd units run on their own, and
  `writeShellApplication` adds its strict mode and honours its
  `excludeShellChecks`.
- The file is checked as one shell, chosen as for justfiles; phases and the
  bash writers are bash.

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
VS Code extension, and bats, bash-language-server and Neovim for the Bats,
`crosscheck` and `smoke` tests) comes from the flake: `direnv allow` on
entry, or `nix develop` explicitly.

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
| `just dist` | Build static release binaries for Linux, macOS and Windows into `dist/` |

With Nix:

- `nix build` builds the package, stamped with the commit date, and runs the
  test suite; update `vendorHash` in `package.nix` whenever go.mod
  dependencies change
- `nix run` runs it
- `nix fmt` formats the nix files with alejandra

Building and publishing the VS Code extension is described in
[docs/vscode-publishing.md](docs/vscode-publishing.md).

## Inspiration

shell-lsp exists because of two projects:

- [bash-language-server](https://github.com/bash-lsp/bash-language-server)
  set the baseline for what a shell language server offers: diagnostics,
  completion, hover, definition, symbols and formatting. shell-lsp covers
  that ground for every dialect, not only bash, and goes on to the rest of
  the LSP specification.
- [ShellCheck](https://github.com/koalaman/shellcheck) defined what linting
  a shell script means: its check codes, `# shellcheck` directives,
  `.shellcheckrc` files and command line are what scripts and CI pipelines
  already rely on.

Both were reimplemented clean-room, without reference to their source code.
Each was treated as a black box whose observable behaviour served as the
specification:

- ShellCheck is only a test oracle. The lint fixtures under
  `internal/lint/testdata/fixtures` are original scripts whose expected
  findings were recorded by running it (`just fixtures`), with deliberate
  differences marked and explained in place, and `just lintcheck` measures
  the linter's precision and recall against it, code by code, over real
  scripts.
- bash-language-server is compared from the outside: `just crosscheck` runs
  both servers over the same corpus and records their diagnostics and
  completion recall in [docs/crosscheck.md](docs/crosscheck.md).

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

## License

MIT
