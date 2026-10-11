#!/bin/sh
set -o errexit

target=/tmp/out
if [ "$1" = a ] || [ "$1" = b ]; then
    echo ab
elif [ "$1" = c ]; then echo c
    echo more; fi

main() {
    if [ "$target" = /tmp/out ]; then echo default; elif [ "$target" = /srv ]; then
        echo srv
    fi
}

main "$@"
echo "$target"
