{
  flake.modules.homeManager.shells = {
    config,
    lib,
    pkgs,
    ...
  }: let
    motdFile = pkgs.writeText "motd.json" (builtins.toJSON {greeting = "hello";});
  in {
    programs.zsh = {
      shellAliases.motd = "${lib.getExe pkgs.jq} -r .greeting ${motdFile}";
      initContent = lib.mkMerge [
        (lib.mkBefore ''
          setopt autocd
        '')
        ''
          print -r -- "$ZDOTDIR"
          ${lib.optionalString config.programs.zsh.enable ''
            bindkey -e
          ''}
        ''
      ];
    };

    programs.bash.bashrcExtra = ''
      shopt -s globstar
    '';

    programs.fish.interactiveShellInit = ''
      set -x FROM_FISH 1
    '';

    programs.git.settings.alias = {
      st = "status";
      top = "!git rev-parse --show-toplevel";
    };

    home.activation.motdCheck = lib.hm.dag.entryAfter ["writeBoundary"] ''
      ${lib.getExe pkgs.jq} --arg path ${lib.escapeShellArg motdFile} -n '$path'
    '';

    home.file."bin/greet.sh" = {
      executable = true;
      text = ''
        #!/usr/bin/env dash
        echo hello
      '';
    };

    xdg.configFile."tasks/justfile".text = ''
      build: test
        echo "$HOME"

      test:
        ${lib.getExe pkgs.jq} . config.json
    '';

    xdg.configFile."images/Dockerfile".text = ''
      FROM alpine
      RUN echo built
    '';

    systemd.user.services.motd.Service.ExecStart = "${lib.getExe pkgs.jq} . ${motdFile}";
  };
}
