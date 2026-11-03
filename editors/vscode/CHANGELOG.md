# Changelog

## Unreleased

- Declares the `nix` language for `.nix` files, with comment toggling and bracket
  pairs, so Nix files start the server without any other extension installed.
- Nix: justfiles, Makefiles and Dockerfiles written from Nix get completion,
  signature help, Hadolint's `DL` rules, unused-name checks and include checks; a
  string named `x.nix` is read as Nix; shell tokens keep their color across a
  splice; hover explains findings in strings proven through a wrapper in another
  file, and such strings are also linted in closed files by workspace diagnostics.
- Nix: an undefined name offers "Change to" and "Add to the arguments of this
  function"; new refactorings rewrite a string between its two forms, turn an
  `inherit` into bindings, and quote or unquote an attribute. Flake input URLs
  link to their forge. Typing `;` formats the binding it ends where format on
  type is on.
- Nix: a refused rename says what would break and where. Definition with
  `nix.evaluate` reaches functions such as `pkgs.runCommand` and any `lib.x` the
  source cannot follow.
- `$name` in a jq or awk program in a plain shell script follows `--arg` and `-v`
  for definition, references and rename.
- The "▶ nix develop" lens on a dev shell opens a terminal and enters the shell,
  instead of building it in the background and exiting.
- Runs shell-lsp for Dockerfiles too, which checks them the way BuildKit
  does and with Hadolint's rules, `# hadolint` pragmas and `.hadolint.yaml`,
  navigates their stages and build arguments, and lints the shell in their
  `RUN` instructions.
- The shell status follows the cursor: in a justfile, Makefile or Nix file
  it shows the shell of the recipe or string the cursor is in.
- Formats Makefiles: spacing around operators and rule colons, trailing
  whitespace make ignores and runs of blank lines, leaving recipes and
  `define` bodies as they are.
- Runs justfile recipes that take parameters: their "▶ Run…" lens asks for
  each argument. Every recipe and Makefile target that runs without arguments
  is a `shell-lsp` task, and a task in `tasks.json` can pass arguments.
- Shows when `shell=` in a `.shellcheckrc` decides the shell, and explains in
  Choose the Shell to Lint As that a directive takes effect only once that
  setting is removed.
- Offers Fix All, Run Bats File and Choose the Shell to Lint As in the command
  palette for Makefiles, justfiles and Nix files too, as for shell scripts;
  Run Bats File on a document that is not a Bats file reports so instead of
  running it.
- Choose the Shell to Lint As in a Makefile, justfile or Nix file explains
  that the file decides the shell and where to change it, instead of offering
  an empty list of shells.
- When the server cannot say which shell a document is linted as, the status
  bar shows a warning with the reason, also logged to the Shell LSP output,
  instead of disappearing as if the server were still starting.
- Runs shell-lsp for Makefiles too, which lints the shell in their recipes.
- Runs shell-lsp for justfiles too, when an extension gives them the `just`
  language.
- Runs shell-lsp for Nix files too, when an extension gives them the `nix`
  language, which lints the shell in their phases, hooks and script writers.
- Windows: finds `shell-lsp.exe` on `PATH` and downloads the Windows release
  binary.
- First release of the extension: runs shell-lsp for shell scripts and Bats
  files, finds the server through `shellLsp.path`, `PATH` or a checksum-verified
  download of the matching release binary, exposes the server's settings, and
  adds commands to restart the server, show its output, fix all problems and
  run Bats tests, with reference-count code lenses opening the references peek.
