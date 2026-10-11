#!/bin/ksh
compound config=(
	name=service
	typeset -i port=8080
	typeset -a hosts=(alpha beta)
	limits=(
		cpu=2
		memory=512
	)
)
point=(x=1 y=2)
nested=( (a=1) (a=2) )
typeset -A indexed=([first]=(id=1) [second]=(id=2))
matrix=( (1 2) (3 4) )

print -r -- "${config.name}:${config.port} ${config.hosts[1]} ${config.limits.cpu}"
print -r -- "${point.x},${point.y} ${nested[1].a} ${indexed[second].id} ${matrix[1][0]}"
config.limits.cpu=4
config.hosts+=(gamma)
point.x+=10
matrix[0][1]=9
print -v config
print -r -- "${!config.*}" "${@config}" "${#config.hosts[@]}"

typeset -C saved=config
nameref ref=config.limits
ref.memory=1024
print -r -- "${saved.limits.memory} ${config.limits.memory}"

function describe {
	nameref target=$1
	print -r -- "${target.name} listens on ${target.port}"
}
describe config

for key in "${!config.limits.@}"; do
	print -r -- "$key"
done
print -r -- "${.sh.version}" "${.sh.pid}" "${#config.@}"
unset config.limits
