# Development

Every dev tool comes from the flake: the Go toolchain pinned to the version
go.mod declares, gopls, golangci-lint, delve, just, shellcheck, shfmt, git, gh,
Node.js for the VS Code extension, bats, bash-language-server and Neovim for the
Bats, `crosscheck` and `smoke` tests, dash, zsh, ksh93u+m, mksh and busybox, the
real shells the parser conformance tests compare with, and the other reference
tools: GNU Make, Hadolint, `dockerfile-oracle`, nixfmt, nixd, nil, statix,
deadnix and bubblewrap. Enter it with `direnv allow` on entry, or `nix develop` explicitly. Outside it, the
tests that compare with a reference tool skip.

| Recipe | What it does |
| --- | --- |
| `just check` | Every release gate in order: vet, lint, test, build |
| `just vet` | Static analysis with `go vet`, also over the forked parser |
| `just lint` | `golangci-lint run` against the strict config in `.golangci.yml` |
| `just lint-fix` | Apply the fixes golangci-lint can make on its own |
| `just test` | `go test ./...`, also over the forked parser |
| `just build` | Compile-check every package |
| `just fmt` | Format every Go source in place |
| `just smoke` | Drive the server from headless Neovim |
| `just crosscheck` | Compare with bash-language-server into `docs/crosscheck.md` |
| `just fixtures` | Label new lint fixtures with shellcheck, then check every fixture |
| `just lintcheck PATH...` | Per-code precision and recall of the linter against shellcheck over real scripts |
| `just corpus-fetch` | Fetch the pinned corpus of public shell repositories into the cache |
| `just corpus-check` | Compare the linter with shellcheck, the parser with the real shells, and the justfile, Makefile and Dockerfile analysis with just, make, BuildKit and Hadolint over the corpus, and lint the recipes, against the committed baselines |
| `just corpus-shellcheck` | Only the shellcheck comparison, against `corpus/shellcheck.md` |
| `just corpus-shells` | Only the comparison with bash, dash, zsh, ksh, mksh and BusyBox `-n`, against `corpus/shells.txt` |
| `just corpus-hosts` | Only the analysis of real justfiles, Makefiles and Dockerfiles, checked with just, make, BuildKit and Hadolint, against `corpus/hosts.txt` |
| `just corpus-recipes` | Only the shell findings in the recipes of those justfiles and Makefiles, against `corpus/recipes.md` |
| `just corpus-nix-lint` | Compare the Nix checks with statix and deadnix, against `corpus/nix-lint.md`; `just corpus-nix-lint-baseline` rewrites it |
| `just nix-parity [nixd\|nil]` | Compare with a Nix language server over the fixtures of `corpus/nix`; [the other `nix-parity` recipes](corpus.md#nix-server-parity) cover baselines and large flakes |
| `just corpus-baseline` | Rewrite the corpus baselines after a deliberate change |
| `just fuzz-targets` | List every fuzz target with its package |
| `just fuzz PACKAGE TARGET [TIME]` | Fuzz one target, one minute by default; failing inputs land in the package's `testdata/fuzz` |
| `just bench-compare [BASE] [COUNT]` | Compare every benchmark between `origin/main` (or BASE) and the working tree with benchstat, alternating six runs (or COUNT) of each |
| `just dist` | Build static release binaries for Linux, macOS and Windows into `dist/` |
| `just sync-flake` | Recompute `vendorHash` in `nix/package.nix` from go.mod and go.sum |
| `just spdx` | Rewrite the license identifiers the Dockerfile label checks accept from the pinned SPDX list |
| `just release LEVEL` | Bump the pre-commit rev, sync `vendorHash`, run the gates, push, wait for CI and tag; `just release-preview` shows the versions it would cut |

The corpus checks, the manifest and how to update the baselines are
described in [corpus.md](corpus.md).

With Nix:

- `nix build` builds the package, stamped with the commit date, and runs the
  test suite; update `vendorHash` in `nix/package.nix` whenever go.mod
  dependencies change
- `nix run` runs it
- `nix fmt` formats the nix files with nixfmt

Building and publishing the VS Code extension is described in
[vscode-publishing.md](vscode-publishing.md).

## Documentation site

The README and `docs/` are published as a site by the `pages` workflow. It builds with VitePress, with the
repository root as its source so that the README and `CLEAN_ROOM.md` are pages and links between the Markdown
files work unchanged:

```sh
npm --prefix docs ci --ignore-scripts
npm --prefix docs run dev
npm --prefix docs run build
```

[docs/index.md](index.md) holds the landing page's text as front matter. No other Markdown file has front matter,
since generated files, embedded rules and release notes are read as they are. Routes, titles and the sidebar live
in `sections` in [docs/.vitepress/config.ts](.vitepress/config.ts), and the build fails on a page in `docs/` that
has no entry there. [docs/.vitepress/markdown.ts](.vitepress/markdown.ts) keeps Markdown literal, so `{{x}}` and
`<x>` are text and not Vue, and points links to files outside the site, such as `docs/releases`, at GitHub.

VitePress requires `@iconify-json/simple-icons`, whose CC0-1.0 licence is not on the dependency-review allowlist,
only to build social link icons from names. The site passes its GitHub icon as an SVG, so
`docs/.vitepress/no-social-icons` takes the place of that package. Mermaid is loaded from jsDelivr by the one page
component that needs it, at the exact version in `Mermaid.vue`, because its npm tree brings in EPL-2.0 code.

The images in `docs/assets/screenshots` come from the VS Code extension running in VSCodium under xvfb, on sample
files in `/tmp/demo`, driven by a script. They are not regenerated by any recipe, so retake one when what it
shows changes.
