for f (*.txt); do echo $f; done
if [[ -n $x ]] { echo short }
echo after

for f (*.log) print -r -- $f
for f in a b c; print -r -- $f
for key value in a 1 b 2; do
	print -r -- "$key=$value"
done
for key value (a 1 b 2) {
	print -r -- "$key=$value"
}
for (( i = 0; i < 3; i++ )) {
	print $i
}
for (( i = 0; i < 3; i++ )) print $i
foreach name (alpha beta)
	print -r -- $name
end
foreach name in gamma delta
	print -r -- $name
end

while (( i < 6 )) {
	(( i++ ))
}
until (( i == 0 )) (( i-- ))
while [[ -z $done ]] {
	done=yes
}
repeat 3 print hi
repeat 2 {
	print block
}
repeat $(( 1 + 1 )); do print loop; done

select choice (one two) {
	print -r -- $choice
	break
}

if [[ -n $1 ]] {
	print one
} elif [[ -n $2 ]] {
	print two
} else {
	print none
}
if (( $# )) print args
if [[ $1 == -v ]] print verbose

case $1 {
	(start|run) print starting ;;
	((a|b)*) print grouped ;;
	(*) print other ;;
}
case $1 in
	(#i)yes) print agreed ;;
	[[:digit:]]##) print number ;;
	*) ;;
esac

{
	print trying
	false
} always {
	print cleanup
}
{ print -r -- $TRY_BLOCK_ERROR } always { (( TRY_BLOCK_ERROR = 0 )) }

() {
	local scratch=$1
	print -r -- $scratch
} first second
() { print -r -- "$@" } "${(@)argv}"
print -r -- $(() { print inner })

function one two {
	print -r -- $0
}
three four () print -r -- $0
function five; print -r -- $0
function () { print anonymous }

coproc cat
print -p hello
read -p line
coproc { print from coproc }
read -p reply

[[ -N ~/.zshrc ]] && print modified
{ local inside=1 }
{ let inside++ }
{ time sleep 0 }
