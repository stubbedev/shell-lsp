{
  lib,
  stdenvNoCC,
  makeWrapper,
  version,
  syncTools,
}:
stdenvNoCC.mkDerivation {
  pname = "mirrorctl";
  inherit version;

  dontUnpack = true;

  nativeBuildInputs = [ makeWrapper ];

  buildPhase = ''
    runHook preBuild

    cat >mirrorctl <<SCRIPT
    #!$SHELL
    set -eu
    case "\''${1:-}" in
      --version) echo "$version" ;;
      sync) shift; exec rsync --archive --delete "\$@" ;;
      verify) shift; exec sha256sum --check "\$@" ;;
      *) echo "usage: mirrorctl {sync|verify|--version}" >&2; exit 2 ;;
    esac
    SCRIPT

    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall

    install -Dm755 mirrorctl "$out/bin/mirrorctl"
    wrapProgram "$out/bin/mirrorctl" --prefix PATH : "${lib.makeBinPath syncTools}"

    runHook postInstall
  '';

  meta = {
    description = "Keeps package mirrors in sync";
    license = lib.licenses.mit;
    mainProgram = "mirrorctl";
    platforms = lib.platforms.linux;
  };
}
