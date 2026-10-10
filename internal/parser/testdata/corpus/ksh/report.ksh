#!/bin/ksh
typeset -A totals
typeset -i lines=0

function add {
	typeset key=$1
	typeset -i amount=$2
	totals[$key]=$(( ${totals[$key]:-0} + amount ))
}

while IFS=, read -r key amount; do
	add "$key" "$amount"
	(( lines += 1 ))
done < "${1:-/dev/stdin}"

for key in "${!totals[@]}"; do
	print -r -- "$key ${totals[$key]}"
done
print -u2 "processed $lines lines"
