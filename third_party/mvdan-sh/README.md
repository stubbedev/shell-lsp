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
| 0007 | zsh: the argument after subscript flags (`${a[(r)$x*]}`) as a word with expansions and quotes, ending at a `,` or `]` outside brackets | [mvdan/sh#120](https://github.com/mvdan/sh/issues/120) |
| 0008 | zsh: every digit of a short positional parameter (`$10` is the tenth argument, not `$1` followed by `0`) | [mvdan/sh#120](https://github.com/mvdan/sh/issues/120) |
| 0009 | expand: arithmetic in `int64` throughout (upstream's `TODO(v4)`), bounded integer conversions for slices, subscripts and `\x`/`\u`/`\U` escapes, and brace sequences that stop at an endpoint or step near the `int64` limits instead of looping forever | — |
| 0010 | syntax: any literal `@` expansion operator parses (`${a@bar}`), as with `bash -n`; bash rejects unknown ones only when it expands a set parameter | — |
| 0011 | syntax: a `#` right after a word part continues the word inside `$(...)`, backquotes and `${ ...;}` too (`$(nix eval "$dir"#attr)`), instead of starting a comment | — |
| 0012 | syntax: a declaration argument whose name is not valid (`export a-b=1`, `declare 0=x`) parses as a word, as with `bash -n`, instead of failing with "invalid var name" | — |
| 0013 | syntax: input nesting more than 1000 levels deep (nested substitutions, expansions, blocks, arithmetic and test expressions, long `&&`, `||` and `|` lists, elif chains) fails with a syntax error, so parsing and walking the tree use bounded stack | — |
| 0014 | pattern: invalid UTF-8 is an error instead of an expression that `regexp.MustCompile` panics on; fuzz targets for `Regexp`, `BracesSeq` and `Format` | — |
| 0015 | syntax: the subscripts of arrays declared associative (`declare -A`, `local -A`, `typeset -A`) parse as string keys (`[e/]=0`, `${m[a.b]}`, `[foo bar]=1`), as with `bash -n` and `zsh -n`, instead of as arithmetic | — |
| 0016 | syntax: with bash, sh and mksh, a `${` that cannot start a parameter or a special parameter followed by a word (`${.sh.file}`, `${(%):-%x}`, `${@name@}`) parses as a literal holding its text, as with `bash -n`, `dash -n` and `mksh -n`, which reject it only when it runs | — |
| 0017 | syntax: the offset of `=` in the current literal is reset for every token, so error recovery after an assignment such as `v=$(( x  y ))` no longer indexes a stale offset and panics | — |
| 0018 | syntax tests: `TestParseConfirm` reports a shell killed by its deadline as a timeout instead of a parse error, allows ten seconds, and runs at most `GOMAXPROCS` shells at once, so slow shell startup (MSYS2 bash on Windows) no longer fails valid inputs | — |

## Tests

The upstream tests are kept and extended, and run with the rest of the module:

    go test ./third_party/mvdan-sh/...

With zsh 5.9, bash 5.3, mksh R59 and ksh93u+m on `PATH`, `TestParseConfirm` also
checks every parse test against the real shells.

After changing the tokens in `syntax/tokens.go`, regenerate `token_string.go`
with `go run golang.org/x/tools/cmd/stringer@v0.52.0 -type token -linecomment
-trimprefix _` (the v0.38.0 pinned upstream fails with Go 1.27) and
`tokens_parse.go` with `go run gen_token_parse.go`, both from `syntax/`.
