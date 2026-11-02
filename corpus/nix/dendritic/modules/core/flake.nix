{
  config,
  inputs,
  ...
}:
let
  nixpkgsFor =
    system:
    import inputs.nixpkgs {
      inherit system;
      config = config.harbor.lib.nixpkgsConfig;
      overlays = builtins.attrValues config.flake.overlays;
    };
in
{
  imports = [ inputs.flake-parts.flakeModules.modules ];

  _module.args.nixpkgsFor = nixpkgsFor;

  systems = [ "x86_64-linux" ];

  perSystem = { system, ... }: {
    _module.args.pkgs = nixpkgsFor system;
  };
}
