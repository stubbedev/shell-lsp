# shell-lsp

A Language Server Protocol implementation for shell scripts: bash, zsh, POSIX
sh, dash and ksh, with dialect auto-detection. Aims to cover everything
bash-language-server does, for every dialect, and to implement the full LSP
specification.

The implementation plan lives in the issue tracker. Status: scaffold with a
working JSON-RPC 2.0 transport and the initialize/shutdown/exit lifecycle over
stdio.

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
golangci-lint, delve, just, shellcheck, shfmt, tree-sitter, git, gh) comes from
the flake: `direnv allow` on entry, or `nix develop` explicitly.

## Nix

- `nix build` builds the package (`package.nix` scaffold; add a `vendorHash`
  once go.mod grows dependencies)
- `nix run` runs it
- `nix fmt` formats the nix files with alejandra

## License

MIT
