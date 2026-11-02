{ config, ... }: {
  perSystem =
    {
      pkgs,
      self',
      ...
    }:
    {
      packages.manual =
        pkgs.runCommand "mirrorctl-manual-${config.project.version}"
          {
            nativeBuildInputs = [ self'.packages.mirrorctl ];
          }
          ''
            mkdir -p "$out/share/doc/mirrorctl"
            mirrorctl --version >"$out/share/doc/mirrorctl/VERSION"
            for section in sync verify; do
              mirrorctl "$section" --help >"$out/share/doc/mirrorctl/$section.txt"
            done
          '';
    };
}
