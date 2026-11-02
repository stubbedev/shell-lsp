{
  lib,
  stdenvNoCC,
  makeWrapper,
  coreutils,
  curl,
  zstd,
  release,
}:
stdenvNoCC.mkDerivation (finalAttrs: {
  pname = "logship";
  inherit (release) version;

  dontUnpack = true;

  nativeBuildInputs = [makeWrapper];

  runtimePath = lib.makeBinPath [coreutils curl zstd];

  buildPhase = ''
    runHook preBuild

    cat >logship <<SCRIPT
    #!$SHELL
    set -eu
    case "\''${1:-}" in
      --version) echo "$version" ;;
      *) exec curl --fail --upload-file "\$2" "\$4" ;;
    esac
    SCRIPT

    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall

    install -Dm755 logship "$out/bin/logship"
    wrapProgram "$out/bin/logship" --prefix PATH : "$runtimePath"

    runHook postInstall
  '';

  doInstallCheck = true;
  installCheckPhase = ''
    [ "$("$out/bin/logship" --version)" = "${finalAttrs.version}" ]
  '';

  meta = {
    description = "Ships rotated service logs to object storage";
    license = lib.licenses.mit;
    mainProgram = "logship";
    platforms = lib.platforms.linux;
  };
})
