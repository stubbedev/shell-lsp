#!/usr/bin/env bash
set -euo pipefail

# Greets a user.
# Usage: greet NAME
greet() {
	local name="$1"
	local -a parts=(a b)
	declare -A seen=([x]=1)
	declare -g counter=0
	printf 'hello %s\n' "${name}" "${parts[@]}"
	(( counter += 1 ))
}

function cleanup {
	rm -f "$tmp" 2>/dev/null
}

tmp=$(mktemp)
export PATH="$HOME/bin:$PATH"
LC_ALL=C sort <"$tmp" >"$tmp.sorted"
for f in *.txt; do
	greet "$f"
done
cat <<-EOF
	tmp is $tmp
	EOF
cat <<'RAW'
$not_expanded
RAW
source ./lib.sh
. "$HOME/.bashrc"
