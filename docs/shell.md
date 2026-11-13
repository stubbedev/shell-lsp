# Shell scripts

What shell-lsp does for a script beyond the linter, which has its own page
([linting.md](linting.md)): dialects, completion and hover, sourced files,
embedded programs, refactorings and Bats.

## Dialects

Each document is linted as one shell. The first of these that applies decides which:

1. the `dialect` setting;
2. `shell=` in the nearest `.shellcheckrc`;
3. `shell_variant` in `.editorconfig`;
4. a `# shellcheck shell=` directive in the comment header before the first
   command;
5. the shebang, also behind `env`, `busybox` and `nix-shell -i`, and with
   the interpreter's version in its name (`bash5`, `zsh-5.9`);
6. a zsh completion tag (`#compdef`, `#autoload`) on the first line;
7. the file name (`.bashrc`, `.zshrc`, `.profile` and the other rc files) or
   its extension (`.bash`, `.bats`, `.zsh`, `.dash`, `.sh`, `.ksh`);
8. the editor's language identifier, unless it is the generic `shellscript`;
9. a vim modeline (`# vim: ft=zsh`);
10. otherwise bash.

An extension or the bash default is only a guess, and a wrong guess is the usual cause of findings
that make no sense for the shell you use. When nothing names the shell at all, SC2148 says so, in the
editor as in `shell-lsp check`. The VS Code extension shows the shell in the status bar with what
decided it, and clicking it records your choice as a `# shellcheck shell=` directive, which every
editor and `shell-lsp check` then follow.

The parser understands the whole grammar of each shell, including zsh's
short and brace forms of loops and `if`, `foreach`, `repeat`,
`{ } always { }`, anonymous functions with arguments and glob qualifiers,
and ksh93's compound variables and types, `namespace` and dotted names. A
ksh script is parsed as ksh93 unless its shebang, shell directive or file
name says mksh.

## Completion and hover

Besides commands, variables and paths, completion follows the shell's own
vocabulary, filtered by the dialect of the document:

- Builtin flags (`read -`, `declare -`, `kill -`, `mapfile -`...) from a table
  per shell, so zsh offers `read -A` and dash offers only `read -r`.
- Test expressions in `[[ ]]`, `[ ]` and `test`: unary operators at the start,
  binary ones after an operand (`=~` and `<` only inside `[[ ]]`), variable
  names after `-v` and option names after `-o`.
- Option names after `set -o`, `shopt` (the `set -o` names after
  `shopt -o`), `setopt` and `unsetopt`, `emulate` modes, trap conditions and
  signal names after `trap`, and signal names alone after `kill`.
- Expansion operators after `${name` (`:-`, `##`, `//`, `@Q`...), zsh
  parameter flags after `${(`, glob qualifiers after `*(` and `(#q`,
  globbing flags after `(#`, associative array keys inside `${name[`, and
  `printf` conversions in the format.
- `case` patterns: the letters of the matching `getopts` optstring, or the
  values the subject is compared against elsewhere in the file.
- Snippets, for clients that support them: keyword skeletons, and idioms
  such as a `getopts` loop, a cleanup trap, `mktemp` with cleanup, strict
  mode and the script's own directory, written for the document's dialect.

Documentation for external commands comes from the installed manual pages,
which the server locates through `MANPATH` (where it is unset or has an
empty entry, in the `share/man` and `man` directories next to every `PATH`
entry and in `/usr/share/man`, `/usr/local/share/man`, `/usr/local/man` and
`/opt/homebrew/share/man`), decompresses and renders itself; neither `man`
nor groff is involved. Subcommands come from the same pages: after `git`,
`systemctl` or `docker` completion offers the documented subcommands, and
the flags completed and hovered after `git checkout` are those of
git-checkout(1), falling back to the command's own page. Nested subcommands
such as `git remote add`, `kubectl config set-context` or `ip link set`
resolve as deep as the pages document them, global options before a
subcommand are skipped with their values (`git -C dir checkout`,
`kubectl -n ns get`), and commands behind the wrappers `builtin`, `busybox`,
`command`, `doas`, `env`, `exec`, `nice`, `nohup`, `setsid`, `sudo`, `time`,
`timeout`, `watch` and `xargs` complete and hover as if typed directly.

Completing inside a word can replace the rest of it (for clients with
insert-replace support). Hover explains the same vocabulary where it is
used, reading each argument by its position exactly as completion does:
builtin flags (also combined, as in `read -rs`), test, arithmetic,
list and `case` operators, redirections and whether a here-document body is
expanded, signals, option names, expansion operators, and every letter that
`$-` can contain.

### Patterns and expansions

Scripts often use the shell's own patterns and parameter expansions instead
of running sed, grep, awk, basename or tr, and those are the hardest part of
a script to read. Hovering one explains it:

