set shell := ["bash", "-euo", "pipefail", "-c"]
set export
set dotenv-load := true
set positional-arguments := false
set quiet
set tempdir := "/tmp"
set working-directory := 'build'
set ignore-comments

run arg:
    echo "$arg" "$1"
