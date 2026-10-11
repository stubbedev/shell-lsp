# mvdan.cc/sh, forked

This directory holds a fork of [mvdan.cc/sh](https://github.com/mvdan/sh) at
**v3.14.1**, limited to the packages shell-lsp builds against (`syntax`,
`expand`, `pattern`, `fileutil` and `internal`). It is part of the
`github.com/stubbedev/shell-lsp` module and imported as
`github.com/stubbedev/shell-lsp/third_party/mvdan-sh/...`; a `replace`
directive would keep the upstream import paths, but Go refuses
`go install module@version` for modules that replace a dependency. The
patches below are written against upstream's paths. The upstream `LICENSE` applies
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
| 0005 | zsh: `${a::=w}`, `${a:/p/r}`, `${a:^b}` and `${a:^^b}`; `#name`, `##c`, `[#16]` and math function calls (new `FuncArithm` node) in arithmetic; glob groups and qualifiers that nest or quote parentheses | [mvdan/sh#120](https://github.com/mvdan/sh/issues/120) |
| 0006 | ksh93: a new `LangKsh93` variant with dotted variable names, compound variables and types (`ArrayExpr.Stmts`, nested values, members and further subscripts), arithmetic functions (`FuncDecl.Params`), `namespace` (new `NamespaceClause` node), `<<#` and the replacing and seeking redirections, `${@a}` and `${#prefix@}`, and the features ksh93 shares with mksh or bash | [mvdan/sh#614](https://github.com/mvdan/sh/issues/614) |

## Tests

The upstream tests are kept and extended. They are a separate module, so run
them with

    go -C third_party/mvdan-sh test ./...

With zsh 5.9, bash 5.3, mksh R59 and ksh93u+m on `PATH`, `TestParseConfirm` also
checks every parse test against the real shells.

After changing the tokens in `syntax/tokens.go`, regenerate `token_string.go`
with `go run golang.org/x/tools/cmd/stringer@v0.52.0 -type token -linecomment
-trimprefix _` (the v0.38.0 pinned upstream fails with Go 1.27) and
`tokens_parse.go` with `go run gen_token_parse.go`, both from `syntax/`.
