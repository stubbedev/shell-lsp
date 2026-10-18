# Clean-room policy

shell-lsp is MIT-licensed. ShellCheck, whose check codes, directives and
command line it stays compatible with, is licensed under the GPL-3.0, as is
Hadolint, whose check codes, pragmas and configuration file it stays
compatible with for Dockerfiles, and bash-language-server under the MIT
licence. To keep every line of shell-lsp
its own, these projects are reimplemented from what they do, never from how
they do it. This file states what may and may not inform the code, the tests
and the documentation. It applies equally to people and to the AI agents that
do most of the work (see [How it was built](README.md#how-it-was-built)).

## Projects this covers

- [ShellCheck](https://github.com/koalaman/shellcheck)
- [Hadolint](https://github.com/hadolint/hadolint)
- [bash-language-server](https://github.com/bash-lsp/bash-language-server)
- Any other shell linter or language server, whatever its licence, when its
  behaviour is compared with ours.

The parser and formatter are not covered: they are a fork of
[mvdan.cc/sh](https://github.com/mvdan/sh), reused under its BSD licence in
[third_party/mvdan-sh](third_party/mvdan-sh), where every change is listed.

## What may be used

- **Running the tools as black boxes.** Feeding them scripts and recording
  what comes out: check codes, positions, severities, exit statuses, which
  scripts they accept, what a language server answers. This is how the lint
  fixtures are labelled (`just fixtures`) and how `just lintcheck` and
  `just crosscheck` measure the differences.
- **Their public interfaces, to stay compatible.** Check codes and their
  numbering, the `# shellcheck` directive and `.shellcheckrc` syntax, command
  line flags, output formats and exit codes. Compatibility with these is the
  point; their wording is not part of them.
- **Primary sources about shells.** The POSIX specification, the manuals of
  bash, zsh, dash, ksh93, mksh and BusyBox, the Language Server Protocol
  specification, published style guides, and running the shells themselves.
  When a reference tool and a real shell disagree, the shell is right.
- **Feature lists.** Knowing that another tool offers a check or an editor
  feature may prompt us to build one. The specification for it is then
  written from the sources above.

## What may not be used

- **Source code**, including tests, test data and commit history, of the
  projects above.
- **Their prose.** Rule descriptions, diagnostic messages, wiki pages,
  documentation examples and code comments are not read when implementing or
  documenting a check, and nothing is copied from them. Every message,
  summary, fix title and document in this repository is written afresh.
- **Their output as text.** Fixtures and baselines record codes and
  positions only. Where shell-lsp deliberately differs, the reason is
  written in our own words next to the case.

Linking to ShellCheck's wiki from a diagnostic, so a user can read it, is
fine: a link copies nothing.

## AI agents

Agents are given the task, this policy, the repository and the primary
sources; their prompts must not include text from the projects above. An
agent that studies a reference tool runs it and must not open its source.
The language models behind the agents were trained on public code, which
may include these projects, so this policy governs what is consulted while
the work is done, and the tests are the evidence of behaviour: they were
labelled by running the tools, not by reading them.

## Contributing

By contributing, you confirm that your change follows this policy. If you
have read the source code of one of the projects above, say so in your pull
request and do not implement the corresponding behaviour yourself; describe
it as a behaviour, with examples, so someone else can.
