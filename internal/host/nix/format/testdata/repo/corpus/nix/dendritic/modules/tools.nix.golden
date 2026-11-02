{
  perSystem =
    {
      pkgs,
      self',
      system,
      ...
    }:
    {
      packages.greet = pkgs.callPackage ./_pkgs/greet.nix {
        greeting = "hello from ${system}";
      };

      devShells.default = pkgs.mkShell {
        inputsFrom = [ self'.packages.greet ];
      };
    };
}
