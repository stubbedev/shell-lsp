{ lockFile }:
let
  lock = builtins.fromJSON (builtins.readFile lockFile);
  inputs = builtins.removeAttrs lock.nodes [ lock.root ];
  sources = builtins.mapAttrs (_: node: builtins.fetchTree node.locked) inputs;
  nixpkgs = sources.${lock.nodes.${lock.root}.inputs.nixpkgs};
in
builtins.deepSeq sources "nixpkgs=${nixpkgs.outPath}"
