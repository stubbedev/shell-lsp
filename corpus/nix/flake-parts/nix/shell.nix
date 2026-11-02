{
  perSystem =
    {
      config,
      pkgs,
      self',
      system,
      ...
    }:
    {
      devShells.default = pkgs.mkShell {
        inputsFrom = [ self'.packages.mirrorctl ];
        packages = config.project.syncTools ++ [ pkgs.shellcheck ];

        MIRROR_ROOT = ".mirror";

        shellHook = ''
          mkdir -p "$MIRROR_ROOT"
          printf 'mirrorctl development shell on %s\n' "${system}"
          if ! command -v rsync >/dev/null; then
            echo "rsync is missing from the shell" >&2
          fi
          export PATH="$PWD/bin:$PATH"
        '';
      };
    };
}
