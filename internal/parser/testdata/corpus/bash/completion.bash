# bash completion for a hypothetical "mytool" command
_mytool() {
	local cur prev words cword
	cur="${COMP_WORDS[COMP_CWORD]}"
	prev="${COMP_WORDS[COMP_CWORD-1]}"
	local -a commands=(build test deploy clean)
	case "$prev" in
		--config|-c)
			mapfile -t COMPREPLY < <(compgen -f -- "$cur")
			return 0
			;;
		deploy)
			COMPREPLY=($(compgen -W "staging production" -- "$cur"))
			return 0
			;;
	esac
	if [[ "$cur" == -* ]]; then
		COMPREPLY=($(compgen -W "--config --verbose --help" -- "$cur"))
	else
		COMPREPLY=($(compgen -W "${commands[*]}" -- "$cur"))
	fi
}
complete -F _mytool mytool
