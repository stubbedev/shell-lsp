# Linting

The linter behind the diagnostics and `shell-lsp check`. Every check is listed
in [checks.md](checks.md), and `SL` checks are explained in [rules.md](rules.md).
[Profiles](../README.md#profiles) are described in the README.

The linter is built into the server; nothing else needs to be installed. It
uses shellcheck's check codes and honours `# shellcheck disable=...`,
`source=` and `shell=` directives, so existing scripts and annotations carry
over unchanged. It also lints zsh, which shellcheck does not support, by
zsh's own rules: unquoted parameters are neither split nor globbed unless
`SH_WORD_SPLIT` or `GLOB_SUBST` is set, glob qualifiers, parameter and
subscript flags are understood, and the parameters zsh, its line editor,
completion system and modules set or read count as assigned and used. It
follows sourced files without extra flags.

Quick fixes are offered where a check has an unambiguous repair, each titled
by what it does (`Double-quote $1`, `Replace -a with -e`, `Add || return`),
alongside actions that insert a `disable` directive for the command or the
whole file, for `SC` and `SL` codes alike. A fix is either safe, meaning it
keeps the script's behaviour (adding `-n`, bracing `$var`, quoting a value
known to hold no spaces or globs), or a suggestion that changes behaviour the
way the finding asks for (quoting `$1`, adding `|| exit`). The first fix of a
finding is the preferred one; the fix-all source action and the
`shellLsp.fixAll` command apply only safe fixes, so running them on save never
changes what a script does.

On a diagnostic, the actions come in this order: its fixes; `Fix all SC2086
in this file (3)`, which applies the preferred fix of every finding of that
code and is titled `(3, review)` when any of those fixes changes behaviour;
`Ignore SC2086 for this command`; `Ignore SC2086 in this file`, which extends
a `disable` directive in the file's leading `# shellcheck` comments or adds
one after the shebang and those comments; and `Ignore SC2086 in this
workspace`, which adds the code to the `disable=` line of the nearest
`.shellcheckrc` in the workspace folder. When there is none, it creates one at
the folder root, carrying over the settings of a `.shellcheckrc` that applied
from outside the folder, and only for clients that can create files. Edits
to `.shellcheckrc` ask the editor for confirmation where it supports change
annotations. An ignore action is not offered for a code that is already
disabled there.

Hovering a diagnostic explains it in place: its one-line summary and
severity, whether it is optional, a before-and-after preview of its preferred
fix marked safe or for review, an example for `SL` checks, the `disable`
directive that silences it, and links to [checks.md](checks.md) or
[rules.md](rules.md) and the ShellCheck wiki. Clients that support
it also get the documentation link on the diagnostic's code.

Checks that go beyond ShellCheck use `SL` codes and are documented in
[rules.md](rules.md), among them untrusted data pasted into code
run by `eval`, `sh -c` or `source`, downloaded code run unchecked, system
directories, home directories and SSH keys made writable by everyone, functions
a script never calls, temporary files it never removes, `trap` and `exit`
that only affect a subshell, syntax errors in embedded awk, sed and jq, and
optional rules from the Google Shell Style Guide.
They are configured exactly like the `SC` checks.

## Directives

`# shellcheck` directives are edited like code:

- Completion offers the keys after `# shellcheck `, every `SC` and `SL` code
  with its severity after `disable=` (also after a comma, after the dash of a
  range and for a bare number such as `2086`, never a code already listed),
  optional check names after `enable=`, shell names after `shell=`, and paths
  after `source=` and `source-path=`. Typing `=` or `,` opens it, since
  editors do not suggest inside comments on their own.
- Hovering a code shows its one-line summary, severity, whether it is
  optional and how to turn it on, a link to its documentation, and which
  findings it silences in the file; keys and optional check names explain
  themselves too.
- The optional `unused-directives` check (`SL2009`) reports disabled codes
  that silence nothing, with a quick fix that removes them.
- The path in `source=` opens with go-to-definition and is a document link,
  and directive keys and values are highlighted.

## .shellcheckrc

The nearest `.shellcheckrc` or `shellcheckrc` above a script, or the one in
`$XDG_CONFIG_HOME` or the home directory, applies in the editor as it does to
`shell-lsp check`: its `disable`, `enable` and `extended-analysis` settings
add to `lint.exclude` and `lint.optional`, and its `shell` chooses the
dialect unless the `dialect` setting does. Its `source-path` setting is only
read by `shell-lsp check`. Completion and hover inside a
`.shellcheckrc` file are not available: editors do not send that file to a
shell language server.
