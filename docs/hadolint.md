# Hadolint compatibility

shell-lsp checks Dockerfiles with the rules Hadolint is known for, under
Hadolint's `DL` codes, and reads Hadolint's inline pragmas and `.hadolint.yaml`.
A project that runs Hadolint can drop it and keep its configuration: the editor
shows the same rules while typing, and `shell-lsp check` runs them in CI.
Every rule is listed with its severity in [checks.md](checks.md#dl1001).

The rules were written from what Hadolint does, never from how it does it:
Hadolint 2.14.0 was run as a black box on more than 500 Dockerfiles written
for the purpose, recording only codes and positions, as
[CLEAN_ROOM.md](../CLEAN_ROOM.md) requires. The cases live in
`internal/hostnav/testdata/hadolint`, and `go test ./internal/hostnav` checks
each of them against both shell-lsp and the live Hadolint. Wherever the two
disagree, the case says why, in a line such as
`>>+ DL3008 2 -- /usr/bin/apt-get is apt-get, called by its path`.
`just corpus-hosts` compares them on the Dockerfiles of the corpus as well.

## Rules

The `DL3xxx` and `DL4xxx` rules check how images are built: package versions
and caches for apt, apk, yum, dnf, microdnf, zypper, pip, npm, gem, yarn and
go, image tags and registries, `USER`, `WORKDIR`, `COPY` and `ADD`, `EXPOSE`,
`ENV`, pipes without `pipefail`, consecutive `RUN`s, and labels.

Some problems are also checks BuildKit itself makes, which shell-lsp already
runs under `SL7` codes ([rules.md](rules.md#sl7001)). Those are reported once,
under the `SL` code, and the `DL` code stands for it wherever a code is named:
in `# hadolint ignore=`, in `ignored` and in `override`.

| Hadolint | Reported as |
| --- | --- |
| DL1000 | [SL7001](rules.md#sl7001), [SL7002](rules.md#sl7002), [SL7003](rules.md#sl7003) |
| DL3000 | [SL7015](rules.md#sl7015) |
| DL3012, DL4003, DL4004 | [SL7016](rules.md#sl7016) |
| DL3022 | [SL7005](rules.md#sl7005) |
| DL3023 | [SL7024](rules.md#sl7024) |
| DL3024 | [SL7007](rules.md#sl7007) |
| DL3025 | [SL7004](rules.md#sl7004) |
| DL3029 | [SL7022](rules.md#sl7022) |
| DL3043, DL3061 | [SL7001](rules.md#sl7001) |
| DL4000 | [SL7014](rules.md#sl7014) |

Hadolint runs ShellCheck on the shell of each `RUN`. shell-lsp lints that shell
with its own linter, under the same `SC` codes, as the shell the stage's
`SHELL` names, and a pragma above a `RUN` covers its shell findings too.

Two rules are off until asked for, as in Hadolint: DL1001, which reports
every `# hadolint ignore=` pragma, and DL3057, which reports an image without a
`HEALTHCHECK`. Naming them under `override` turns them on, as does enabling
the optional checks `no-ignore-pragmas` and `require-healthcheck` with
`lint.optional`, `--enable` or `enable=` in `.shellcheckrc`.

## Pragmas

A comment line of its own, outside any instruction:

- `# hadolint ignore=DL3008,SC2086` ignores those codes in the instruction
  that starts on the next line. A blank line or another comment in between
  ends its reach.
- `# hadolint global ignore=DL3059` ignores them in the whole file, wherever
  it stands.
- `# hadolint shell=bash` lints the shell of every later `RUN` in the stage as
  that shell, as a later `SHELL` instruction would; whichever comes last wins.
  A shell shell-lsp does not read, such as `pwsh`, leaves those `RUN`s alone.

A trailing `# comment` after the codes is allowed. A pragma with an unknown
code, an empty item or a second word is not a pragma and ignores nothing.

## Configuration

shell-lsp uses the `.hadolint.yaml` or `.hadolint.yml` nearest to the
Dockerfile, looking in its directory and then in each directory above it, the
way `.shellcheckrc` is found. Hadolint looks only in the directory it is run
from, which an editor does not have; projects keep the file at their root, so
both find the same one. When no project has one, the first of these is used:
`$XDG_CONFIG_HOME/hadolint.yaml` (or `~/.config/hadolint.yaml`),
`~/.hadolint/hadolint.yaml` and `~/.hadolint.yaml`, each also as `.yml`.
Files are not merged. The editor rereads a file when it changes.

| Setting | What shell-lsp does with it |
| --- | --- |
| `ignored` | A list of codes reported nowhere in the Dockerfile, `DL`, `SC` or `SL`. |
| `override` | A mapping of `error`, `warning`, `info` and `style` to lists of codes reported at that severity. A code under several takes the most severe. |
| `trustedRegistries` | A list of registries `FROM` may pull from (DL3026); `*` matches any run of characters, so `*.gcr.io` trusts `eu.gcr.io`. Without the setting, DL3026 is off. |
| `label-schema` | A mapping of label names to `text`, `url`, `email`, `rfc3339`, `spdx`, `hash` or `semver`. The final image must carry each label (DL3049), and each must have a value of its kind (DL3051 to DL3058). |
| `strict-labels` | `true` reports labels the schema does not name (DL3050). |
| `disable-ignore-pragma` | `true` makes `ignore=` and `global ignore=` pragmas ignore nothing. |
| `failure-threshold`, `no-fail`, `format`, `no-color`, `verbose` | Checked, then left to Hadolint: they shape Hadolint's own output and exit status. `shell-lsp check` has its own options for both ([cli.md](cli.md)). |

A configuration with a mistake is not applied. The server logs a message to
the editor that names the file, line and column, as it does for a broken
`.shellcheckrc`, and `shell-lsp check` stops with that error.
Unlike Hadolint, shell-lsp also treats an unknown setting, a level that
`override` does not know, and a code that is no check code as mistakes, so a
misspelt setting cannot go unnoticed; an empty file, on the other hand,
configures nothing and is no mistake. The file is read as YAML without anchors,
aliases, tags or block scalars, and with each value on one line.

Hadolint's command-line flags and its `HADOLINT_` environment variables are not
read.

## Where shell-lsp differs

Hadolint matches the words of a command where shell-lsp reads the command the
way the tool does. The cases record every difference; these are the kinds.

- **Commands.** A command called by its path, such as `/usr/bin/apt-get`, or
  through `sudo`, `env`, `nice`, `xargs`, `exec` or `command`, is the command
  it runs. The first word after the options is the subcommand, so
  `apt-get remove vim install git` removes, and the words after `--`, the
  values of options such as `-o` or `--target-release` and the build
  arguments after `gem install x --` are no packages.
- **What the tools accept.** Each tool's own rules decide: apt-get's quiet
  level 2 or more, `-o APT::Get::Assume-Yes=true` and `apt-get dist-clean`;
  apk's `~` versions and `--no-network`; pip's requirement and constraint
  files, VCS references and every boolean value of `PIP_NO_CACHE_DIR`; npm's
  `npm i` and `#ref` git specs and its `latest` tag; local paths and archives,
  which are fixed files; go's local packages and `go run` arguments.
- **Cleaning up.** A cache counts as removed when the removed path or the
  cache mount covers it, whatever `rm` options spell it, and not when an
  unrelated `rm -rf` runs. Caches, labels, the last `USER` and `HEALTHCHECK`
  matter only in the final stage and the stages others are built `FROM`; a
  build stage that only `COPY --from` reads never becomes an image.
- **Values.** `$NAME` takes the value `ARG` or `ENV` gives it, so
  `useradd -u "$UID"` with `ARG UID=1000` is fine. A value nothing sets, such
  as an `ARG` without a default, is unknown and reported by no rule.
- **The Dockerfile grammar is BuildKit's.** Stage names are case-insensitive,
  `localhost:5000/app` names the registry `localhost:5000`, `EXPOSE 53/sctp`
  and `ADD` heredocs are valid, and a syntax error does not stop the other
  checks. The exec form of `RUN` runs no shell, so `&&` in it is a word, and
  neither a PowerShell `RUN` nor the string `sh -c` runs is read as shell.
- **Formats.** RFC 3339, SemVer and email values are held to their
  specifications; `spdx` accepts SPDX license expressions over License List
  3.27, which the OCI `licenses` label is defined to hold; and `hash` accepts
  abbreviated and SHA-256 commit hashes.
- **Places.** A finding sits on the word it is about, such as the unpinned
  package, and each such word gets one, where Hadolint reports each rule once
  per instruction at its first column.
