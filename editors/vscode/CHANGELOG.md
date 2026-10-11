# Changelog

## Unreleased

- First release of the extension: runs shell-lsp for shell scripts and Bats
  files, finds the server through `shellLsp.path`, `PATH` or a checksum-verified
  download of the matching release binary, exposes the server's settings, and
  adds commands to restart the server, show its output, fix all problems and
  run Bats tests, with reference-count code lenses opening the references peek.
