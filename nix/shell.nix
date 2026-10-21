{
  perSystem = {
    lib,
    pkgs,
    self',
    ...
  }: {
    devShells.default = pkgs.mkShell {
      # The package's Go toolchain, pinned to the version go.mod declares:
      # the default `go` attribute lags behind, and with GOTOOLCHAIN=auto it
      # would silently download its own toolchain instead of failing. gopls
      # and golangci-lint are built with the same nixpkgs Go, so they parse
      # what the compiler accepts.
      inputsFrom = [self'.packages.default];

      packages = with pkgs;
        [
          # Development tools
          gopls # Go language server
          golangci-lint # Linter behind `just lint`, config in .golangci.yml
          delve # Go debugger
          just # Task runner, and the reference oracle for the justfile fixtures
          nodejs_24

          # Cross-checking the language server under development
          shellcheck # Reference oracle that labels the linter's test fixtures
          gnumake # Reference oracle for the Makefile fixtures
          (callPackage ../tools/dockerfile-oracle/package.nix {}) # BuildKit's Dockerfile frontend, the reference oracle for the Dockerfile fixtures
          hadolint # Reference oracle for the Hadolint-compatible Dockerfile checks
          yq-go # Reference YAML parser for the .hadolint.yaml reader
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

          # Development workflow
          git # Version control
          gh # GitHub CLI
        ]
        # bubblewrap and busybox are Linux-only, so the Darwin shell goes
        # without them.
        ++ lib.optionals stdenv.hostPlatform.isLinux [
          bubblewrap # Sandbox for running make on the corpus Makefiles
          # busybox links an applet for every tool it bundles, which
          # would shadow coreutils, grep and sed on PATH; only the busybox
          # binary itself is put there, so its shell is `busybox sh`.
          (runCommand "busybox-only" {} ''
            mkdir -p $out/bin
            ln -s ${busybox}/bin/busybox $out/bin/busybox
          '')
        ];

      shellHook = ''
        # Keep accidental cgo out, and keep the compiler the flake
        # provides: a module asking for a newer Go fails instead of
        # downloading one at first use.
        export CGO_ENABLED=0 GOTOOLCHAIN=local
      '';
    };

    formatter = pkgs.alejandra;
  };
}
