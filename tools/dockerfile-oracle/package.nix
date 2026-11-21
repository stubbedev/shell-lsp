{ buildGoModule }:
buildGoModule {
  pname = "dockerfile-oracle";
  version = "0.34.0";
  src = ./.;
  vendorHash = "sha256-Op1l96Dfb7zLmWWLeGrZ/zV7GZ1hVVZrY2G4sB7ZhQo=";
  env.CGO_ENABLED = 0;
  meta.description = "BuildKit's Dockerfile parser and checks as a black box for the shell-lsp fixtures";
}
