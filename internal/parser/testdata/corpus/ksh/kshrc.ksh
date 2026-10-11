#!/bin/ksh
# ~/.kshrc
[[ $- == *i* ]] || return 0

set -o emacs -o trackall -o noclobber
HISTFILE=$HOME/.ksh_history
HISTSIZE=5000
FCEDIT=${VISUAL:-vi}
typeset -x PAGER=less

function _git_branch {
	typeset branch
	branch=$(git symbolic-ref --short HEAD 2>/dev/null) || return
	print -rn -- " ($branch)"
}

function _prompt {
	typeset status=$? dir=${PWD/#$HOME/\~}
	(( status )) && print -rn -- "[$status] "
	print -rn -- "${USER}@${HOSTNAME%%.*}:${dir}$(_git_branch)"
}
PS1='$(_prompt)\$ '

typeset -A abbrev=(
	[gs]='git status'
	[gd]='git diff'
)
for name in "${!abbrev[@]}"; do
	alias "$name=${abbrev[$name]}"
done
alias ll='ls -l' la='ls -la'

function KEYBD_trap {
	case ${.sh.edchar} in
		$'\e[A') .sh.edchar=$'\020' ;;
		$'\e[B') .sh.edchar=$'\016' ;;
	esac
}
trap KEYBD_trap KEYBD

function cd {
	command cd "$@" || return
	print -r -- "${#DIRSTACK[@]}" >/dev/null
	set -A DIRSTACK -- "$PWD" "${DIRSTACK[@]:0:9}"
}

function mcd {
	mkdir -p -- "$1" && cd -- "$1"
}

function retry {
	typeset -i tries=$1
	shift
	until "$@"; do
		(( --tries > 0 )) || return 1
		sleep 1
	done
}

[[ -r ~/.kshrc.local ]] && . ~/.kshrc.local
true
