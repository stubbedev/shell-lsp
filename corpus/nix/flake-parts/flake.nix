{
  description = "mirrorctl: keeps package mirrors in sync, built as a flake-parts module tree";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts = {
      url = "github:hercules-ci/flake-parts";
      inputs.nixpkgs-lib.follows = "nixpkgs";
    };
  };

  outputs = inputs @ {flake-parts, ...}:
    flake-parts.lib.mkFlake {inherit inputs;} {
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];

      imports = [
        ./nix/project.nix
        ./nix/packages
        ./nix/checks.nix
        ./nix/shell.nix
        ./nix/overlay.nix
      ];
    };
}
