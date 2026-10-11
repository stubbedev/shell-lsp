{
  lib,
  buildGo127Module,
  version ? "0-unstable",
}:
buildGo127Module {
  pname = "shell-lsp";
  inherit version;

  src = lib.cleanSource ./.;

  vendorHash = "sha256-TxEBzxsQdz/o4zjOoFggtfgrKDrpblWqOxs1a3io5Sk=";

  excludedPackages = ["third_party"];

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
