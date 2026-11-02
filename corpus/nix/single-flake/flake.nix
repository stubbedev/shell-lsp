{
  description = "logship: rotates service logs and ships them to object storage";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = {
    self,
    nixpkgs,
    ...
  } @ inputs: let
    systems = ["x86_64-linux" "aarch64-linux"];
    forAllSystems = nixpkgs.lib.genAttrs systems;
    release = import ./release.nix;
    revision = inputs.nixpkgs.shortRev or "dirty";

    pkgsFor = system:
      import nixpkgs {
        inherit system;
        overlays = [self.overlays.default];
      };
  in {
    overlays.default = final: _prev: {
      logship = final.callPackage ./pkgs/logship.nix {inherit release;};
      logship-rotate = final.callPackage ./pkgs/rotate.nix {
        retentionDays = release.defaults.retentionDays;
      };
    };

    packages = forAllSystems (system: let
      pkgs = pkgsFor system;
    in {
      default = self.packages.${system}.logship;
      inherit (pkgs) logship logship-rotate;

      manual = pkgs.runCommand "logship-manual-${release.version}" {} ''
        mkdir -p "$out/share/doc/logship"
        {
          echo "logship ${release.version}"
          echo "built against nixpkgs ${revision}"
        } >"$out/share/doc/logship/VERSION"
      '';
    });

    apps = forAllSystems (system: {
      default = {
        type = "app";
        program = "${self.packages.${system}.logship}/bin/logship";
      };
    });

    checks = forAllSystems (system: let
      pkgs = pkgsFor system;
      logship = self.packages.${system}.logship;
    in {
      version = pkgs.runCommand "logship-version-check" {nativeBuildInputs = [logship];} ''
        reported="$(logship --version)"
        if [ "$reported" != "${release.version}" ]; then
          echo "logship reports $reported, expected ${release.version}" >&2
          exit 1
        fi
        touch "$out"
      '';
    });

    devShells = forAllSystems (system: let
      pkgs = pkgsFor system;
    in {
      default = pkgs.mkShell {
        inputsFrom = [self.packages.${system}.logship];
        packages = with pkgs; [shellcheck shfmt];

        LOGSHIP_SPOOL = ".spool";

        shellHook = ''
          mkdir -p "$LOGSHIP_SPOOL"
          for tool in shellcheck shfmt; do
            command -v "$tool" >/dev/null || echo "missing $tool" >&2
          done
          export PATH="$PWD/bin:$PATH"
        '';
      };
    });

    nixosModules.default = {
      config,
      lib,
      pkgs,
      ...
    }: let
      cfg = config.services.logship;
    in {
      options.services.logship = {
        enable = lib.mkEnableOption "the logship log shipper";
        bucket = lib.mkOption {
          type = lib.types.str;
          description = "Bucket that rotated logs are uploaded to.";
        };
      };

      config = lib.mkIf cfg.enable {
        systemd.services.logship = {
          wantedBy = ["multi-user.target"];
          serviceConfig.StateDirectory = "logship";
          script = ''
            exec ${self.packages.${pkgs.stdenv.hostPlatform.system}.logship}/bin/logship \
              --spool "$STATE_DIRECTORY" --bucket ${lib.escapeShellArg cfg.bucket}
          '';
        };
      };
    };
  };
}
