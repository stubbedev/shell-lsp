# shell-lsp

A Language Server Protocol implementation for shell scripts: bash, zsh, POSIX
sh, dash and ksh, with dialect auto-detection. Aims to cover everything
bash-language-server does, for every dialect, and to implement the full LSP
specification.

The implementation plan lives in the issue tracker. Status: protocol core,
incremental document sync, dialect detection, an error-tolerant parser for every
dialect, diagnostics (syntax errors plus a native linter, push or pull), hover, completion, navigation, symbols, folding, formatting and code actions.

Every LSP method, in both directions, is listed with its status in
[docs/lsp-methods.md](docs/lsp-methods.md), generated from the table the
conformance tests check the server against.

Files pulled in with `source` or `.` act as imports: their functions and
globals are completed, hovered, highlighted and navigated to as if they were
defined in the document, open editor buffers win over the copy on disk, and
the linter follows them. Paths built from `$0`,
`${BASH_SOURCE[0]}`, `dirname`, `cd ... && pwd` and earlier assignments are
resolved, as are `# shellcheck source=` directives.

## Goals

- Auto-detected dialects: bash, zsh, sh, dash, ksh (shebang, extension, modeline)
- Parity with bash-language-server: diagnostics, completion, hover, definition,
  symbols, folding, formatting, and linting at least as thorough as shellcheck
- Full LSP coverage: references, rename, semantic tokens, code actions,
  signature help, inlay hints, workspace symbols, pull diagnostics, and the
  remaining capabilities where they are meaningful for shell
- Single static Go binary; stdio transport; no runtime dependencies

## Usage

```
shell-lsp            # serve LSP over stdio
shell-lsp --version
```

Editor wiring for Neovim, Helix, Zed and VS Code is in
[docs/editors.md](docs/editors.md).

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
| `dialect` | detected per file | Force `bash`, `zsh`, `sh`, `dash` or `ksh` for every document |
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
through `MANPATH` (or next to every `PATH` entry), decompresses and renders
itself; neither `man` nor groff is involved. Documents with syntax errors are not formatted, and range
formatting only changes the lines that intersect the range.

## Linting

The linter is built into the server; nothing else needs to be installed. It
uses shellcheck's check codes and honours `# shellcheck disable=...`,
`source=` and `shell=` directives, so existing scripts and annotations carry
over unchanged. It also lints zsh, which shellcheck does not support, and it
follows sourced files without extra flags. Quick fixes are offered where a
check has an unambiguous repair, alongside actions that insert a `disable`
directive for the command or the whole file.

## Commands

`workspace/executeCommand` accepts:

| Command | Arguments | Effect |
| --- | --- | --- |
| `shellLsp.fixAll` | document URI | Applies every non-overlapping lint autofix in the document via `workspace/applyEdit` |
| `shellLsp.applyEdit` | `WorkspaceEdit` | Applies the edit via `workspace/applyEdit`; code actions use it for clients without code action literal support |

## Recipes

| Recipe | What it does |
| --- | --- |
| `just check` | Every release gate in order: vet, lint, test, build |
| `just vet` | Static analysis with `go vet` |
| `just lint` | `golangci-lint run` against the strict config in `.golangci.yml` |
| `just test` | `go test ./...` |
| `just build` | Compile-check every package |
| `just fmt` | Format every Go source in place |
| `just smoke` | Drive the server from headless Neovim |
| `just crosscheck` | Compare with bash-language-server into `docs/crosscheck.md` |
| `just fixtures` | Label new lint fixtures with shellcheck, then check every fixture |
| `just lintcheck PATH...` | Per-code precision and recall of the linter against shellcheck over real scripts |
| `just dist` | Build static release binaries for Linux and macOS into `dist/` |

Every dev tool (Go toolchain pinned to the version go.mod declares, gopls,
golangci-lint, delve, just, shellcheck, shfmt, git, gh) comes from
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
