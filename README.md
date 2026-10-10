# shell-lsp

A Language Server Protocol implementation for shell scripts: bash, zsh, POSIX
sh, dash and ksh, with dialect auto-detection. Aims to cover everything
bash-language-server does, for every dialect, and to implement the full LSP
specification.

The implementation plan lives in the issue tracker. Status: protocol core,
incremental document sync, dialect detection, an error-tolerant parser for every
dialect, diagnostics (syntax errors plus shellcheck, push or pull), hover and completion.

## Goals

- Auto-detected dialects: bash, zsh, sh, dash, ksh (shebang, extension, modeline)
- Parity with bash-language-server: diagnostics, completion, hover, definition,
  symbols, folding, formatting, shellcheck integration
- Full LSP coverage: references, rename, semantic tokens, code actions,
  signature help, inlay hints, workspace symbols, pull diagnostics, and the
  remaining capabilities where they are meaningful for shell
- Single static Go binary; stdio transport; no runtime dependencies

## Usage

```
shell-lsp            # serve LSP over stdio
shell-lsp --version
```

Editor wiring (any LSP client):

```json
{"languages": {"sh": "shell-lsp", "bash": "shell-lsp", "zsh": "shell-lsp"}}
```

## Configuration

Settings arrive as `initializationOptions` and as the `shellLsp` section of
`workspace/didChangeConfiguration`. Unknown keys and invalid values are
rejected with an error naming the offending field.

```json
{
  "dialect": "bash",
  "shellcheck": {
    "enable": true,
    "path": "shellcheck",
    "arguments": ["--severity=style", "--external-sources"],
    "exclude": ["SC2034"]
  }
}
```

| Key | Default | Meaning |
| --- | --- | --- |
| `dialect` | detected per file | Force `bash`, `zsh`, `sh`, `dash` or `ksh` for every document |
| `shellcheck.enable` | `true` | Run shellcheck for diagnostics |
| `shellcheck.path` | `shellcheck` | Binary name or path, resolved through `PATH` |
| `shellcheck.arguments` | `[]` | Extra arguments; `--format`, `--shell`, `--exclude` and `-` are managed by the server |
| `shellcheck.exclude` | `[]` | Codes to suppress, as `SC2034` or `2034` |

shellcheck has no zsh support, so zsh documents get syntax diagnostics only; the
server says so once via `window/logMessage`, as it does when shellcheck is not
installed.

## Recipes

| Recipe | What it does |
| --- | --- |
| `just check` | Every release gate in order: vet, lint, test, build |
| `just vet` | Static analysis with `go vet` |
| `just lint` | `golangci-lint run` against the strict config in `.golangci.yml` |
| `just test` | `go test ./...` |
| `just build` | Compile-check every package |
| `just fmt` | Format every Go source in place |

Every dev tool (Go toolchain pinned to the version go.mod declares, gopls,
golangci-lint, delve, just, shellcheck, shfmt, git, gh) comes from
the flake: `direnv allow` on entry, or `nix develop` explicitly.

## Nix

- `nix build` builds the package; update `vendorHash` in `package.nix` whenever
  go.mod dependencies change
- `nix run` runs it
- `nix fmt` formats the nix files with alejandra

## License

MIT
