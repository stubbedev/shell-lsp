# ~/.zshrc
[[ -o interactive ]] || return

typeset -U path fpath cdpath
path=(
	~/.local/bin(N/)
	~/go/bin(N/)
	/opt/homebrew/bin(N/)
	$path
)
cdpath=( . ~ ~/src(N/) )
export EDITOR=${commands[nvim]:-${commands[vim]:-vi}}
export LESS='-R -F -i'

HISTFILE=${XDG_STATE_HOME:-$HOME/.local/state}/zsh/history
HISTSIZE=100000
SAVEHIST=$HISTSIZE
[[ -d ${HISTFILE:h} ]] || mkdir -p ${HISTFILE:h}

setopt auto_cd auto_pushd pushd_ignore_dups pushd_silent
setopt extended_history hist_ignore_all_dups hist_reduce_blanks share_history
setopt extended_glob glob_dots no_beep interactive_comments
unsetopt flow_control

autoload -Uz compinit colors edit-command-line zmv
zmodload zsh/complist zsh/datetime
zmodload -F zsh/stat b:zstat
() {
	setopt local_options extended_glob
	local dump=${ZDOTDIR:-$HOME}/.zcompdump
	if [[ -n $dump(#qN.mh+24) ]]; then
		compinit -i -d $dump
	else
		compinit -C -d $dump
	fi
}
colors

zstyle ':completion:*' menu select
zstyle ':completion:*' matcher-list 'm:{a-zA-Z}={A-Za-z}' 'r:|[._-]=* r:|=*'
zstyle ':completion:*' list-colors ${(s.:.)LS_COLORS}
zstyle ':completion:*:descriptions' format '%F{yellow}-- %d --%f'
zstyle ':completion:*:*:kill:*:processes' command 'ps -u $USER -o pid,%cpu,tty,cputime,cmd'

bindkey -e
zle -N edit-command-line
bindkey '^X^E' edit-command-line
bindkey -M menuselect '^[[Z' reverse-menu-complete
typeset -A key=(
	Home "${terminfo[khome]}"
	End "${terminfo[kend]}"
)
[[ -n ${key[Home]} ]] && bindkey -- ${key[Home]} beginning-of-line
[[ -n ${key[End]} ]] && bindkey -- ${key[End]} end-of-line

alias ls='ls --color=auto' ll='ls -lh' la='ls -lAh'
alias -g L='| less' G='| grep -i' NE='2>/dev/null'
alias -s {md,txt}=$EDITOR
hash -d src=~/src
hash -d conf=${XDG_CONFIG_HOME:-~/.config}

mkcd() { mkdir -p -- $1 && cd -- $1 }
up() {
	local -i n=${1:-1}
	cd ${(j:/:)${(s::)${(l:n::.:)}//./..}}
}
extract() {
	local archive
	for archive in "$@"; do
		case ${archive:l} in
			(*.tar.gz|*.tgz) tar xzf $archive ;;
			(*.tar.xz) tar xJf $archive ;;
			(*.zip) unzip -q $archive ;;
			(*) print -u2 -r -- "extract: unknown archive: $archive"; return 1 ;;
		esac
	done
}
largest() {
	print -rl -- **/*(.DOL[1,${1:-10}])
}
since() {
	local -i start=$EPOCHSECONDS
	"$@"
	print -P "%F{8}took $(( EPOCHSECONDS - start ))s%f"
}
bak() { zmv -W '*.(conf|ini)' '*.$1.bak' }

precmd_title() { print -Pn "\e]0;%n@%m: %~\a" }
preexec_title() { print -Pn "\e]0;${(q)1}\a" }
autoload -Uz add-zsh-hook
add-zsh-hook precmd precmd_title
add-zsh-hook preexec preexec_title

PROMPT='%(?..%F{red}%? )%F{blue}%~%f %# '
RPROMPT='%F{8}%D{%H:%M}%f'

for rc in ${ZDOTDIR:-$HOME}/.zshrc.d/*.zsh(N-.); do
	source $rc
done
unset rc

[[ -f ~/.zshrc.local ]] && source ~/.zshrc.local
true
