package perf

import (
	"fmt"
	"strings"
)

func LargeScript(lines int) string {
	var sb strings.Builder
	sb.WriteString("#!/usr/bin/env bash\nset -euo pipefail\n")
	for i := 0; strings.Count(sb.String(), "\n") < lines; i++ {
		fmt.Fprintf(&sb, `# Handles step %[1]d.
step_%[1]d() {
	local name="$1" count=0
	local -a items=(alpha beta "gamma $name")
	for item in "${items[@]}"; do
		case "$item" in
			alpha) (( count += %[1]d )) ;;
			beta|gamma*) echo "${item^^}" >&2 ;;
			*) printf '%%s\n' "$item" ;;
		esac
	done
	if [[ -n "${GLOBAL_%[1]d:-}" && $count -gt 3 ]]; then
		cat <<EOF
step %[1]d: $name $count
EOF
	fi
	result_%[1]d=$(grep -c "$name" /dev/null || true)
}
GLOBAL_%[1]d=value
step_%[1]d "arg%[1]d"
`, i)
	}
	return sb.String()
}

func EmbeddedScript(lines int) string {
	var sb strings.Builder
	sb.WriteString("#!/usr/bin/env bash\n")
	for i := 0; strings.Count(sb.String(), "\n") < lines; i++ {
		fmt.Fprintf(&sb, `awk -F: '$3 >= %[1]d && $7 !~ /nologin/ { users[$1]++ } END { for (u in users) printf "%%s %%d\n", u, users[u] }' /etc/passwd
sed -n -e '/^start_%[1]d/,/^end/{' -e 's/[[:space:]]*$//;s/^#\(.*\)/\1/p' -e '}' notes.txt
jq -r --arg id "$id" '.items[] | select(.id == $id or .n > %[1]d) | "\(.name)\t\(.tags | join(","))"' items.json
total_%[1]d=$(awk '{ s += $NF } END { print s / NR }' sizes.txt | sed 's/\.[0-9]*$//')
`, i)
	}
	return sb.String()
}
