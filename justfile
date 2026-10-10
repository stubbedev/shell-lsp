# shell-lsp dev tasks.

export CGO_ENABLED := "0"

# List all recipes with their descriptions.
default:
    @just --list

# Run every release gate in order: vet, lint, test, build.
check: vet lint test build

# Static analysis of every package with go vet.
vet:
    go vet ./...

# Lint every package; settings live in .golangci.yml.
lint:
    golangci-lint run

# Run the test suite for every package.
test:
    go test ./...

# Compile-check every package; the output is discarded.
build:
    go build -o /dev/null ./...

# Format every Go source in place with gofmt.
fmt:
    gofmt -w .

# Compare diagnostics and completion recall with bash-language-server; rewrites docs/crosscheck.md.
crosscheck:
    #!/usr/bin/env bash
    set -euo pipefail
    bin="$(mktemp -d)"
    trap 'rm -rf "$bin"' EXIT
    go build -o "$bin/shell-lsp" .
    go run ./cmd/crosscheck -ours "$bin/shell-lsp"

# Drive the server from headless Neovim: diagnostics, hover, definition, completion.
smoke:
    #!/usr/bin/env bash
    set -euo pipefail
    bin="$(mktemp -d)"
    trap 'rm -rf "$bin"' EXIT
    go build -o "$bin/shell-lsp" .
    nvim --headless -l scripts/nvim-smoke.lua "$bin/shell-lsp"
