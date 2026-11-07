{ buildGo127Module }:
buildGo127Module {
  pname = "dockerfile-oracle";
  version = "0.34.0";
  src = ./.;
  vendorHash = "sha256-u9DqXyAhFksqPJ4WBQc6KAlpQ6WBhL2pCxvayAxC8Vg=";
  env.CGO_ENABLED = 0;
  meta.description = "BuildKit's Dockerfile parser and checks as a black box for the shell-lsp fixtures";
}
