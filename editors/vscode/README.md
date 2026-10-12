# Shell LSP

Language support for shell scripts in VS Code, powered by
[shell-lsp](https://github.com/stubbedev/shell-lsp): a single static language
server for bash, zsh, POSIX sh, dash and ksh with dialect auto-detection.

- Diagnostics from a built-in linter that uses ShellCheck's codes and
  directives, lints zsh, and follows sourced files; no shellcheck needed.
- Completion and hover for builtins, keywords, functions, variables, paths and
  the options and subcommands of installed commands, read from their manual
  pages.
- Go to definition, references, rename, call hierarchy, symbols, semantic
  highlighting, inlay hints, signature help, folding and code lenses.
- Formatting with the printer shfmt is built on, honouring `.editorconfig`;
  no shfmt needed.
- Quick fixes, refactorings and a fix-all action.
- Bats: tests in the outline and "Run" code lenses that report failures as
  diagnostics.
- The shell in Makefile and justfile recipes and in Nix phases, hooks and
  script writers, linted in place, with fixes written back in their syntax.

The server starts for documents VS Code identifies as Shell Script
(`.sh`, `.bash`, `.zsh`, `.ksh`, rc files and shebang detection), as
Makefile, or as `just` or `nix` (which justfile and Nix extensions provide),
and this
extension adds `.bats` files and `bats` shebangs.

## The server binary

The extension runs the `shell-lsp` binary it finds first:

1. the path in `shellLsp.path`, which must be an absolute path to an
   executable;
2. `shell-lsp` on `PATH`;
3. a binary this extension downloaded earlier.

When none is found, a notification offers to download the release binary
matching the extension's version from
[GitHub](https://github.com/stubbedev/shell-lsp/releases). The download is
checked against the release's `SHA256SUMS` before it is saved and made
executable, and it is refused if the checksum does not match. Prebuilt
binaries exist for Linux, macOS and Windows on x64 and arm64.

To install the server yourself instead, use the release binaries,
`nix profile install github:stubbedev/shell-lsp` or
`go install github.com/stubbedev/shell-lsp@latest`.

## Settings

| Setting | Default | Meaning |
| --- | --- | --- |
| `shellLsp.path` | `""` | Absolute path of the `shell-lsp` binary; user settings only |
| `shellLsp.dialect` | detected per file | Force `bash`, `zsh`, `sh`, `dash`, `ksh` or `busybox` for every document |
| `shellLsp.lint.enable` | `true` | Report lint findings alongside syntax errors |
| `shellLsp.lint.profile` | `recommended` | How much to report: `recommended`, `minimal` or `strict` |
| `shellLsp.lint.exclude` | `[]` | Codes to suppress, as `SC2034` or `2034` |
| `shellLsp.lint.optional` | `[]` | Opt-in checks to enable by name, such as `require-variable-braces`, or `all` |
| `shellLsp.format.enable` | `true` | Answer formatting requests |
| `shellLsp.format.indent` | editor setting | Spaces per level; `0` indents with tabs |
| `shellLsp.format.binaryNextLine` | `false` | Put `&&`, `\|\|` and `\|` at the start of continuation lines |
| `shellLsp.format.switchCaseIndent` | `false` | Indent `case` arms inside `case ... esac` |
| `shellLsp.format.spaceRedirects` | `false` | Write `> file` instead of `>file` |
| `shellLsp.format.functionNextLine` | `false` | Put the opening brace of a function on its own line |
| `shellLsp.format.simplify` | `false` | Apply shfmt's simplifications (like `shfmt -s`) |
| `shellLsp.format.minify` | `false` | Minify the script (like `shfmt -mn`) |
| `shellLsp.trace.server` | `off` | Log the messages exchanged with the server to the Shell LSP output |

Every setting except `shellLsp.path` and `shellLsp.trace.server` can differ
per workspace folder. Only the values you set are sent to the server, so a
project's `.editorconfig` (`indent_style`, `binary_next_line`,
`switch_case_indent`, `shell_variant` and the rest of shfmt's keys) decides
whatever you leave at its default, while a `shellLsp.format.*` or
`shellLsp.dialect` value you set wins over it. Changing `shellLsp.path`
restarts the server.

## Commands

- **Shell LSP: Restart Server** stops the server, looks for the binary again
  and starts it.
- **Shell LSP: Show Output** opens the server's log.
- **Shell LSP: Fix All Auto-Fixable Problems** applies every non-overlapping safe
  lint fix in the active script, the ones that keep its behaviour.
- **Shell LSP: Run Bats File** runs the active Bats file and reports failures
  as diagnostics. Running tests executes workspace code, so it is disabled in
  Restricted Mode.
- **Shell LSP: Choose the Shell to Lint As** picks the shell for the active
  script and records it as a `# shellcheck shell=` directive after the
  shebang, or removes that directive. Clicking the shell name in the status
  bar runs it. The status bar shows the shell the script is linted as, marks
  it "(guessed)" when only the extension or the bash default decided it, and
  says in its tooltip what decided it.

## Known limitations

VS Code also files `.fish` and `.csh` scripts under Shell Script; shell-lsp
reports scripts whose shebang names fish or csh as unsupported, and checks
extension-only fish or csh files as bash. Install a fish or csh extension to
give those files their own language.
