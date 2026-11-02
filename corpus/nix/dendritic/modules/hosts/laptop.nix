{config, ...}: {
  configurations.homeManager.laptop = {
    system = "x86_64-linux";
    module = {
      imports = builtins.attrValues config.flake.modules.homeManager;
      features.editor = true;
      harbor.paths.stateDir = "/var/lib/harbor";
    };
  };
}
