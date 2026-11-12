{
  perSystem =
    { pkgs, inputs', ... }:
    {
      _module.args.buildGoModule = pkgs.buildGoModule.override { go = inputs'.devtools.packages.go; };
    };
}
