#!/bin/ksh
enum Color_t=(red green blue)
Color_t shade=green

typeset -T Point_t=(
	float x=0
	float y=0
	function len {
		print -r -- $(( sqrt(_.x*_.x + _.y*_.y) ))
	}
	function shift {
		(( _.x += $1 ))
		(( _.y += $2 ))
	}
)
Point_t origin
Point_t corner=(x=3 y=4)
corner.shift 1 1
corner.len
Point_t -a path=( (x=1 y=1) (x=2 y=2) )
print -r -- "${@corner}" "${path[1].x}"

typeset -T Stack_t=(
	typeset -a items
	integer size=0
	push() {
		_.items[_.size++]=$1
	}
	pop() {
		(( _.size > 0 )) || return 1
		print -r -- "${_.items[--_.size]}"
	}
)
Stack_t stack
stack.push one
stack.push two
stack.pop

counter=0
function counter.get {
	.sh.value=$(( ${.sh.value} + 1 ))
}
function counter.set {
	print -u2 -r -- "counter set to ${.sh.value}"
}
counter=5
print -r -- "$counter $shade"

namespace inventory {
	typeset -A items
	function add {
		items[$1]=$2
	}
	function show {
		for key in "${!items[@]}"; do
			print -r -- "$key=${items[$key]}"
		done
	}
}
.inventory.add apples 3
.inventory.show
print -r -- "${.inventory.items[apples]}"
