# Corpus checks

The lint fixtures and the parser tests are written by hand, so they only
cover what someone thought of. Three checks run the linter, the parser and
the justfile, Makefile and Dockerfile analysis over a pinned corpus of
real-world files instead and compare them with their references: ShellCheck,
the shells themselves, just, GNU make, BuildKit and Hadolint. Each records what it finds in a committed baseline, and
fails when the result changes.

| Recipe | What it does |
| --- | --- |
| `just corpus-fetch` | Fetch the pinned repositories into the cache |
| `just corpus-shellcheck` | Compare the linter with ShellCheck against [corpus/shellcheck.md](../corpus/shellcheck.md) |
| `just corpus-shells` | Compare the parser with the shells' `-n` against [corpus/shells.txt](../corpus/shells.txt) |
| `just corpus-hosts` | Analyse the justfiles, Makefiles and Dockerfiles against [corpus/hosts.txt](../corpus/hosts.txt), checking them with just, make, BuildKit and Hadolint |
| `just corpus-nix-lint` | Compare the Nix checks with statix and deadnix on a 2,500-file sample of the nixpkgs `flake.lock` pins, against [corpus/nix-lint.md](../corpus/nix-lint.md) |
| `just corpus-check` | All three comparisons |
| `just corpus-baseline` | Rewrite the three baselines from the current code |

All of them run `cmd/lintcheck` and need the tools from `nix develop`:
ShellCheck 0.11.0, bash, dash, zsh, ksh, mksh, BusyBox, just, GNU Make,
bubblewrap, Hadolint 2.14.0 and `dockerfile-oracle`, a small program in
[tools/dockerfile-oracle](../tools/dockerfile-oracle) that runs BuildKit's
Dockerfile parser and build checks offline, at the
versions `flake.lock` pins.

## The corpus

[corpus/repos.txt](../corpus/repos.txt) lists public repositories, one per
line:

    name kind licence url commit [path...]

- `name` names the repository in reports and baselines, so a script appears
  as `nvm/nvm.sh`.
