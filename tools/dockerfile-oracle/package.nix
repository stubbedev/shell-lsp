{ buildGoModule }:
buildGoModule {
  pname = "dockerfile-oracle";
  version = builtins.head (
    builtins.match ".*[[:space:]]github.com/moby/buildkit v([^[:space:]]+)[[:space:]].*" (
      builtins.readFile ./go.mod
    )
  );
  src = ./.;
  vendorHash = "sha256-3YFMuweD4QkeYKPSI/mzRnbWxYE1hvDRIJ33SMViyfw=";
  env.CGO_ENABLED = 0;
  meta.description = "BuildKit's Dockerfile parser and checks as a black box for the shell-lsp fixtures";
}
