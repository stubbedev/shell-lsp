{
  lib,
  stdenv,
  fetchFromGitHub,
}:
stdenv.mkDerivation (finalAttrs: {
  pname = "ksh93";
  version = "1.0.10";

  src = fetchFromGitHub {
    owner = "ksh93";
    repo = "ksh";
    tag = "v${finalAttrs.version}";
    hash = "sha256-hrXW+PKQ6v0CCKiZ8E3HAEcrt9m4ZO5QfwFT9rCgbxc=";
  };

  buildPhase = ''
    runHook preBuild
    sh bin/package make CC="$CC" SHELL="${stdenv.shell}"
    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall
    sh bin/package install "$out"
    runHook postInstall
  '';

  meta = {
    description = "KornShell 93u+m, the maintained continuation of AT&T ksh93";
    homepage = "https://github.com/ksh93/ksh";
    license = lib.licenses.epl20;
    mainProgram = "ksh";
    platforms = lib.platforms.unix;
  };
})
