autoload -Uz vcs_info add-zsh-hook colors && colors
zstyle ':vcs_info:*' enable git
zstyle ':vcs_info:git:*' formats ' %F{magenta}(%b)%f'
setopt prompt_subst

typeset -ga precmd_functions
_prompt_precmd() {
	vcs_info
	local -i last=$?
	local user=${(%):-%n}
	psvar=()
	(( last != 0 )) && psvar[1]=$last
	PROMPT="%F{cyan}${user}%f %~${vcs_info_msg_0_} %(1V.%F{red}%1v%f .)%# "
}
add-zsh-hook precmd _prompt_precmd

path=(~/.local/bin $path)
typeset -U path
for plugin in ${ZDOTDIR:-$HOME}/plugins/*.zsh(N); do
	source $plugin
done
