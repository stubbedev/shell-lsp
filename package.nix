{
  lib,
  buildGo127Module,
  version ? "0-unstable",
}:
buildGo127Module {
  pname = "shell-lsp";
  inherit version;

  src = lib.cleanSource ./.;

  vendorHash = "sha256-sULOJKCnZ9nS/EVH8Q4MhH7U9zdrcruxCKI6HqP/1a4=";

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
