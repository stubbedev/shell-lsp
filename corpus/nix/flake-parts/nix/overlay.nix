{
  config,
  inputs,
  withSystem,
  ...
}: {
  flake = {
    lockedNixpkgs = inputs.nixpkgs.shortRev or "dirty";

    overlays.default = _final: prev: {
      mirrorctl = withSystem prev.stdenv.hostPlatform.system ({config, ...}: config.packages.mirrorctl);
    };

    nixosModules.default = {pkgs, ...}: {
      nixpkgs.overlays = [config.flake.overlays.default];
      environment.systemPackages = [pkgs.mirrorctl];
    };
  };
}