- A glob pattern is taken apart piece by piece, wherever it appears: in
  `${var#…}`, `${var%%…}`, `${var//…/…}`, zsh's `${var:#…}` and subscript
  searches, `[[ … == … ]]`, `case` items and file names. That covers POSIX
  wildcards and bracket expressions, bash and ksh extended globs (`@(…)`,
  `!(…)`, ksh93's `{2,3}(…)`, non-greedy `*-(…)` and `~(i)`, `~(E)` and
  `~(F)` options), and zsh's groups, numeric ranges (`<1-10>`),
  `^`, `~`, `#` and `##`, globbing flags such as `(#i)`, `(#b)` and `(#s)`,
  and glob qualifiers such as `(.om[1,3]N)`. Captured groups are numbered
  as they land in `$match`, and an operator whose shell option the file
  never enables (`extendedglob`, `extglob`, `globstar`) is pointed out.
- The regular expression of `[[ … =~ … ]]` is taken apart the same way,
  following each shell's quoting rules for what reaches the regex engine,
  with its groups numbered as they land in `BASH_REMATCH` or `$match`.
- When the tested value is known, `[[ … == … ]]`, `case` items and `=~`
  say whether they match, and `=~` shows what `BASH_REMATCH` or `$match`
  then holds.
- A parameter expansion shows its result when the values it reads are known
  from literal assignments earlier in the file, naming the lines it took
  them from: `${f:t:r}` with `f=archive.tar.gz` gives `archive.tar`, and
  `${line//(#b)([^= ]##)=([^ ]#)/$match[2]:$match[1]}` swaps every key and
  value. The evaluator is tested against bash and zsh themselves and covers zsh's flags
  (`(U)`, `(s:,:)`, `(j:,:)`, `(o)`, `(u)`, `(M)`...), modifiers (`:h`,
  `:t`, `:r`, `:e`, `:gs/x/y/`...), nested expansions and `$match`, and
  bash's `${x^^}`, `${x@U}`, slices and the `&` of `patsub_replacement`.
  Where a value comes from a command, a loop or a computation, or a result
  would depend on the locale or a shell option the file does not settle, it
  shows nothing rather than a guess.

