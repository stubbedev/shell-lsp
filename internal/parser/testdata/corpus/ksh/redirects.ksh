#!/bin/ksh
log=${TMPDIR:-/tmp}/redirects.$$.log
trap 'rm -f "$log"' EXIT

cat <<# EOF
	indented with a tab
	  kept relative indentation
	EOF
cat <<-EOF
	tabs stripped
	EOF
cat <<'EOF'
literal $log
EOF

print -r -- first >; "$log"
print -r -- second >>"$log"
tr a-z A-Z <>; "$log"
exec 3<> "$log"
exec 3<#((0))
read -r -u3 line
print -r -- "offset $(3<#)"
exec 3>#((EOF))
exec 3<##second
exec 3<&-
exec 4<"$log"
exec 4<#'second'
exec 4<&-
print -r -- "$line"

{
	print -r -- out
	print -u2 -r -- err
} 2>&1 >/dev/null | cat
diff <(sort "$log") <(sort -r "$log") >/dev/null || true

sort |&
print -p charlie
print -p alpha
exec 5>&p
exec 5>&-
read -p -r sorted
print -r -- "$sorted"
exec {fd}<"$log"
read -r -u$fd line
exec {fd}<&-

print -r -- @(one|two) +(a|b)c ?(x)y *(z) !(*.log) ~(E)^a.*$ ~(i:readme)
case $line in
	@(first|second)) print -r -- matched ;&
	*(?)) print -r -- fallthrough ;;&
	*) print -r -- always ;;
esac
[[ $line == ~(E)^f.+t$ ]] && print -r -- regex
[[ $line =~ ^f(ir)st$ ]] && print -r -- "${.sh.match[1]}"
