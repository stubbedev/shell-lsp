{
  lib,
  flake-parts-lib,
  ...
}: let
  inherit (flake-parts-lib) mkPerSystemOption;
in {
  options = {
    project.version = lib.mkOption {
      type = lib.types.str;
      description = "Version stamped on every package of the project.";
    };

    perSystem = mkPerSystemOption ({pkgs, ...}: {
      options.project.syncTools = lib.mkOption {
        type = lib.types.listOf lib.types.package;
        description = "Programs the sync script needs on PATH.";
        default = with pkgs; [coreutils rsync];
      };
    });
  };

  config.project.version = "0.9.1";
}
