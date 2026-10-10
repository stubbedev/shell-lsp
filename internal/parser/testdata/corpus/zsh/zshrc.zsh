#!/usr/bin/env zsh
autoload -Uz compinit && compinit
typeset -A opts
opts=(a 1 b 2)
setopt extended_glob
for f in **/*(.om[1,3]); do
	print -r -- ${(U)f:t}
done
diff =(sort a) =(sort b)
repeat 3 echo hi
function prompt_setup {
	local -a parts=( ${(s:/:)PWD} )
	PROMPT="%~ ${#parts} "
}
() { echo anonymous $1 } arg
