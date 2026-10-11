{
  lib,
  buildGo127Module,
  version ? "0-unstable",
}:
buildGo127Module {
  pname = "shell-lsp";
  inherit version;

  src = lib.cleanSource ./.;

  vendorHash = "sha256-5Ek77w2lFWehxLcQDOte4Gc4fiAzCdrTt6UGHY6JOlE=";

  env.CGO_ENABLED = 0;

  ldflags = ["-s" "-w" "-X main.version=${version}"];

  doCheck = true;

  meta = {
    description = "Language server for bash, zsh and POSIX sh";
    homepage = "https://github.com/stubbedev/shell-lsp";
    license = lib.licenses.mit;
    mainProgram = "shell-lsp";
    platforms = lib.platforms.unix;
  };
}
