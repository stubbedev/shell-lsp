{
  lib,
  config,
  inputs,
  withSystem,
  nixpkgsFor,
  ...
}:
let
  hostOptions = lib.types.submodule {
    options = {
      system = lib.mkOption {
        type = lib.types.str;
        description = "System double the host is built for.";
      };
      module = lib.mkOption {
        type = lib.types.deferredModule;
        description = "The top-level module of the host.";
      };
    };
  };
in
{
  options.configurations = {
    homeManager = lib.mkOption {
      type = lib.types.lazyAttrsOf hostOptions;
      default = { };
      description = "Standalone home environments.";
    };
    nixos = lib.mkOption {
      type = lib.types.lazyAttrsOf hostOptions;
      default = { };
      description = "Whole machines.";
    };
  };

  config.flake = {
    homeConfigurations = lib.mapAttrs (
      _name: host:
      withSystem host.system (
        { pkgs, ... }:
        inputs.home-manager.lib.homeManagerConfiguration {
          inherit pkgs;
          modules = [ host.module ];
        }
      )
    ) config.configurations.homeManager;

    nixosConfigurations = lib.mapAttrs (
      _name: host:
      inputs.nixpkgs.lib.nixosSystem {
        pkgs = nixpkgsFor host.system;
        modules = [ host.module ];
      }
    ) config.configurations.nixos;
  };
}
