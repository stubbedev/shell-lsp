{
  config,
  nixpkgsFor,
  ...
}:
{
  flake.imagePackages = nixpkgsFor (builtins.head config.systems);
  flake.editorArguments = builtins.functionArgs config.flake.modules.nixos.editor;
}
