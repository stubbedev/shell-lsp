# Corpus checks

The lint fixtures and the parser tests are written by hand, so they only
cover what someone thought of. Two checks run the linter and the parser over
a pinned corpus of real-world scripts instead and compare them with their
references, ShellCheck and the shells themselves. Both record what they find
in a committed baseline, and fail when the result changes.

| Recipe | What it does |
| --- | --- |
| `just corpus-fetch` | Fetch the pinned repositories into the cache |
| `just corpus-shellcheck` | Compare the linter with ShellCheck against [corpus/shellcheck.md](../corpus/shellcheck.md) |
| `just corpus-shells` | Compare the parser with the shells' `-n` against [corpus/shells.txt](../corpus/shells.txt) |
| `just corpus-check` | Both comparisons |
| `just corpus-baseline` | Rewrite both baselines from the current code |

All of them run `cmd/lintcheck` and need the tools from `nix develop`:
ShellCheck 0.11.0, bash, dash, zsh, ksh, mksh and BusyBox at the versions
`flake.lock` pins.

## The corpus

[corpus/repos.txt](../corpus/repos.txt) lists public repositories, one per
line:

    name kind licence url commit [path...]

- `name` names the repository in reports and baselines, so a script appears
  as `nvm/nvm.sh`.
- `kind` is `scripts` for a repository whose shell scripts are compared, or
  `oils-spec` for the spec tests of the [Oils](https://github.com/oils-for-unix/oils)
  project (Apache-2.0), whose test cases are compared one by one.
- `licence` is the repository's SPDX licence, for the record: nothing from
  the corpus is committed, vendored or redistributed.
- `url` is `https://github.com/OWNER/REPO` and `commit` a full commit hash,
  which is all that is fetched.
- The optional paths limit a large repository, such as the kernel or
  Kubernetes, to the directories that hold its scripts.

`lintcheck fetch` shallow-fetches each commit into
`$XDG_CACHE_HOME/shell-lsp/corpus` (`~/.cache/shell-lsp/corpus`, or another
directory given with `-cache`), checks that the checkout is that commit,
drops the git metadata and moves it into place, so an interrupted fetch
leaves nothing half-written. A repository already in the cache is not
fetched again; entries for repositories no longer listed are removed. git
runs without the user's or the system's configuration, hooks, submodules
or Git LFS.

The fetched repositories are untrusted data. Nothing in them is run: they
are only read by ShellCheck, by the shells with `-n`, which reads a script
without running it, and by the parser and linter in-process.

A file is a script when what decides its dialect names one: a shebang, a
`# shellcheck shell=` directive, a zsh completion tag, a file name such as
`.zshrc`, an extension or a modeline, as the language server decides it. A
shebang naming another interpreter, Bats files and files without any such
evidence are left out, and scripts that are not UTF-8 or have CRLF line
endings are counted as skipped.

To add a repository, append its line with the commit to pin, which
`git ls-remote https://github.com/OWNER/REPO HEAD` prints, run
`just corpus-baseline`, classify the new entries of `corpus/shells.txt` and
commit the manifest with both baselines.

## ShellCheck parity

`lintcheck shellcheck` lints every script that ShellCheck supports (bash,
sh, dash, ksh and BusyBox) and runs ShellCheck 0.11.0 on it with the same
shell, in parallel and largest script first. A finding agrees when both
report the same code at the same line and column; findings with codes the
linter does not implement are ignored. [corpus/shellcheck.md](../corpus/shellcheck.md)
records, for every implemented code, how many findings agree, how many only
the linter reports and how many only ShellCheck reports, with the number of
scripts compared and the scripts ShellCheck crashes on.

The check fails when any number changes. A regression, fewer agreements or
more disagreements for a code, prints examples of the current mismatches
for that code; an improvement asks for the baseline to be refreshed, so that
the next regression cannot hide behind it. `just lintcheck PATH...`
(`lintcheck compare`) prints the same comparison with examples for any
directory, which helps when working on one code.

## Parser conformance

`lintcheck shells` parses every script with the dialect detected for it and
runs the shell for that dialect with `-n`:

| Dialect | Shell |
| --- | --- |
| bash | `bash -O extglob -n` (bash 5.3); the parser always accepts extended globs |
| sh, dash | `dash -n` |
| zsh | `zsh -f -n`; only a "parse error" counts, since zsh -n also exits non-zero after `! cmd` and on some checks of constant expressions |
| ksh | `ksh -n`, the ksh 2020.0.0 that nixpkgs packages rather than ksh93u+m |
| mksh | `mksh -n` |
| BusyBox | `busybox sh -n` |

Every case of the Oils spec tests is also checked with each shell its file
lists in `compare_shells` (`ash` is BusyBox), parsed with that shell's
dialect. A difference is either `shell-rejects`, where the shell reports a
syntax error and the parser does not, or `parser-rejects`, the other way
round.

[corpus/shells.txt](../corpus/shells.txt) lists the known differences, one
per line as `category shell verdict script[#case]`, sorted by script. The
check fails on a difference the baseline lacks, which it prints with the
shell's message or the parser's error, on a listed difference that no
longer occurs, on a change in the number of parses, and on any entry still
`unclassified`. `lintcheck shells -explain` prints every difference with
those messages.

Every entry carries one of these categories:

- `posix-extension`: the script is POSIX sh, dash or BusyBox ash (a `.sh`
  file without a shebang is sh, as for ShellCheck) and uses bash, ksh or zsh
  syntax that the shell rejects, such as arrays, `function`, `[[` or names
  with `-`. The parser reads every POSIX dialect with the bash grammar, so the
  rest of the file still gets a syntax tree, and the linter reports each such
  construct as a portability finding (SC3xxx) instead.
- `other-dialect`: the parser reports a construct of another dialect, such as
  bash's `${x@P}` or `${!name}` in zsh, as a dialect error, while the shell's
  `-n` accepts it and fails only if it runs.
- `runtime-check`: the shell parses arithmetic in `$(( ))`, `(( ))`, `let` and
  subscripts, and checks parameter expansion operators, only when it
  evaluates them, so `-n` accepts arithmetic that is invalid or assembled
  from expansions, like `(( $count $op 1 ))`. The parser checks them
  statically.
- `paren-ambiguity`: `((` or `$((` opens nested subshells or a command
  substitution. The shell retries as subshells when arithmetic fails; the
  parser commits to arithmetic, a limit of the upstream parser.
- `stricter-by-design`: the parser rejects on purpose what the shell accepts
  but has no useful meaning, such as an expansion in a here-document
  delimiter or an array assigned before a bash command.
- `parser-gap`: the parser is wrong for that shell, rejecting valid syntax
  (zsh function names built from expansions, `&|`, arithmetic on `#` and
  `?`) or accepting invalid syntax (array declarations in mksh). These are
  known bugs, not decisions; fixing one removes its entries.
- `unclassified`: a new difference that `just corpus-baseline` wrote and no
  one has classified yet. The check fails until each gets one of the
  categories above.

Fixes to the forked parser that this check found are listed in
[third_party/mvdan-sh/README.md](../third_party/mvdan-sh/README.md), patches
0020 to 0026.

## Updating the baselines

Refresh the baselines with `just corpus-baseline` after a change that is
meant to move the numbers: a lint rule, a parser fix, a new repository or
commit in the manifest, or a `flake.lock` update that changes ShellCheck or a
shell. Then give every `unclassified` line in `corpus/shells.txt` its category
by hand, and review the diff of both files like a golden file: each changed
count and each added or removed line should be explained by the change.
