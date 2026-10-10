{
  lib,
  buildGo127Module,
  version ? "0-unstable",
}:
buildGo127Module {
  pname = "shell-lsp";
  inherit version;

  src = lib.cleanSource ./.;

  # Empty while the module has no dependencies. Once go.mod grows one, run
  # `nix build` and put the reported got: hash here.
  vendorHash = null;

  env.CGO_ENABLED = 0;

  # The test suite is pure Go and safe to run in the sandbox.
  doCheck = true;

  meta = {
    description = "Language server for bash, zsh and POSIX sh";
    homepage = "https://github.com/stubbedev/shell-lsp";
    license = lib.licenses.mit;
    mainProgram = "shell-lsp";
    platforms = lib.platforms.unix;
  };
}
