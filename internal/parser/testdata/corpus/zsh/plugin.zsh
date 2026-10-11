0=${(%):-%N}
0=${${0:#$ZSH_ARGZERO}:-${(%):-%N}}
typeset -g SUGGEST_PLUGIN_DIR=${0:A:h}
typeset -gA SUGGEST_CONFIG=(
	highlight 'fg=8'
	strategy history
)
typeset -gi SUGGEST_MAX_SIZE=${SUGGEST_MAX_SIZE:-20}
fpath=( $SUGGEST_PLUGIN_DIR/functions(N/) $fpath )
autoload -Uz add-zsh-hook is-at-least

if ! is-at-least 5.3; then
	print -u2 -r -- "suggest: zsh 5.3 or newer is required"
	return 1
fi

_suggest_fetch() {
	emulate -L zsh
	setopt local_options extended_glob no_sh_word_split
	local prefix=$1 match
	local -a history_matches
	history_matches=( ${(@)${(@)history[(R)${(b)prefix}*]}[1,$SUGGEST_MAX_SIZE]} )
	match=${history_matches[1]:-}
	typeset -g SUGGEST_RESULT=$match
}

_suggest_highlight() {
	region_highlight=( ${region_highlight:#*suggest*} )
	(( $#POSTDISPLAY )) || return 0
	region_highlight+=( "$#BUFFER $(( $#BUFFER + $#POSTDISPLAY )) ${SUGGEST_CONFIG[highlight]} memo=suggest" )
}

_suggest_modify() {
	local original=$BUFFER
	zle .$WIDGET -- "$@"
	local -i ret=$?
	[[ $BUFFER == $original ]] && return ret
	POSTDISPLAY=
	if (( $#BUFFER > 0 && $#BUFFER <= SUGGEST_MAX_SIZE * 10 )); then
		_suggest_fetch $BUFFER
		[[ -n $SUGGEST_RESULT ]] && POSTDISPLAY=${SUGGEST_RESULT#$BUFFER}
	fi
	_suggest_highlight
	return ret
}

_suggest_accept() {
	BUFFER+=$POSTDISPLAY
	POSTDISPLAY=
	(( CURSOR = $#BUFFER ))
	_suggest_highlight
}

_suggest_bind() {
	local widget
	for widget in self-insert backward-delete-char vi-backward-delete-char; do
		[[ ${widgets[$widget]} == user:_suggest_* ]] && continue
		zle -N $widget _suggest_modify
	done
	zle -N suggest-accept _suggest_accept
	bindkey '^[[C' suggest-accept
	bindkey -M viins '^F' suggest-accept
}

_suggest_start() {
	add-zsh-hook -d precmd _suggest_start
	{
		_suggest_bind
	} always {
		unfunction _suggest_bind
	}
}

add-zsh-hook precmd _suggest_start

TRAPUSR1() {
	zle && zle reset-prompt
}

() {
	local file
	for file in $SUGGEST_PLUGIN_DIR/strategies/*.zsh(N-.on); do
		builtin source $file
	done
} "$@"

zstyle ':suggest:*' strategy ${(s: :)SUGGEST_CONFIG[strategy]}
zstyle -s ':suggest:*' strategy REPLY && print -r -- $REPLY >/dev/null
