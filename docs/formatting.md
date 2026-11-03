# Formatting

The same formatting answers `textDocument/formatting` in an editor and
`shell-lsp fmt` on the command line ([cli.md](cli.md#fmt)). No formatter binary
is run.

| File | Formatted like | Configured by |
| --- | --- | --- |
| Shell scripts | shfmt, with the printer it is built on | the `format.*` settings and `.editorconfig` |
| Nix | `nixfmt` 1.5.0, default width of 100 columns and indent of 2 | nothing; the settings and `.editorconfig` do not affect it |
| justfiles | `just --fmt` | nothing |
| Makefiles | spacing only, leaving what make reads unchanged | nothing |
| Dockerfiles | not formatted | |

Shell inside a Nix string, a recipe or a `RUN` instruction is left as written.
The justfile and Makefile rules are spelled out under
[Formatting](hosts.md#justfiles-makefiles-and-dockerfiles-natively).

## Shell scripts and range formatting

Formatting runs the same printer shfmt is built on, in-process, so no shfmt
binary is needed. Documents with syntax errors are not formatted, and range
formatting changes the lines that intersect the range, and what must move with
them to keep the document's meaning.

Lines move together when moving one alone would change what the document says.
A Nix `''` string strips the smallest indentation of its lines, so its lines
move as one. Every line of a justfile recipe needs one indentation, so a recipe
that is reindented moves as one. And where the formatter's output only lines up
with the source further on, as when one `fi` is taken for another, the edits up
to that point go together rather than lose or repeat text.

A selection inside the shell of a Nix string formats the Nix around that shell,
as formatting the whole file does. The shell itself is left as it is, in
`writeShellScript`, `runCommand`, phases and `home.file` alike, so the string
keeps its value and the edits are empty when the string is already in place.
Recipes of justfiles and Makefiles are not reformatted as shell either, and a
script embedded in any other file is not formatted at all.

## .editorconfig

Projects formatted with shfmt keep working unchanged: `.editorconfig` files are
read the way shfmt reads them (`indent_style`, `indent_size`, `binary_next_line`,
`switch_case_indent`, `space_redirects`, `keep_padding`, `function_next_line`,
`simplify`, `minify`, `ignore`, and the `[[shell]]`, `[[bash]]` and `[[zsh]]`
sections), and `shell_variant` selects the dialect. A `format.*` setting wins
over `.editorconfig`, which wins over the editor's tab size; an explicit
`dialect` setting wins over `shell_variant`. A malformed `.editorconfig` fails
formatting with an error naming the key, and changes to it apply as soon as the
editor reports them.
