{
  description = "shell-lsp - language server for bash, zsh and POSIX sh";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
  }:
    flake-utils.lib.eachDefaultSystem (
      system: let
        pkgs = nixpkgs.legacyPackages.${system};
        # Nix has no version to read from the source tree, so builds from a
        # checkout are stamped with the commit date.
        version = "0-unstable-${self.lastModifiedDate or "0"}";
      in {
        packages = rec {
          shell-lsp = pkgs.callPackage ./package.nix {inherit version;};
          default = shell-lsp;
        };

        apps = rec {
          shell-lsp = flake-utils.lib.mkApp {drv = self.packages.${system}.shell-lsp;};
          default = shell-lsp;
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            # Go toolchain. Pinned to the version go.mod declares: the
            # default `go` attribute lags behind, and with GOTOOLCHAIN=auto
            # it would silently download its own toolchain instead of
            # failing. gopls and golangci-lint are built with the same
            # nixpkgs Go, so they parse what the compiler accepts.
            go_1_27

            # Development tools
            gopls # Go language server
            golangci-lint # Linter behind `just lint`, config in .golangci.yml
            delve # Go debugger
            just # Task runner
            nodejs_24

            # Cross-checking the language server under development
            shellcheck # Reference oracle that labels the linter's test fixtures
            shfmt # Reference formatter for shell scripts
            bats # Real test runner behind the Bats integration test
            bash-language-server # Comparison server behind `just crosscheck`
            neovim # Headless editor smoke test behind `just smoke`

            # Real shells for the conformance tests and `just corpus-shells`,
            # which check that the parser accepts and rejects what each
            # shell does. The forked parser's TestParseConfirm wants
            # ksh93u+m, which nixpkgs lacks: its ksh is the abandoned ksh2020.
            dash
            zsh
            (callPackage ./ksh93.nix {})
            mksh
            # busybox links an applet for every tool it bundles, which
            # would shadow coreutils, grep and sed on PATH; only the busybox
            # binary itself is put there, so its shell is `busybox sh`.
            (runCommand "busybox-only" {} ''
              mkdir -p $out/bin
              ln -s ${busybox}/bin/busybox $out/bin/busybox
            '')

            # Development workflow
            git # Version control
            gh # GitHub CLI
          ];

          shellHook = ''
            # Keep accidental cgo out, and keep the compiler the flake
            # provides: a module asking for a newer Go fails instead of
            # downloading one at first use.
            export CGO_ENABLED=0 GOTOOLCHAIN=local
          '';
        };

        formatter = pkgs.alejandra;
      }
    );
}
