_:
let
  editorOption =
    lib:
    lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Install and configure the editor.";
    };
in
{
  flake.modules.homeManager.features = { lib, ... }: {
    options.features.editor = editorOption lib;
  };

  flake.modules.nixos.features = { lib, ... }: {
    options.features.editor = editorOption lib;
    options.harbor.owner = lib.mkOption {
      type = lib.types.str;
      description = "The account that the home environment belongs to.";
    };
  };
}
