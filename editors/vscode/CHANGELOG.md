# Changelog

## Unreleased

- Runs shell-lsp for Makefiles too, which lints the shell in their recipes.
- Runs shell-lsp for justfiles too, when an extension gives them the `just`
  language.
- Windows: finds `shell-lsp.exe` on `PATH` and downloads the Windows release
  binary.
- First release of the extension: runs shell-lsp for shell scripts and Bats
  files, finds the server through `shellLsp.path`, `PATH` or a checksum-verified
  download of the matching release binary, exposes the server's settings, and
  adds commands to restart the server, show its output, fix all problems and
  run Bats tests, with reference-count code lenses opening the references peek.
