# mvdan.cc/sh, forked

This directory holds a fork of [mvdan.cc/sh](https://github.com/mvdan/sh) at
**v3.14.1**, limited to the packages shell-lsp builds against (`syntax`,
`expand`, `pattern`, `fileutil` and `internal`). The repository's `go.mod`
points the module path at it with

    replace mvdan.cc/sh/v3 => ./third_party/mvdan-sh

so every import path stays `mvdan.cc/sh/v3/...`. The upstream `LICENSE` applies
to everything in this directory. Upstream style is kept on purpose, comments
included, so that the changes below can be sent upstream as they are.

## Why

Upstream marks zsh support as experimental and incomplete
([mvdan/sh#120](https://github.com/mvdan/sh/issues/120)), and parses ksh as
mksh ([mvdan/sh#614](https://github.com/mvdan/sh/issues/614)). shell-lsp needs
every valid zsh and ksh93 script to parse into a correct syntax tree instead of
producing syntax errors, so the gaps are closed here first.

## Changes

None yet: this is the pristine v3.14.1 source, minus the `typedjson` package
and the `go:generate` tool dependency, which shell-lsp does not use.

## Tests

The upstream tests are kept and extended. They are a separate module, so run
them with

    go -C third_party/mvdan-sh test ./...

With zsh 5.9, bash 5.3 and mksh R59 on `PATH`, `TestParseConfirm` also checks
every parse test against the real shells.
