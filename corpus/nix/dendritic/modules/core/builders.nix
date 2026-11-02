{
  config,
  lib,
  ...
}: {
  options.harbor.builders = lib.mkOption {
    type = lib.types.lazyAttrsOf lib.types.raw;
    default = {};
    description = "Functions of the package set that build things, each given the final set.";
  };

  config.flake.overlays.harbor = final: _prev: let
    harbor = config.harbor.lib // lib.mapAttrs (_: build: build toolArgs) config.harbor.builders;
    toolArgs = {
      inherit final harbor;
      inherit (final) lib;
    };
  in {
    inherit harbor;
  };
}
