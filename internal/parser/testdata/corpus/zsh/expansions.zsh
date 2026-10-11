typeset -a words=(alpha beta gamma) other=(1 2)
typeset -A table=(one 1 two 2)
name=Hello.World.tar.gz

print -r -- ${(U)name} ${(L)name} ${(C)name}
print -r -- ${name:r} ${name:e} ${name:t} ${name:h} ${name:A} ${name:r:r}
print -r -- ${(j:, :)words} ${(s:.:)name} ${(f)"$(print a; print b)"}
print -r -- ${(o)words} ${(O)words} ${(u)words} ${(k)table} ${(v)table} ${(kv)table}
print -r -- ${#words} ${words[2]} ${words[-1]} ${words[2,-1]} ${words[(i)beta]} ${words[(r)g*]}
print -r -- ${table[one]} ${(@)words:#beta} ${words/#a/A} ${words//a/A} ${name:/Hello.World.tar.gz/whole}
print -r -- ${words:^other} ${words:^^other} ${unset_name::=assigned} ${unset_name}
print -r -- ${${name%.gz}%.tar} ${(%):-%n} ${(q)name} ${(qq)name} ${(Q)${(q)name}}
print -r -- ${name:s/World/There/} ${name:gs/./-/} ${(l:10::0:)#words} ${(r:12:: :)name}x
print -r -- ${+table[one]} ${+commands[git]} ${(t)table} ${(P)${:-name}}
print -r -- ${~name} ${=name} ${^words}.txt
print -r -- ${(e)${:-'$name'}} ${(z)${:-'a b; c'}}

print -r -- $(( #name )) $(( ##a )) $(( ##\n )) $(( [#16] 255 )) $(( [##2] 5 ))
print -r -- $(( 2 ** 10 )) $(( 7 / 2. )) $(( 1e3 )) $(( 0x1f + 0b101 + 8#17 ))
print -r -- $(( sqrt(16) + abs(-2) + int(2.5) )) $(( hypot(3, 4) )) $(( rand48() < 1 ))
integer -i 16 hex=255
float -F 3 pi=3.14159
(( pi *= 2, hex += 1 ))
print -r -- $hex $pi

setopt extended_glob
print -rl -- *(N) *(.N) *(/N) *(@N) *(*N) **/*.zsh(N.om[1,5]) *(Lk+10N) *(mh-1N)
print -rl -- ^*.zsh(N) *.(zsh|sh)(N) (#i)readme*(N) ((a|b)|c)*(N) <1-10>.log(N) **/(#a1)nmae(N)
print -rl -- *(e:'[[ -s $REPLY ]]':N) *(+nonempty) *(N:t:r) *(Om[1])(N)
print -rl -- ${(M)words:#(#b)(a)*} $match ${(@)words%%(#m)a*}

eval 'print -r -- ${${(%):-%x}:A:h}'
print -r -- =(print temp) <(print in) >(cat >/dev/null)
print -r -- "${(@)words[1,2]}" "${#${(@)words}}" "$(( ${#words} + 1 ))"
