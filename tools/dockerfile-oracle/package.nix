{ buildGoModule }:
buildGoModule {
  pname = "dockerfile-oracle";
  version = builtins.head (
    builtins.match ".*[[:space:]]github.com/moby/buildkit v([^[:space:]]+)[[:space:]].*" (
      builtins.readFile ./go.mod
    )
  );
  src = ./.;
  vendorHash = "sha256-xIIgu/y1D/S0d7bykxliqJcv4IWzh5s4qPYx1T0M0c4=";
  env.CGO_ENABLED = 0;
  meta.description = "BuildKit's Dockerfile parser and checks as a black box for the shell-lsp fixtures";
}
