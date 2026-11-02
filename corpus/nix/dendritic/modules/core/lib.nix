{
  config,
  self,
  lib,
  ...
}: {
  options.harbor.lib = lib.mkOption {
    type = lib.types.lazyAttrsOf lib.types.raw;
    default = {};
    description = "Plain data and functions shared by every class of module.";
  };

  config.flake.lib = config.harbor.lib;

  config.harbor.lib = {
    source = self;
  };
}
