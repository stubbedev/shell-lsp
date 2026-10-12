# shell-lsp dev tasks.

export CGO_ENABLED := "0"

# List all recipes with their descriptions.
default:
    @just --list

# Run every release gate in order: vet, lint, test, build.
check: vet lint test build

# Static analysis of every package with go vet, the forked parser included.
vet:
    go vet ./...

# Lint every package; settings live in .golangci.yml.
lint:
    golangci-lint run

# Apply the fixes golangci-lint can make on its own.
lint-fix:
    golangci-lint run --fix

# Run the test suite for every package, the forked parser included.
test:
    go test ./...

# Compile-check every package; the output is discarded.
build:
    go build -o /dev/null ./...

# Format every Go source in place with gofmt.
fmt:
    gofmt -w .

# Compare diagnostics and completion recall with bash-language-server; rewrites docs/crosscheck.md.
crosscheck:
    #!/usr/bin/env bash
    set -euo pipefail
    bin="$(mktemp -d)"
    trap 'rm -rf "$bin"' EXIT
    go build -o "$bin/shell-lsp" .
    go run ./cmd/crosscheck -ours "$bin/shell-lsp"

# Label new lint fixtures with shellcheck's findings, then check every fixture against it.
fixtures:
    go test ./internal/lint -run Shellcheck -update
    go test ./internal/lint

# Compare the linter with shellcheck, code by code, over every shell script under the given paths.
lintcheck +paths:
    go run ./cmd/lintcheck compare {{ paths }}

# Fetch the repositories pinned in corpus/repos.txt into the user cache directory.
corpus-fetch:
    go run ./cmd/lintcheck fetch

# Compare the linter with shellcheck over the corpus; fails when corpus/shellcheck.md no longer matches.
corpus-shellcheck: corpus-fetch
    go run ./cmd/lintcheck shellcheck

# Compare the parser with the real shells' -n over the corpus; fails on differences corpus/shells.txt lacks.
corpus-shells: corpus-fetch
    go run ./cmd/lintcheck shells

# Run both corpus comparisons.
corpus-check: corpus-shellcheck corpus-shells

# Rewrite corpus/shellcheck.md and corpus/shells.txt from the corpus; classify new shells entries by hand.
corpus-baseline: corpus-fetch
    go run ./cmd/lintcheck shellcheck -update
    go run ./cmd/lintcheck shells -update

# Print every fuzz target as "package target", one per line.
fuzz-targets:
    #!/usr/bin/env bash
    set -euo pipefail
    go test -list '^Fuzz' ./... | awk '/^Fuzz/ { names[++n] = $1; next } /^ok/ { for (i = 1; i <= n; i++) print $2, names[i]; n = 0 }'

# Fuzz one target of a package for a while, as in `just fuzz ./internal/parser FuzzParse 5m`; failing inputs land in the package's testdata/fuzz.
fuzz package target time="1m":
    go test -run '^$' -fuzz '^{{ target }}$' -fuzztime '{{ time }}' '{{ package }}'

# Run every benchmark at a base revision and in the working tree, alternating runs to even out noise, and compare them with benchstat.
bench-compare base="origin/main" count="6":
    #!/usr/bin/env bash
    set -euo pipefail
    dir="$(mktemp -d)"
    trap 'rm -rf "$dir"' EXIT
    git worktree add --quiet --detach "$dir/base" "{{ base }}"
    trap 'git worktree remove --force "$dir/base"; rm -rf "$dir"' EXIT
    bench=(go test -run '^$' -bench . -benchmem -count 1 -p 1 ./...)
    for _ in $(seq "{{ count }}"); do
        (cd "$dir/base" && "${bench[@]}") >>"$dir/base.txt"
        "${bench[@]}" >>"$dir/head.txt"
    done
    go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260929162123-406019bb8b68 base="$dir/base.txt" head="$dir/head.txt"

# Drive the server from headless Neovim: diagnostics, hover, definition, completion.
smoke:
    #!/usr/bin/env bash
    set -euo pipefail
    bin="$(mktemp -d)"
    trap 'rm -rf "$bin"' EXIT
    go build -o "$bin/shell-lsp" .
    nvim --headless -l scripts/nvim-smoke.lua "$bin/shell-lsp"

# Build release binaries for every supported platform into dist/, stamped with the git version.
dist version=`git describe --tags --always --dirty`:
    #!/usr/bin/env bash
    set -euo pipefail
    version="{{ version }}"
    rm -rf dist
    mkdir -p dist
    for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
        goos="${target%/*}"
        goarch="${target#*/}"
        suffix=""
        if [[ "$goos" == windows ]]; then
            suffix=".exe"
        fi
        GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "-s -w -X main.version=${version#v}" \
            -o "dist/shell-lsp-${version}-${goos}-${goarch}${suffix}" .
    done
    (cd dist && sha256sum shell-lsp-* >SHA256SUMS)

