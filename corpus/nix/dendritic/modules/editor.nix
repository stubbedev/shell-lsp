{config, ...}: let
  ownerName = config.harbor.lib.ownerName;
in {
  flake.modules.homeManager.editor = {
    config,
    lib,
    pkgs,
    ...
  }:
    lib.mkIf config.features.editor {
      home.packages = [pkgs.neovim];
      home.sessionVariables.EDITOR_STATE = config.harbor.paths.stateDir;
      home.sessionVariables.EDITOR_OWNER = ownerName;
    };

  flake.modules.nixos.editor = {
    config,
    lib,
    pkgs,
    ...
  }: {
    environment.systemPackages = lib.mkIf config.features.editor [pkgs.neovim];
  };
}
