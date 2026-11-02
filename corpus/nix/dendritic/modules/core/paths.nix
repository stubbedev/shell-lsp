_: {
  flake.modules.homeManager.paths = {
    config,
    lib,
    ...
  }: {
    options.harbor.paths.stateDir = lib.mkOption {
      type = lib.types.str;
      description = "Directory for state that survives a rebuild.";
    };

    config.harbor.paths.stateDir = lib.mkDefault "${config.home.homeDirectory}/.local/state";
  };
}
