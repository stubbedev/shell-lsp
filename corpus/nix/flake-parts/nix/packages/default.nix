{ config, ... }:
let
  projectVersion = config.project.version;
in
{
  imports = [
    ./manual.nix
  ];

  perSystem =
    {
      config,
      pkgs,
      system,
      ...
    }:
    {
      packages = {
        default = config.packages.mirrorctl;

        mirrorctl = pkgs.callPackage ../../pkgs/mirrorctl.nix {
          version = projectVersion;
          syncTools = config.project.syncTools;
        };

        platform-note = pkgs.writeTextFile {
          name = "mirrorctl-platform";
          text = "mirrorctl ${projectVersion} for ${system}\n";
          destination = "/share/mirrorctl/platform";
        };
      };
    };
}