# Recompute vendorHash in package.nix from go.mod and go.sum; run after any dependency change.
sync-flake:
    #!/usr/bin/env bash
    set -euo pipefail
    fake="sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
    cur="$(grep -oP 'vendorHash = "\K[^"]+' package.nix)"
    sed -i "s#vendorHash = \"${cur}\"#vendorHash = \"${fake}\"#" package.nix
    out="$(nix build .#default --no-link 2>&1 || true)"
    got="$(printf '%s' "$out" | grep -oP 'got:\s+\K(sha256-\S+)' | head -1 || true)"
    sed -i "s#vendorHash = \"${fake}\"#vendorHash = \"${got:-$cur}\"#" package.nix
    err="$(mktemp)"
    trap 'rm -f "$err"' EXIT
    if ! nix build .#default --no-link >/dev/null 2>"$err"; then
        echo "nix build fails for a reason other than a stale vendorHash:" >&2
        tail -20 "$err" >&2
        exit 1
    fi
    echo "vendorHash = ${got:-$cur}"

# Print the current version or the one a release level would cut, from the latest v* tag.
_next-version level:
    #!/usr/bin/env bash
    set -euo pipefail
    current="$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*')"
    current="${current#v}"
    IFS=. read -r major minor patch <<<"$current"
    level="{{ level }}"
    case "$level" in
    current) echo "$current" ;;
    patch) echo "$major.$minor.$((patch + 1))" ;;
    minor) echo "$major.$((minor + 1)).0" ;;
    major) echo "$((major + 1)).0.0" ;;
    *)
        echo "unknown release level: $level (want patch, minor or major)" >&2
        exit 2
        ;;
    esac

# Show the version each release level would cut, from the latest v* tag.
release-preview:
    @echo "current: $(just _next-version current)"
    @echo "patch:   $(just _next-version patch)"
    @echo "minor:   $(just _next-version minor)"
    @echo "major:   $(just _next-version major)"

release-patch: (release "patch")
release-minor: (release "minor")
release-major: (release "major")

# Bump the pre-commit rev, sync vendorHash, run the gates, push, wait for CI, then tag; needs docs/releases/vX.Y.Z.md committed.
release level:
    #!/usr/bin/env bash
    set -euo pipefail
    current="$(just _next-version current)"
    next="$(just _next-version "{{ level }}")"
    tag="v$next"

    branch="$(git rev-parse --abbrev-ref HEAD)"
    if [[ "$branch" != main ]]; then
        echo "releases are cut from main, not $branch" >&2
        exit 1
    fi
    if ! git diff --quiet || ! git diff --cached --quiet; then
        echo "working tree is dirty; commit or stash first" >&2
        exit 1
    fi
    notes="docs/releases/$tag.md"
    if ! git ls-files --error-unmatch "$notes" >/dev/null 2>&1; then
        echo "write and commit the release notes in $notes first" >&2
        exit 1
    fi

    git fetch --quiet origin main
    if ! git merge-base --is-ancestor origin/main HEAD; then
        echo "main is behind origin/main; pull first" >&2
        exit 1
    fi

    failures="$(gh run list --branch main --limit 200 --json headSha,workflowName,conclusion \
        --jq '.[] | select(.conclusion == "failure" or .conclusion == "timed_out" or .conclusion == "startup_failure") | "\(.headSha) \(.workflowName) \(.conclusion)"')"
    unreleased="$(git rev-list "v$current..HEAD")"
    failed=""
    if [[ -n "$failures" && -n "$unreleased" ]]; then
        failed="$(grep -F -f <(printf '%s\n' "$unreleased") <<<"$failures" || [[ $? -eq 1 ]])"
    fi
    if [[ -n "$failed" ]]; then
        echo "CI failed on commits since v$current; investigate before releasing:" >&2
        echo "$failed" >&2
        exit 1
    fi

    echo "releasing v$current -> $tag"
    if ! grep -q "^    rev: v$current\$" docs/cli.md; then
        echo "docs/cli.md has no pre-commit 'rev: v$current' line to bump" >&2
        exit 1
    fi
    sed -i "s/^    rev: v$current\$/    rev: $tag/" docs/cli.md
    just sync-flake
    just check
    git add docs/cli.md package.nix
    git commit -m "Point pre-commit at $tag"
    git push origin HEAD:main

    sha="$(git rev-parse HEAD)"
    runs=()
    for _ in $(seq 60); do
        mapfile -t runs < <(gh run list --commit "$sha" --json databaseId --jq '.[].databaseId')
        if [[ ${#runs[@]} -gt 0 ]]; then
            break
        fi
        sleep 5
    done
    if [[ ${#runs[@]} -eq 0 ]]; then
        echo "no CI run started for $sha within 5 minutes; tag $tag by hand once CI passes" >&2
        exit 1
    fi
    watched=()
    while true; do
        mapfile -t runs < <(gh run list --commit "$sha" --json databaseId --jq '.[].databaseId')
        pending=()
        for run in "${runs[@]}"; do
            if [[ " ${watched[*]} " != *" $run "* ]]; then
                pending+=("$run")
            fi
        done
        if [[ ${#pending[@]} -eq 0 ]]; then
            break
        fi
        for run in "${pending[@]}"; do
            if ! gh run watch "$run" --exit-status >/dev/null; then
                echo "CI run $run failed for $sha; not tagging $tag" >&2
                exit 1
            fi
            watched+=("$run")
        done
    done

    git tag -a "$tag" -m "shell-lsp $next"
    git push origin "$tag"
    echo "released $tag; the release workflow publishes it"
