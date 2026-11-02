{
  flake.nixosModules.mirrorctl =
    {
      config,
      lib,
      pkgs,
      ...
    }:
    let
      mirrorRoot = "/var/lib/mirrorctl";
    in
    {
      systemd.services.mirrorctl = {
        environment.MIRROR_ROOT = mirrorRoot;
        preStart = ''
          mkdir -p "$MIRROR_ROOT"
        '';
        script = ''
          ${lib.getExe pkgs.jq} --arg root "$MIRROR_ROOT" -n '$root'
        '';
        serviceConfig.ExecStartPost = [ "-${pkgs.coreutils}/bin/true" ];
      };

      system.activationScripts.mirrorctl = lib.stringAfter [ "users" ] ''
        mkdir -p ${lib.escapeShellArg mirrorRoot}
      '';

      environment.interactiveShellInit = ''
        export MIRROR_ROOT=${lib.escapeShellArg mirrorRoot}
      '';

      environment.etc."mirrorctl/hook.sh".text = ''
        echo "${config.networking.hostName}"
      '';

      boot.initrd.postMountCommands = ''
        echo mounted
      '';
    };
}