- `kind` is `scripts` for a repository whose shell scripts are compared,
  `oils-spec` for the spec tests of the [Oils](https://github.com/oils-for-unix/oils)
  project (Apache-2.0), whose test cases are compared one by one, or `hosts`
  for a repository whose justfiles, Makefiles and Dockerfiles are analysed.
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
without running it, by `just --dump`, which parses a justfile without running
a recipe or a backtick, by `dockerfile-oracle`, which parses and checks a
Dockerfile without building it or fetching an image, and by the parser and
linter in-process. make is never
run on them, since reading a Makefile already runs its `$(shell ...)` calls
and remakes its included files.

A file is a script when what decides its dialect names one: a shebang, a
`# shellcheck shell=` directive, a zsh completion tag, a file name such as
`.zshrc`, an extension or a modeline, as the language server decides it. A
shebang naming another interpreter, Bats files and files without any such
evidence are left out, and scripts that are not UTF-8 or have CRLF line
endings are counted as skipped.

To add a repository, append its line with the commit to pin, which
`git ls-remote https://github.com/OWNER/REPO HEAD` prints, run
`just corpus-baseline`, classify the new entries of `corpus/shells.txt` and
commit the manifest with the baselines.

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
| ksh | `ksh -n`, ksh93u+m 1.0.10, which the flake builds from `nix/ksh93.nix` because nixpkgs packages only the abandoned ksh 2020 |
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

## Justfiles, Makefiles and Dockerfiles

`lintcheck hosts` analyses every justfile, Makefile and Dockerfile of the `hosts`
repositories, named and with the extensions the language server recognises,
the way the language server does: each file with the files it includes and
the files that include it, found among the files of the same repository.
[corpus/hosts.txt](../corpus/hosts.txt) lists every finding as
`file:line:column code`, sorted by file and position. For a justfile it also
runs `just --dump` and adds a `just disagrees` line when just rejects a file
the analysis reports no error in, or accepts one with an error.

Every Makefile with a standard name is also read by GNU make itself, with
`make -pqr`, which reads and evaluates the whole file without running a
recipe. Reading a Makefile runs its `$(shell ...)` calls, so make runs in a
bubblewrap sandbox: no network, the repository behind a throwaway overlay,
1 GiB of memory and a minute of time. An included file that a build would
generate is stubbed as an empty file in that overlay, so make reads on. A
`make disagrees` line records a file make rejects at a line where the
analysis reports no read-time error (syntax, conditionals, `define`, recipe
prefixes), or accepts while the analysis reports one. Files that stop
themselves with `$(error ...)`, need `./configure` output outside the
repository, or fail in a file they include stay unjudged, and the check
prints how many files make or BuildKit judged.

Every Dockerfile is also parsed and checked by BuildKit. A `BuildKit
disagrees` line records a file BuildKit rejects at a line where the analysis
reports no error, or accepts while the analysis reports one, and every build
check BuildKit reports on an instruction that the analysis does not report
there under its `SL7` code, or the other way round. Hadolint lints each
Dockerfile as well, with the `.hadolint.yaml` the analysis uses, and a
`Hadolint disagrees` line records each `DL` finding on an instruction that
only one of the two reports there. Most of these are the deliberate
differences [hadolint.md](hadolint.md) describes, kept in the baseline so a
change in either shows. A file Hadolint cannot lint gets a `Hadolint fails`
line.

The check fails when any line is added or removed, which it prints as a
diff, and when a file takes longer than two seconds.

## Nix lint comparison

`just corpus-nix-lint` runs statix and deadnix as black boxes beside the Nix checks and compares each lint. It also applies every fix in files up to 128 KiB and fails if one stops parsing or changes what a name resolves to. `lintcheck nixlint -sample 100000` covers all of nixpkgs in about 6 minutes, and `lintcheck nixlint -examples 5 -skip secrets,certs PATH` reports on any tree without a baseline. CI does not run this, because a run spawns two tools per file.

## Updating the baselines

Refresh the baselines with `just corpus-baseline` after a change that is
meant to move the numbers: a lint rule, a parser fix, a new repository or
commit in the manifest, or a `flake.lock` update that changes ShellCheck or a
shell. Then give every `unclassified` line in `corpus/shells.txt` its category
by hand, and review the diff of the three files like a golden file: each changed
count and each added or removed line should be explained by the change.

## CI

The `corpus` workflow fetches the corpus once, caches it with
`actions/cache` under a key derived from `corpus/repos.txt`, and runs the
ShellCheck and the shells comparisons as two jobs in the Nix development
shell, so they use the tools `flake.lock` pins. A third job checks the
justfiles, Makefiles and Dockerfiles the same way, after allowing the unprivileged user
namespaces bubblewrap needs.

## Nix server parity

shell-lsp means to replace nixd and nil as a single server for Nix and the
shell it embeds. `cmd/nixparity` measures how far that is true instead of
claiming it: it starts two servers over stdio, shell-lsp and a reference, on
the same directory, opens the same Nix files in both, asks both the same
questions and compares the answers. Nix support in shell-lsp is still being
built, so the measurement shows gaps; those gaps are what it is for. The
reference servers are black boxes under [CLEAN_ROOM.md](../CLEAN_ROOM.md):
they are run and their answers compared. Reports and baselines hold counts
and positions only, never a message, a hover text or a completion label.

| Recipe | What it does |
| --- | --- |
| `just nix-parity [nixd\|nil]` | Compare over the two fixtures of `corpus/nix`, with nixd by default |
| `just nix-parity-check` | The same against nixd, failing when a count no longer matches its baseline |
| `just nix-parity-baseline` | Rewrite `corpus/nix/*.parity.json` from the current code and nixd |
| `just nix-parity-fetch` | Fetch the large flakes that [corpus/nix/repos.txt](../corpus/nix/repos.txt) pins |
| `just nix-parity-large [PATH] [nixd\|nil]` | Compare on the flake at PATH, or the pinned nixpkgs when PATH is empty |

The recipes need `nixd` or `nil` on `PATH`, and write `NAME.md` and
`NAME.json` under `shell-lsp/nixparity` in the user cache directory, besides
printing the Markdown. `nixparity compare` takes any directory and any two
command lines with `-ours` and `-theirs`; without `-ours` it builds shell-lsp
from the working directory. `-ours-init '{"nix":{"evaluate":true}}'` passes
initialization options to shell-lsp, and `-ours-settle 2m` gives it that long
between opening the files and the first question, which is how the evaluated
option and package answers are compared with nixd's.

### Fixtures

`corpus/nix/single-flake` keeps its outputs in one `flake.nix` that uses the
`inputs` argument and reaches other files through literal `import ./x.nix` and
`callPackage ./y.nix`. `corpus/nix/flake-parts` has a `mkFlake { inherit
inputs; }` root, an `imports` module tree, `perSystem` modules using `pkgs`,
`system`, `config` and `self'`, top-level `flake` and `systems`, and values
shared between modules through `config`. Both are small projects that evaluate
and build, with shell in their phases and `shellHook`, and pin the inputs of
this repository's `flake.lock`.

The large flake is kept out of `corpus/repos.txt`, whose repositories feed the
ShellCheck and shell baselines. `corpus/nix/repos.txt` has the same format and
is fetched by the same code into its own cache directory, `corpus-nix`. Its
`kind` column is not read here. Unlike the corpus above, a measured project
is not only read: a reference server may evaluate the Nix it is given, so
point `nix-parity-large` only at code you trust.

### What is compared

Files come from a walk that skips hidden directories, and `-files` of them
(200 by default) are sampled. A file over 4 MiB or not UTF-8 is listed as
skipped. In each file `-positions` names (20 by default) are sampled, leaving
out comments and keywords and keeping names inside shell strings. Sampling
depends only on `-seed` and the file's path, so a run asks the same questions
every time. Positions are in UTF-16, the only encoding offered to the servers.

| Feature | Asked | Compared as |
| --- | --- | --- |
| diagnostics | per file, as published | set of start positions |
| documentSymbol | per file | set of name and start of its selection range |
| foldingRange | per file | set of start and end lines |
| formatting | per file | the text after applying the edits |
| definition | per position | set of file and range |
| references | per position, with the declaration | set of file and range |
| rename | per position | set of file, range and new text |
| hover | per position | the hover range, when both give one; the wording is never compared |
| completion | per position, two characters into the name | set of labels |

Paths inside the project are compared relative to it. Each server's answer
gets one outcome:

- `answered`, or `empty` for null or an empty result. An empty edit list from
  a formatter is an answer: the text stays as it is.
- `refused` for an error response, `unsupported` for a capability the server
  does not declare or a method it does not know, and `silent` when it publishes
  no diagnostics for a file within 10 seconds of the last publication. A server
  need not publish for a clean file, so `silent` is not a failure.
- `malformed`, `timeout` or `crashed`: failures. Every call is limited by
  `-timeout`, 30 seconds by default.

The two outcomes of a question give one verdict: `agree` or `disagree` when
both answered, `ours missing` or `theirs missing` when one did, `both empty`
when neither did, and `failed` when either side failed. A failure never counts
as agreement or as an empty answer, whatever the other side said. Whole-answer
agreement is strict, so the report also counts the items of the compared
answers that are shared and that only one side gave.

### Differences that are decided

Every question on which a reference answers and shell-lsp does not, and every
class of disagreement, was opened and judged by Nix semantics. These classes
are deliberate: a report counts them, and they are not bugs of shell-lsp.

| Feature | Reference | What it does | Why shell-lsp differs |
| --- | --- | --- | --- |
| rename | nixd, nil | Renames a module argument such as `lib` or `config` | The module system passes it by that name, so the renamed code no longer evaluates. shell-lsp refuses and names the reason |
| rename | nixd, nil | Renames an attribute of a set that leaves its binding: a set passed on, read by a computed name, or an option declared outside the project | Nothing can follow the other readers. shell-lsp refuses with the reason, and renames what it can prove |
| rename | nixd, nil | Edits `inherit x` differently | Both rename the binding; shell-lsp's edit of an `inherit` covers a different range, so the set of edits differs at those positions |
| references | nixd | Lists every name read through a `with` for any one of them | They have nothing in common but the `with`. shell-lsp lists the reads of that name through that `with` |
| references | nixd | On `inherit (final) lib;` lists only the local uses | shell-lsp lists those and every other read of `final.lib`, which the same word also is |
| definition | nixd | On a nixpkgs attribute points at the value, so `lib.escapeShellArgs` lands inside `concatMapStringsSep`, and `lib.getExe` on the lambda after its name | shell-lsp points at the attribute, where its documentation is |
| definition | nixd | Takes `meta.position` as a zero-based line, one line below where it is | shell-lsp reads it as the one-based line it is |
| definition | nixd | On a formal in a module goes to that formal even when `_module.args` sets it | shell-lsp goes to the place that sets the value |
| definition | nixd | On an option gives only its declaration | shell-lsp also lists the assignments in the project, which are what a reader wants next |
| definition | nixd | `pkgs.x.override` lands in `lib/customisation.nix` | Every derivation has it; shell-lsp does not answer rather than name one implementation for all |
| completion | nil | Offers any word of the file, so `config.host.pl` gets `platform` whether or not it exists there | shell-lsp offers what the project declares or sets, and with `nix.evaluate` what evaluation finds |
| completion | nixd | Offers the package set in a plain name position, so `lib` completes to `lib25519` outside any `with pkgs` | Those names are not in scope there |
| completion | nixd | Reads the last attribute of a select at any cursor in it, so `lib.ty\|pes.lazyAttrsOf` offers `lazyAttrsOf` | At that cursor the attribute being typed is `types` |
| definition, completion | nixd | Reads a derivation's `meta`, `meta.maintainers` and `passthru` as the NixOS options of those names, so they land in `lib/modules/generic/meta-maintainers.nix` and `nixos/modules/misc/passthru.nix` | A derivation is not a module. shell-lsp offers the keys `mkDerivation` and `meta` take, and goes to the binding |
| completion | nil | Offers builtins right after the `{` of an argument set, and `pkgs` or `lib` while a formal is being written | Those are keys and names being declared, not expressions to read |
| hover | nil | On `a.b` of an unknown `a` answers ``Field `b` `` with the type `?` | It repeats the word under the cursor. shell-lsp answers once it knows what `a` is, as on `finalAttrs.version` |
| documentSymbol | nixd | Adds literal values and reads as children of an attribute | An outline of names reads better, and the value is one hover away. shell-lsp nests attribute paths and `let` blocks, and lists formals |
| foldingRange | nixd | Folds a whole `let … in body`, whose first line a client can fold only once | shell-lsp folds the bindings, keeping the body visible, and also folds comment runs, multi-line `inherit`s and strings |

Without `nix.evaluate`, nixd still answers `pkgs.x` because it evaluates
`<nixpkgs>` on its own. shell-lsp does not start an evaluator unless asked. It
reads the package set from the locked nixpkgs source instead, or from the
checkout when the file is inside nixpkgs: `pkgs/top-level/all-packages.nix`, the
trivial builders and `pkgs/by-name`, for definition and hover. Attributes that
only exist after evaluation, such as `pkgs.nerd-fonts.jetbrains-mono` and the
versions nixd shows as inlay hints, need the setting on.

### Latency, memory and failures

The servers run one after the other, one request at a time, so neither
disturbs the other's timing. The report gives p50, p90, p99 and maximum latency
per feature over the calls that got a response; for diagnostics, that is the
time from opening a file to its first publication. Peak RSS sums the server's
process group, sampled every 250 ms, and is at least the peak the kernel
reports for the server process itself. Idle RSS samples the same group once more, three seconds after the last
request. Both are measured on Linux and reported as `not measured` elsewhere.

A server that exits is restarted with its files open again, and one that
times out three times in a row is killed and restarted, at most three times per
run; after that every remaining question counts as `crashed`. Every server
process group is killed and reaped when its run ends.

### The baseline

`just nix-parity-check` compares the counts with
`corpus/nix/FIXTURE.parity.json` and fails on any change, including a change
in the reference server's version, the sampling or the skipped files. A
regression, fewer agreements or more disagreements, failures or questions only
the reference answers, prints the examples for that feature; any other change
asks for the baseline to be refreshed. Latency, memory and restarts are left
out because they differ between runs and machines: compare them by reading the
reports. Refresh with `just nix-parity-baseline` and review the diff like the
other baselines. CI does not run this check.
