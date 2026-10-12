# Corpus checks

The lint fixtures are written by hand, so they only cover what someone
thought of. The corpus check runs the linter over a pinned corpus of
real-world scripts instead and compares it with ShellCheck. It records what
it finds in a committed baseline, and fails when the result changes.

| Recipe | What it does |
| --- | --- |
| `just corpus-fetch` | Fetch the pinned repositories into the cache |
| `just corpus-shellcheck` | Compare the linter with ShellCheck against [corpus/shellcheck.md](../corpus/shellcheck.md) |
| `just corpus-baseline` | Rewrite the baseline from the current code |

All of them run `cmd/lintcheck` and need ShellCheck 0.11.0 from
`nix develop`, at the version `flake.lock` pins.

## The corpus

[corpus/repos.txt](../corpus/repos.txt) lists public repositories, one per
line:

    name kind licence url commit [path...]

- `name` names the repository in reports and baselines, so a script appears
  as `nvm/nvm.sh`.
- `kind` is `scripts`, for a repository whose shell scripts are compared.
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
are only read by ShellCheck and by the parser and linter in-process.

A file is a script when what decides its dialect names one: a shebang, a
`# shellcheck shell=` directive, a zsh completion tag, a file name such as
`.zshrc`, an extension or a modeline, as the language server decides it. A
shebang naming another interpreter, Bats files and files without any such
evidence are left out, and scripts that are not UTF-8 or have CRLF line
endings are counted as skipped.

To add a repository, append its line with the commit to pin, which
`git ls-remote https://github.com/OWNER/REPO HEAD` prints, run
`just corpus-baseline` and commit the manifest with the baseline.

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

## Updating the baseline

Refresh the baseline with `just corpus-baseline` after a change that is
meant to move the numbers: a lint rule, a parser fix, a new repository or
commit in the manifest, or a `flake.lock` update that changes ShellCheck.
Then review the diff like a golden file: each changed count should be
explained by the change.
