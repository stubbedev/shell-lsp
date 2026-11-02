{ config, ... }: {
  perSystem =
    {
      pkgs,
      self',
      ...
    }:
    {
      checks = {
        inherit (self'.packages) mirrorctl manual;

        version =
          pkgs.runCommand "mirrorctl-version-check"
            {
              nativeBuildInputs = [ self'.packages.default ];
            }
            ''
              reported="$(mirrorctl --version)"
              if [ "$reported" != "${config.project.version}" ]; then
                echo "mirrorctl reports $reported, expected ${config.project.version}" >&2
                exit 1
              fi
              touch "$out"
            '';
      };
    };
}
