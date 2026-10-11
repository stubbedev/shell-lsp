# shell-lsp

A Language Server Protocol implementation for shell scripts: bash, zsh, POSIX
sh, dash and ksh, with dialect auto-detection. Aims to cover everything
bash-language-server does, for every dialect, and to implement the full LSP
specification.

Everything runs inside one static binary with no runtime dependencies: the
linter, the formatter, the manual-page reader behind hover and completion, and
the `.editorconfig` reader are all native. Highlights:

- An error-tolerant parser for every dialect, with incremental document sync,
  that knows the whole zsh and ksh93 grammar ([Parser](#parser)).
- Diagnostics from a native linter that reuses ShellCheck's check codes and
  directives, lints zsh, follows sourced files, and adds checks of its own,
  such as untrusted data reaching `eval`, `sh -c` or `source`
  ([docs/rules.md](docs/rules.md)). Every check is listed with its severity
  in [docs/checks.md](docs/checks.md).
- Completion and hover for builtins, keywords, functions, variables, paths,
  and the options and subcommands of installed commands, read from their
  manual pages ([Completion and hover](#completion-and-hover)).
- Navigation, references, rename, call hierarchy, symbols, semantic tokens,
  inlay hints, signature help, folding, selection ranges and code lenses.
- Formatting with the printer shfmt is built on, honouring `.editorconfig`.
- Quick fixes, refactorings and a fix-all source action.
- Bats support: tests in the outline, and "Run" lenses that report
  failures as diagnostics.
- `shell-lsp check` and `shell-lsp fmt` for CI and pre-commit, compatible
  with the `shellcheck` and `shfmt` command lines ([docs/cli.md](docs/cli.md)).

Every LSP method, in both directions, is listed with its status in
[docs/lsp-methods.md](docs/lsp-methods.md), generated from the table the
conformance tests check the server against.

Files pulled in with `source` or `.` act as imports: their functions and
globals are completed, hovered, highlighted and navigated to as if they were
defined in the document, open editor buffers win over the copy on disk, and
the linter follows them. Paths built from `$0`,
`${BASH_SOURCE[0]}`, `dirname`, `cd ... && pwd` and earlier assignments are
resolved, as are `# shellcheck source=` directives.

## Installation

shell-lsp is one static binary with no runtime dependencies. Pick one:

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

`shell-lsp --version` prints the installed version. Then point your editor at
it: [docs/editors.md](docs/editors.md) covers Neovim, Helix, Zed, VS Code and
any other client.

## Goals

- Auto-detected dialects: bash, zsh, sh, dash, ksh (shebang, zsh completion
  `#compdef` and `#autoload` tags, extension, modeline)
- Parity with bash-language-server: diagnostics, completion, hover, definition,
  symbols, folding, formatting, and linting at least as thorough as shellcheck
- Full LSP coverage: references, rename, semantic tokens, code actions,
  signature help, inlay hints, workspace symbols, pull diagnostics, and the
  remaining capabilities where they are meaningful for shell
- Single static Go binary; stdio transport; no runtime dependencies

## Usage

```
shell-lsp                        # serve LSP over stdio
shell-lsp check [OPTIONS] PATH   # lint, like shellcheck
shell-lsp fmt [FLAGS] [PATH]     # format, like shfmt
shell-lsp --version
```

Editor wiring for Neovim, Helix, Zed and VS Code is in
[docs/editors.md](docs/editors.md).

`shell-lsp check` takes ShellCheck's options, `.shellcheckrc` files, output
formats (plus SARIF for code scanning) and exit codes, and `shell-lsp fmt`
takes shfmt's flags, so both drop into CI and pre-commit hooks where
`shellcheck` and `shfmt` ran before, and report exactly what the editor
shows. Checking the 2579 scripts of a Nixpkgs corpus takes 0.6 s on 12 cores;
one `shellcheck` process over the same files takes about two minutes. The
rule sets differ, so read that as an order of magnitude, not a like-for-like
benchmark. Flags, formats, CI and pre-commit
recipes are in [docs/cli.md](docs/cli.md).

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
binary is needed. Likewise, hover and completion documentation for external
commands comes from the installed manual pages, which the server locates
through `MANPATH` (where it is unset or has an empty entry, in the `share/man`
and `man` directories next to every `PATH` entry and in `/usr/share/man`,
`/usr/local/share/man`, `/usr/local/man` and `/opt/homebrew/share/man`),
decompresses and renders
itself; neither `man` nor groff is involved. Subcommands come from the same
pages: after `git`, `systemctl` or `docker` completion offers the documented
subcommands, and the flags completed and hovered after `git checkout` are those
of git-checkout(1), falling back to the command's own page. Nested subcommands
such as `git remote add`, `kubectl config set-context` or `ip link set` resolve
as deep as the pages document them, global options before a subcommand are
skipped with their values (`git -C dir checkout`, `kubectl -n ns get`), and
commands behind the wrappers `builtin`, `busybox`, `command`, `doas`, `env`,
`exec`, `nice`, `nohup`, `setsid`, `sudo`, `time`, `timeout`, `watch` and
`xargs` complete and hover as if typed directly.
Documents with syntax errors are not formatted, and range
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

Completing inside a word can replace the rest of it (for clients with
insert-replace support). Hover explains the same vocabulary where it is
used: builtin flags (also combined, as in `read -rs`), test, arithmetic,
list and `case` operators, redirections and whether a here-document body is
expanded, signals, option names, expansion operators, and every letter that
`$-` can contain.

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

Completion and hover inside a `.shellcheckrc` file are not available: editors
do not send that file to a shell language server.

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

## Commands

`workspace/executeCommand` accepts:

| Command | Arguments | Effect |
| --- | --- | --- |
| `shellLsp.fixAll` | document URI | Applies every non-overlapping safe lint fix in the document via `workspace/applyEdit` |
| `shellLsp.applyEdit` | `WorkspaceEdit` | Applies the edit via `workspace/applyEdit`; code actions use it for clients without code action literal support |
| `shellLsp.showReferences` | `{uri, position}` | Returns the reference locations a reference-count code lens stands for |
| `shellLsp.runBats` | `{uri, test?}` | Runs the Bats file, or only the named test, and reports the results |

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

## Parser

Scripts are parsed with a fork of [mvdan.cc/sh](https://github.com/mvdan/sh)
v3.14.1 in [third_party/mvdan-sh](third_party/mvdan-sh), built as part of
this module, so the server has no external Go dependencies at all. Upstream treats zsh as experimental and
parses ksh as mksh, so the fork adds the rest of the zsh grammar (short and
brace forms of loops and `if`, `foreach`, `repeat`, `{ } always { }`,
anonymous functions with arguments, case groups, `coproc` without a name,
expansion operators such as `${a::=w}` and `${a:^b}`, math functions and
glob groups and qualifiers) and a ksh93 variant (compound variables and
types, `namespace`, `.sh.math` functions, dotted names, `<<#` and the seek
redirections). A ksh script is parsed as ksh93 unless its shebang, shell
directive or file name says mksh. Every change is a patch with a reproducer
under [third_party/mvdan-sh/patches](third_party/mvdan-sh/patches), listed in
the fork's [README](third_party/mvdan-sh/README.md), so it can be sent
upstream.

## Recipes

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

Every dev tool (Go toolchain pinned to the version go.mod declares, gopls,
golangci-lint, delve, just, shellcheck, shfmt, git, gh, Node.js for the
VS Code extension, and bats, bash-language-server and Neovim for the Bats,
`crosscheck` and `smoke` tests) comes from
the flake: `direnv allow` on entry, or `nix develop` explicitly. shellcheck is
only a test oracle: the lint fixtures under `internal/lint/testdata/fixtures`
are original scripts whose expected findings were recorded from it, with
deliberate differences marked and explained in place.

## Nix

- `nix build` builds the package, stamped with the commit date, and runs the
  test suite; update `vendorHash` in `package.nix` whenever go.mod
  dependencies change
- `nix run` runs it
- `nix fmt` formats the nix files with alejandra

## License

MIT
