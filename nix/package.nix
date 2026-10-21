{self, ...}: {
  perSystem = {
    lib,
    pkgs,
    self',
    ...
  }: let
    # Nix has no version to read from the source tree, so builds from a
    # checkout are stamped with the commit date.
    version = "0-unstable-${self.lastModifiedDate or "0"}";
  in {
    packages = {
      shell-lsp = pkgs.buildGo127Module {
        pname = "shell-lsp";
        inherit version;

        src = lib.fileset.toSource {
          root = ../.;
          fileset = lib.fileset.difference (lib.fileset.gitTracked ../.) (
            lib.fileset.unions [
              ../flake.nix
              ../flake.lock
              ../nix
            ]
          );
        };

        vendorHash = "sha256-TxEBzxsQdz/o4zjOoFggtfgrKDrpblWqOxs1a3io5Sk=";

        excludedPackages = ["third_party" "tools"];

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
      };
      default = self'.packages.shell-lsp;
    };

    apps = {
      shell-lsp = {
        type = "app";
        program = lib.getExe self'.packages.shell-lsp;
      };
      default = self'.apps.shell-lsp;
    };
  };
}
