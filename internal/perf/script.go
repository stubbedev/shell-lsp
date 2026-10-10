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
