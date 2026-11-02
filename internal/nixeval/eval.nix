let
  failed = "«failed to evaluate»";
  maxTreeDepth = 32;
  maxRenderDepth = 3;
  maxRenderWidth = 32;

  forced = v: builtins.tryEval (builtins.deepSeq v v);

  attemptText = v: let
    r = forced v;
  in
    if !r.success
    then failed
    else if builtins.isString r.value
    then r.value
    else null;

  stringOrNull = v: let
    r = forced v;
  in
    if r.success && builtins.isString r.value
    then r.value
    else null;

  isLiteral = v: builtins.isAttrs v && builtins.elem (v._type or null) ["literalExpression" "literalMD" "mdDoc"];

  literalText = v:
    if isLiteral v
    then v.text
    else if builtins.isString v
    then v
    else null;

  renderName = n:
    if builtins.match "[a-zA-Z_][a-zA-Z0-9_'-]*" n != null
    then n
    else builtins.toJSON n;

  render = depth: v: let
    type = builtins.typeOf v;
    tooDeep = depth >= maxRenderDepth;
    next = render (depth + 1);
  in
    if builtins.elem type ["string" "int" "float" "bool" "null"]
    then builtins.toJSON v
    else if type == "path"
    then toString v
    else if type == "lambda"
    then "«function»"
    else if type == "list"
    then
      if v == []
      then "[ ]"
      else if tooDeep || builtins.length v > maxRenderWidth
      then "[ … ]"
      else "[ ${builtins.concatStringsSep " " (map next v)} ]"
    else if isLiteral v
    then v.text
    else if (v.type or null) == "derivation"
    then "«derivation»"
    else if v == {}
    then "{ }"
    else if tooDeep || builtins.length (builtins.attrNames v) > maxRenderWidth
    then "{ … }"
    else "{ ${builtins.concatStringsSep " " (map (n: "${renderName n} = ${next v.${n}};") (builtins.attrNames v))} }";

  defaultOf = opt:
    if opt ? defaultText
    then
      attemptText (
        if builtins.isString opt.defaultText
        then opt.defaultText
        else render 0 opt.defaultText
      )
    else if opt ? default
    then attemptText (render 0 opt.default)
    else null;

  exampleOf = opt:
    if opt ? example
    then attemptText (render 0 opt.example)
    else null;

  declarationsOf = opt: let
    r = forced (opt.declarationPositions or (map (file: {inherit file;}) (opt.declarations or [])));
  in
    if r.success
    then r.value
    else [];

  isOption = v: builtins.isAttrs v && (v._type or null) == "option";

  isHidden = opt: (opt.internal or false) || builtins.elem (opt.visible or true) [false "transparent"];

  subOptionsOf = opt:
    if builtins.elem (opt.visible or true) [true "transparent"]
    then opt.type.getSubOptions opt.loc
    else {};

  describe = opt: {
    inherit (opt) loc;
    type = attemptText (opt.type.description or null);
    description = attemptText (literalText (opt.description or null));
    default = defaultOf opt;
    example = exampleOf opt;
    declarations = declarationsOf opt;
  };

  walk = depth: path: tree: let
    names = builtins.attrNames tree;
  in
    if names == []
    then []
    else if depth >= maxTreeDepth
    then [{truncated = path;}]
    else
      builtins.concatMap (name: let
        v = tree.${name};
      in
        if isOption v
        then
          (
            if isHidden v
            then []
            else [(describe v)]
          )
          ++ walk (depth + 1) v.loc (subOptionsOf v)
        else if builtins.isAttrs v && (v.type or null) != "derivation"
        then walk (depth + 1) (path ++ [name]) v
        else [])
      names;

  optionsReport = tree: let
    entries = builtins.partition (entry: entry ? truncated) (walk 0 [] tree);
  in {
    options = entries.wrong;
    truncated = map (entry: entry.truncated) entries.right;
  };

  capturedOptions = "__shellLspFlakePartsOptions";

  interceptMkFlake = input:
    if input ? lib.mkFlake && input ? lib.evalFlakeModule
    then
      input
      // {
        lib =
          input.lib
          // {
            mkFlake = args: module: let
              evaluated = input.lib.evalFlakeModule args module;
            in
              evaluated.config.flake // {${capturedOptions} = evaluated.options;};
          };
      }
    else input;

  outputsWithFlakePartsOptions = flake:
    (import (flake.outPath + "/flake.nix")).outputs (builtins.mapAttrs (_: interceptMkFlake) flake.inputs // {self = flake;});

  usesFlakeParts = flake:
    builtins.any (input: input ? lib.mkFlake && input ? lib.evalFlakeModule) (builtins.attrValues flake.inputs)
    && outputsWithFlakePartsOptions flake ? ${capturedOptions};

  configurationNames = flake: output: builtins.attrNames (flake.${output} or {});

  optionTrees = flake: name: {
    nixos = flake.nixosConfigurations.${name}.options;
    home-manager = flake.homeConfigurations.${name}.options;
    flake-parts = (outputsWithFlakePartsOptions flake).${capturedOptions};
  };

  attrAt = root: path:
    builtins.foldl' (
      set: name:
        if builtins.isAttrs set
        then set.${name} or null
        else null
    )
    root
    path;

  packagesOf = {
    ref,
    input,
    system,
    ...
  }:
    (builtins.getFlake ref).inputs.${input}.legacyPackages.${system};
in {
  sources = {ref}: let
    flake = builtins.getFlake ref;
  in {
    nixos = configurationNames flake "nixosConfigurations";
    home-manager = configurationNames flake "homeConfigurations";
    flake-parts = usesFlakeParts flake;
  };

  options = {
    ref,
    kind,
    name,
  }:
    optionsReport (optionTrees (builtins.getFlake ref) name).${kind};

  base = {
    ref,
    input,
    system,
  }:
    optionsReport ((builtins.getFlake ref).inputs.${input}.lib.nixosSystem {
      inherit system;
      modules = [];
    }).options;

  attrs = args: let
    value = attrAt (packagesOf args) args.path;
  in
    if builtins.isAttrs value
    then builtins.attrNames value
    else [];

  package = args: let
    value = builtins.tryEval (attrAt (packagesOf args) args.path);
    isDerivation = value.success && builtins.isAttrs value.value && stringOrNull (value.value.type or null) == "derivation";
  in
    if !isDerivation
    then null
    else {
      name = stringOrNull (value.value.name or null);
      version = stringOrNull (value.value.version or null);
      description = stringOrNull (value.value.meta.description or null);
      position = stringOrNull (value.value.meta.position or null);
    };
}
