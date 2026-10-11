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

Besides leaving out the `typedjson` package and the `go:generate` tool
dependency, which shell-lsp does not use, the fork differs from v3.14.1 by the
patches in [patches/](patches). Each one is a `git diff` against the module
root, applies on top of the previous ones starting from pristine v3.14.1
(`git apply patches/*.patch`), and starts with a description and a minimal
reproducer so that it can be sent upstream as it is.

| Patch | Change | Upstream issue |
| --- | --- | --- |
| 0001 | zsh: alternate forms of `for`, `foreach`, `select`, `while`, `until` and `if` (`for f (*.txt) cmd`, `if [[ c ]] { cmd }`, `while (( c )) cmd`, loops over several names), and `repeat` loops as a new `RepeatClause` node | [mvdan/sh#120](https://github.com/mvdan/sh/issues/120) |
| 0002 | zsh: arguments after the body of an anonymous function (`() { ... } arg`), several names before `()`, a separator between `function` names and the body, and `$(() { ... })` lexed as a command substitution | [mvdan/sh#120](https://github.com/mvdan/sh/issues/120) |
| 0003 | zsh: `{ try-list } always { always-list }` as a new `TryClause` node | [mvdan/sh#120](https://github.com/mvdan/sh/issues/120) |
| 0004 | zsh: case patterns starting with a group (`(a|b)*)`) or globbing flags, `case x { ... }`, `coproc` without a name, `}` closing a block right after `local`, `let` or `time`, and `[[ -N file ]]` | [mvdan/sh#120](https://github.com/mvdan/sh/issues/120) |

## Tests

The upstream tests are kept and extended. They are a separate module, so run
them with

    go -C third_party/mvdan-sh test ./...

With zsh 5.9, bash 5.3 and mksh R59 on `PATH`, `TestParseConfirm` also checks
every parse test against the real shells.
