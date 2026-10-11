#!/bin/ksh
function .sh.math.hypot x y {
	.sh.value=$(( sqrt(x*x + y*y) ))
}
function .sh.math.clamp v lo hi {
	(( .sh.value = v < lo ? lo : v > hi ? hi : v ))
}

float r=$(( hypot(3, 4) ))
integer n=$(( clamp(15, 0, 10) ))
typeset -E3 e=1.0e-3
typeset -F2 money=19.999
typeset -i2 bits=10
typeset -X x=255
typeset -lu big=4294967296

print -r -- "$r $n $e $money $bits $x $big"
print -r -- $(( sin(0) + cos(0) + exp(0) + log(1) + pow(2, 3) + fmod(7, 3) ))
print -r -- $(( floor(2.7) + ceil(2.1) + trunc(-1.5) + round(2.5) + int(9.9) ))
print -r -- $(( 16#ff + 2#1010 + 0x10 + 1_000 ))
print -r -- $(( r > 4.9 && r < 5.1 ? 1 : 0 )) $(( -2 ** 2 )) $(( 5 % 3 ))
(( n++, r /= 2, bits <<= 1 ))
let "n += 2" "r *= 3"
printf '%.3f %d\n' "$r" "$n"
for (( i = 0; i < 3; i++ )); do
	print -r -- "$(( i * 1.5 ))"
done
