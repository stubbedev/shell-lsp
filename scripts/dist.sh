#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
	echo "usage: scripts/dist.sh VERSION" >&2
	exit 2
fi
version="$1"
targets=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64)

rm -rf dist
mkdir -p dist
for target in "${targets[@]}"; do
	goos="${target%/*}"
	goarch="${target#*/}"
	suffix=""
	if [[ "$goos" == windows ]]; then
		suffix=".exe"
	fi
	CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "-s -w -X main.version=${version#v}" \
		-o "dist/shell-lsp-${version}-${goos}-${goarch}${suffix}" .
done
(cd dist && sha256sum shell-lsp-* >SHA256SUMS)
