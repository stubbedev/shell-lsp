# Shell scripts

What shell-lsp does for a script beyond the linter, which has its own page
([linting.md](linting.md)). The [README](../README.md#shell-scripts) covers
dialects, manual pages and refactorings.

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

Documentation and subcommands of external commands come from their manual pages;
see [Completion and hover](../README.md#completion-and-hover) in the README.

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
