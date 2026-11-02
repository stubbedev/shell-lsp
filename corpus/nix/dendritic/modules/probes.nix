{
  perSystem = {pkgs, ...}: {
    checks.owner-file = pkgs.harbor.probe {
      label = "owner-file";
      vars.expected = "/etc/harbor/owner";
      script = ''
        test -e "$expected" && touch "$out"
      '';
    };

    checks.quiet = pkgs.harbor.quiet {
      label = "quiet";
      script = ''
        touch "$out"
      '';
    };

    checks.plain = pkgs.harbor.describe {
      label = "not a script";
    };
  };
}
