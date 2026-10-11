#!/bin/bash
# Exercises the refactorings on shapes they rewrite.
set -e

source ./lib.sh

mode=dev
label='a b'
count=3

usage() {
	cat <<USAGE
  Usage: $0 [mode]
USAGE
}

dispatch() {
	if [ "$1" = start ]; then up; elif [ "$1" = stop ]; then down; else usage; fi

	if [[ $mode == dev* || $mode == test ]]; then run dev
		run more
	elif [[ $mode = "prod" ]]
	then
		cat <<-EOF
		deploying
		EOF
		echo "multi
  line"; fi >log

	if test "$2" = a; then # first
		echo a; echo b &
	elif test "$2" = b; then x=$(
			printf '%s\n' "$label"
		)
	else echo "${x:-none}"; fi
}

function greet {
	local who=world
	echo "hi $who $count"
	(( count += 1 ))
	echo x
}

greet
dispatch "$@"
echo "$mode" "$label" $((count * 2))
