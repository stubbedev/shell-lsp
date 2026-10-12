# Building and publishing the VS Code extension

The extension lives in `editors/vscode`. It is TypeScript bundled by esbuild
into one file, so the `.vsix` carries no `node_modules`.

## Building and testing

From `editors/vscode`, inside `nix develop` (which provides Node.js):

| Command | What it does |
| --- | --- |
| `npm ci` | Installs the locked dependencies |
| `npm run lint` | ESLint with the strict type-checked rules |
| `npm run typecheck` | `tsc --noEmit` over the sources, tests and build script |
| `npm test` | Unit tests (binary lookup, download, checksum verification, settings) |
| `npm run test:integration` | Builds `shell-lsp`, downloads VS Code, and drives it with the extension: diagnostics, hover, references lens, settings, fix-all, formatting with `.editorconfig` and Bats file detection |
| `npm run package` | Builds `shell-lsp-<version>.vsix` |

The integration test needs a display; on a headless Linux machine run it as
`xvfb-run -a npm run test:integration`, as CI does. The VS Code it downloads
is a generic Linux build that does not start on NixOS; there, point the test
at a VS Code from nixpkgs instead:

```sh
export SHELL_LSP_VSCODE_EXECUTABLE="$(NIXPKGS_ALLOW_UNFREE=1 nix build --impure --no-link --print-out-paths nixpkgs#vscode)/lib/vscode/code"
nix shell nixpkgs#xvfb-run -c xvfb-run -a npm run test:integration
```

CI runs all of the above in the `vscode` job and keeps the `.vsix` as the
`vscode-extension` artifact of every run.

## Releases

Pushing a `v*` tag runs `.github/workflows/release.yml`. After the server
binaries are released, the `vscode` job sets the extension version to the
tag (without the `v`), packages `shell-lsp-<version>.vsix`, and attaches it to
the GitHub release. The extension downloads the server from the release that
matches its own version, so the two always ship together. Before the release
is created, `scripts/checkReleaseAssets.ts` checks that `just dist` built and
summed exactly the binaries the extension downloads, one per platform it
supports; CI runs the same check on every push, so a renamed binary or a
platform added on one side only fails there instead of as a 404 for users.

It then publishes to the Visual Studio Marketplace if the repository secret
`VSCE_PAT` is set, and to Open VSX if `OVSX_PAT` is set. A missing secret
skips that registry with a notice in the run summary instead of failing.
Pre-release tags such as `v1.0.0-rc.1` get their `.vsix` on the GitHub
release but are not published to either registry, since the Marketplace only
takes plain `major.minor.patch` versions.

## One-time setup

### Visual Studio Marketplace

1. The extension is published as `stubbedev.shell-lsp`: `publisher` in
   `editors/vscode/package.json` is `stubbedev`. Create that publisher at
   <https://marketplace.visualstudio.com/manage/createpublisher>, signed in
   with the Microsoft account that should own it, using `stubbedev` as the
   publisher ID. If the ID is taken, choose another, and change `publisher`
   in `editors/vscode/package.json` and the extension ID in
   `docs/editors.md` and the README to match.
2. Create a personal access token in Azure DevOps
   (<https://dev.azure.com>, any organization; create one if you have none):
   User settings, Personal access tokens, New Token, with Organization set to
   "All accessible organizations" and the scope Marketplace: Manage (under
   "Show all scopes"). Pick an expiry date you will remember to renew.
3. Check it: `VSCE_PAT=<token> npx vsce verify-pat stubbedev` from
   `editors/vscode`.

### Open VSX

1. Sign in at <https://open-vsx.org> with GitHub, accept the publisher
   agreement in your profile settings, and create an access token under
   Settings, Access Tokens.
2. Create the namespace once:
   `OVSX_PAT=<token> npx ovsx create-namespace stubbedev` from `editors/vscode`.
   To get the verified badge, claim the namespace as described at
   <https://github.com/eclipse-openvsx/openvsx/wiki/Namespace-Access>.
3. Check it: `OVSX_PAT=<token> npx ovsx verify-pat stubbedev`.

### Repository secrets

In the GitHub repository, Settings, Secrets and variables, Actions, add:

| Secret | Value |
| --- | --- |
| `VSCE_PAT` | The Azure DevOps token for the Visual Studio Marketplace |
| `OVSX_PAT` | The Open VSX access token |

The tokens are only handed to two steps of the `vscode` job: the one that
checks which of them are set, and the one that publishes with each. Without
them, releases still attach the `.vsix` to the GitHub release.
