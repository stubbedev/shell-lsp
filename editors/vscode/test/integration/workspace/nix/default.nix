{ lib, pkgs, x }:
let
  y = 1;
in
{
  cfg = {
    a = x;
    b = y;
    c = lib.id x;
    d = ./data.txt;
    e = ''
      one

        two
    '';
  };
  script = pkgs.writeShellScript "s" ''
          first

            second
  '';
  quoted = "one\ntwo\n";
  aliased = with lib; id x;
  options.port = lib.mkOption { type = lib.types.port; };
}
