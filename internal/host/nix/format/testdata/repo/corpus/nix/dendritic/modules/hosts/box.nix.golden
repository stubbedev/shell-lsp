{
  config,
  inputs,
  ...
}:
let
  nixosModules = config.flake.modules.nixos;
  homeModules = config.flake.modules.homeManager;
in
{
  configurations.nixos.box = {
    system = "x86_64-linux";
    module = { config, ... }: {
      imports = builtins.attrValues nixosModules ++ [ inputs.home-manager.nixosModules.home-manager ];

      networking.hostName = "box";
      features.editor = true;
      harbor.owner = "mara";

      home-manager.users.${config.harbor.owner} = {
        imports = builtins.attrValues homeModules;
        features.editor = true;
        home.stateVersion = "26.05";
      };
    };
  };
}
