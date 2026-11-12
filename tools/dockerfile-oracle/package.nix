{ buildGoModule }:
buildGoModule {
  pname = "dockerfile-oracle";
  version = "0.34.0";
  src = ./.;
  vendorHash = "sha256-8lpCTduxupoOyQtewZlnu/gy2t1AbnOVr3pUcAkfwVw=";
  env.CGO_ENABLED = 0;
  meta.description = "BuildKit's Dockerfile parser and checks as a black box for the shell-lsp fixtures";
}
