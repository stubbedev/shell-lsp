#!/bin/sh
set -eu
log() {
	printf '%s\n' "$*" >&2
}
readonly VERSION=1.0
export LANG=C prefix
count=0
while [ "$count" -lt 3 ]; do
	count=$((count + 1))
done
case "$1" in
	-h|--help) log usage ;;
	*) log "$@" ;;
esac
