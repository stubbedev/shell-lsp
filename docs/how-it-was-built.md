# How it was built

## Inspiration

shell-lsp exists because of two projects, and owes its Dockerfile rules to a
third:

- [bash-language-server](https://github.com/bash-lsp/bash-language-server)
  set the baseline for what a shell language server offers: diagnostics,
  completion, hover, definition, symbols and formatting. shell-lsp covers
  that ground for every dialect, not only bash, and goes on to the rest of
  the LSP specification.
- [ShellCheck](https://github.com/koalaman/shellcheck) defined what linting
  a shell script means: its check codes, `# shellcheck` directives,
  `.shellcheckrc` files and command line are what scripts and CI pipelines
  already rely on.
- [Hadolint](https://github.com/hadolint/hadolint) did the same for
  Dockerfiles: its `DL` codes, `# hadolint` pragmas and `.hadolint.yaml` are
  what Dockerfile pipelines rely on.

All three were reimplemented clean-room, without reference to their source code.
Each was treated as a black box whose observable behaviour served as the
specification; [CLEAN_ROOM.md](../CLEAN_ROOM.md) states what may and may not be
used:

- ShellCheck is only a test oracle. The lint fixtures under
  `internal/lint/testdata/fixtures` are original scripts whose expected
  findings were recorded by running it (`just fixtures`), with deliberate
  differences marked and explained in place, and `just lintcheck` measures
  the linter's precision and recall against it, code by code, over real
  scripts.
- bash-language-server is compared from the outside: `just crosscheck` runs
  both servers over the same corpus and records their diagnostics and
  completion recall in [crosscheck.md](crosscheck.md).
- Hadolint is only a test oracle too: the Dockerfiles under
  `internal/hostnav/testdata/hadolint` were written for the purpose, their
  expected findings recorded by running it, with deliberate differences
  explained in place ([hadolint.md](hadolint.md)).

The Nix side follows the same policy. nixfmt, deadnix, nixd and nil are run as
black boxes, and statix is treated more narrowly for its documented lint names
and behaviour, as [CLEAN_ROOM.md](../CLEAN_ROOM.md) spells out.

What carries over is the interface, so existing scripts, annotations and
configuration keep working: the `SC` check codes and their numbering, the
directive and `.shellcheckrc` syntax, and the options, output formats and
exit codes of the `shellcheck` command line. Every message, summary, fix
and line of the implementation is shell-lsp's own.

Parsing and formatting build on code that is reused, not reimplemented: a
fork of [mvdan.cc/sh](https://github.com/mvdan/sh) v3.14.1, the library
shfmt is built on, lives in [third_party/mvdan-sh](../third_party/mvdan-sh)
under its own BSD license. The fork adds the rest of the zsh grammar and a
ksh93 variant; every change is a patch with a reproducer, listed in its
[README](../third_party/mvdan-sh/README.md) so it can be sent upstream.

## Who wrote it, and what backs it up

AI agents wrote the code, the tests and the documentation. The author chose
what to build and how far to take it, set the design rules the code has to
follow, and decided what landed. Correctness rests mainly on checks that
anyone can rerun, rather than on line-by-line review of the code:

- **Tests came before the implementation, from the reference tools.** The
  agents that implemented a feature worked against tests derived from what
  ShellCheck, bash-language-server and shfmt observably do, not from their
  source, as described under [Inspiration](#inspiration). The lint fixtures
  record ShellCheck's own findings, and each place where shell-lsp
  deliberately differs is marked in the fixture with its reason. Beyond those
  tools, the scope follows what gopls and rust-analyzer offer, such as
  refactorings and completion that depends on context. Parsing and
  formatting are the exception: they reuse shfmt's library through the fork
  in `third_party/mvdan-sh` rather than reimplementing it.
- **Real shells settle disagreements.** When the documentation, ShellCheck
  and shell-lsp disagree about a shell, the shell itself decides: the flags
  each builtin accepts and the option each `set` letter turns on, for
  instance, were checked by running bash, dash, busybox, zsh, ksh93 and
  mksh.
- **Measurement against the references.** `just lintcheck` reports the
  linter's precision and recall against ShellCheck, code by code, over real
  scripts, and `just crosscheck` records how its diagnostics and completion
  compare with bash-language-server's in
  [crosscheck.md](crosscheck.md). `just corpus-check` holds the
  linter to ShellCheck and the parser to the shells' own `-n` over a pinned
  corpus of public repositories and the Oils spec tests, against committed
  baselines that CI enforces ([corpus.md](corpus.md)).
- **The same gates for every change.** `go vet`, a strict golangci-lint
  configuration, the test suite on Linux, macOS and Windows (with the seed
  corpora of its fuzz tests, whose targets are also fuzzed every night),
  headless Neovim and VS Code integration tests, a check that release
  binaries match the names the VS Code extension downloads, the Nix build,
  the test suite again in the Nix dev shell, where
  the lint fixtures are checked against ShellCheck and the parser against
  bash, dash, mksh, zsh and ksh93, and CodeQL, govulncheck and Grype scans.

Maintenance follows the same pattern. Refactoring, for instance, starts with
agents auditing the code for facts stated in more than one place and
invariants that only comments enforce; other agents then fix each finding as
its own commit, with a regression test for every bug it uncovers, and each
commit has to build and pass the tests on its own before it lands.
