#!/bin/ksh
function main {
	typeset -i n=0
	typeset -A map
	map[key]=value
	while (( n < 3 )); do
		print -r -- "${map[key]} $n"
		(( n++ ))
	done
}
main "$@"