Pattern operators, bracket expressions, numeric ranges, globbing flags,
glob qualifiers and the parts of a regular expression are also highlighted
as semantic tokens. The linter reports pattern operators whose option is
never turned on, capture variables read without a capturing match, bash's
`&` in a replacement, zsh patterns that zsh rejects
([SL2016](rules.md#sl2016) to [SL2019](rules.md#sl2019)) and
regular expression escapes such as `\d` that `=~` does not understand
([SL2022](rules.md#sl2022)). It suggests the shell's own way when
`basename`, `dirname` (in zsh), `pwd`, `tr`, `cut`, `grep -q`, sed (SC2001)
or, as the optional `useless-use-of-cat` check, `cat` start a process for
what the shell can do itself ([SL2020](rules.md#sl2020),
[SL2021](rules.md#sl2021)), with a fix that rewrites it.

## Sourced files

Files pulled in with `source` or `.` act as imports: their functions and
globals are completed, hovered, highlighted and navigated to as if they were
defined in the document, open editor buffers win over the copy on disk, and
the linter follows them. Paths built from `$0`, `${BASH_SOURCE[0]}`,
`dirname`, `cd ... && pwd` and earlier assignments are resolved, as are
`# shellcheck source=` directives. Renaming or moving a script in the editor
rewrites the `source` paths that point at it, and a rename that changes its
extension from one shell to another (`.sh` to `.bash`) updates its shebang
to match.

## Embedded languages

Inside an awk or jq program, completion offers its functions and the variables its
command line sets, and signature help follows the arguments of a call.

awk, sed and jq programs passed as literal arguments (`awk '...'`, `sed -e
'...'`, `jq -r '...'`) or fed as a quoted here-document to `awk -f -` and
`sed -f -` are parsed by built-in parsers; no awk, sed or jq binary is run.
The command is also found behind the same wrappers completion looks
through and as the command of
`find ... -exec`, `-execdir`, `-ok` or `-okdir`; a program containing the
`{}` or `xargs -I` placeholder those substitute at run time is skipped, and
`xargs` does not hand its command the statement's here-document. Syntax
errors, and definite mistakes in their regular expressions (sed addresses and
`s`, awk `/re/` and `~ "re"`, the patterns of jq's `test`, `match`, `sub` and
friends), are reported as lint findings at their exact position inside the
shell string with the native codes [`SL3001`](rules.md#sl3001) (awk),
[`SL3002`](rules.md#sl3002) (sed) and [`SL3003`](rules.md#sl3003)
(jq), up to ten per program, so `lint.enable`, `lint.exclude` and
`# shellcheck disable=` apply to them like to any other check. The programs
are highlighted through semantic tokens, and hovering an awk or jq builtin, or
a sed command or flag, shows its documentation. Programs that contain shell
expansions, or come from files, are skipped rather than guessed at, and
common GNU and gawk extensions are accepted. The one exception is a value no one
can know that the file around the shell puts there, such as a Nix `${x}` or a
just `{{x}}`, and a shell's `$$`: it stands for one word of the program, and a
finding next to it is dropped.

A variable bound on the command line (`jq --arg name "$value"`, `--argjson`,
`--slurpfile`, `--rawfile`, `awk -v name="$value"`, `--assign`) is known in the
program: hovering `$name` in jq or `name` in awk says which flag sets it and to
what. In a file with a host language this goes on through the chain, see
[Nix files](nix.md). A program variable that the program also declares (`as
$name`, a function parameter) is left alone.

## Refactorings

Code actions offered at the cursor or selection, each only where it applies and keeps the script's
meaning; their edits are computed on `codeAction/resolve` when the client supports it, and an action
that edits another file as well carries a change annotation asking the client to confirm it, so
editors that support annotations show a preview first.

| Action | Kind | What it does |
| --- | --- | --- |
| Extract to variable | `refactor.extract` | Moves the selected word or part of a word into a variable |
| Extract to function | `refactor.extract` | Moves the selected statements into a new function |
| Toggle quotes | `refactor.rewrite` | Converts between single and double quotes |
| Remove the quotes | `refactor.rewrite` | Unquotes a quoted literal made only of letters, digits and `_ . / : @ % + -`, which the shell reads the same either way; not offered for a command name, a here-document delimiter or inside arithmetic |
| Declare as local | `refactor.rewrite` | Adds `local` to an assignment inside a function |
| Convert backticks | `refactor.rewrite` | Rewrites `` `cmd` `` as `$(cmd)`, undoing the backquote escaping |
| Convert to `[[ ]]` | `refactor.rewrite` | Rewrites `[ ... ]` or `test ...` in bash, zsh and ksh, turning `-a`, `-o`, `\(`, `\)`, `\<` and `\>` into `&&`, `\|\|`, `(`, `)`, `<` and `>`; skipped when globs or unquoted expansions would be read differently |
| Inline variable | `refactor.inline` | Replaces the reads of a variable assigned once to a literal with the value, quoted for each place it is read, and removes the assignment; only when nothing else writes, exports, unsets or could read the variable before the assignment or from another scope. A word spelling the name counts as harmless only as an argument of `echo`, `cd`, `pwd`, `type`, `true`, `false`, `:` or (in bash and sh) an external command, or as a redirection target; `eval`, `trap` and `alias` block it unless all their arguments are literals that never mention the name; and `source`/`.` blocks it only when a sourced file, followed transitively, cannot be resolved or touches the variable outside its own `local`s |
| Toggle the function keyword | `refactor.rewrite` | Switches between `function f { ... }` and `f() { ... }` when the cursor or selection is on the keyword, the name or its `()`; the keyword form is only offered in bash and zsh, and ksh is left alone because there the two forms scope variables differently |
| Add strict mode | `source.strictMode` | Adds whatever part of `set -euo pipefail` (`set -eu` in sh, dash and busybox) a script does not already enable, counting shebang flags, `set -o` names and zsh's `setopt err_exit`, `no_unset` and `pipe_fail` in any case or underscore spelling; it extends the first top-level `set` or `setopt` line that enables some of them, or else inserts a `set` line after the shebang and leading comments |
| Add shebang | `source.shebang` | Inserts the interpreter line for the detected shell (`#!/usr/bin/env bash`, `#!/bin/sh`, ...) into a script that has none; not offered for Bats files or zsh completion functions, whose first line must stay `#compdef` or `#autoload` |
| Convert to case | `refactor.rewrite` | Rewrites an `if`/`elif` chain whose every condition compares the same expansion for equality (`[ "$x" = lit ]`, `test`, `[[ $x == pattern ]]`, joined with `\|\|` or `-o`) into `case`, keeping each body and the `else` branch; one-line chains and `then`, `else` or `fi` sharing a line with commands are split onto their own lines, and the arms follow the document's tab or space indentation (falling back to the configured `indent`) and the `switchCaseIndent` / `switch_case_indent` setting |
| Move function to a sourced file | `refactor.move` | Moves a top-level function, with the comment block directly above it, to the end of a workspace file this script sources before defining the function, as one edit across both files; not offered when the target file is written for another shell that would not parse the function or that the portability checks (`SC3xxx`) flag it for, such as bash syntax moving into an sh, dash or busybox file |

## Bats

In files with a `bats` shebang, every `@test` appears in the outline and in
workspace symbols, and `setup`, `teardown`, `setup_file` and `teardown_file`
are marked as hooks. Code lenses offer "Run file" at the top of the file and
"Run test" above each test. A test whose name contains an expansion gets
"Run matching tests" instead: the literal parts of its name, with anything
in between, select it (and any sibling that happens to match).

Running uses the project's own bats: `test/bats/bin/bats`,
`node_modules/.bin/bats` or `test/libs/bats/bin/bats` under the workspace
folder, then `bats` on `PATH`. It runs with `--tap` (and an anchored
`--filter` for a single test) from the workspace folder, reports progress per
test, shows a passed/failed/skipped summary, and turns each failure into an
error diagnostic with source `bats` on the failing line, or on the test when
the output names no line in the file. Those diagnostics stay until the file
or that test runs again, and they move with their test (or helper function)
as the buffer is edited, even when bats ran an older saved copy. A run is stopped when cancelled and after 30
minutes, and stopping it ends every process the suite started, not only
bats itself.
