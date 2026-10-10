#!/bin/sh
set -eu

prefix=/usr/local
verbose=0

usage() {
	cat <<USAGE
Usage: $0 [-p prefix] [-v]
USAGE
}

log() {
	if [ "$verbose" -eq 1 ]; then
		printf '%s\n' "$*" >&2
	fi
}

while getopts 'p:vh' opt; do
	case "$opt" in
		p) prefix=$OPTARG ;;
		v) verbose=1 ;;
		h) usage; exit 0 ;;
		*) usage >&2; exit 2 ;;
	esac
done
shift $((OPTIND - 1))

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) echo "unsupported system" >&2; exit 1 ;;
esac

if ! command -v curl >/dev/null 2>&1; then
	echo "curl is required" >&2
	exit 1
fi

log "installing for $os into $prefix"
mkdir -p "$prefix/bin"
