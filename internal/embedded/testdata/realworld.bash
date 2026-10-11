#!/usr/bin/env bash
set -euo pipefail

# Log summaries.
awk '{ count[$1]++ } END { for (ip in count) print count[ip], ip }' access.log | sort -rn | head -n 10
awk -F'"' '{ print $2 }' access.log | awk '{ print $2 }' | sort | uniq -c
awk '$9 >= 500 { errors++ } END { printf "%.2f%%\n", 100 * errors / NR }' access.log
awk 'NR % 1000 == 0 { printf "." } END { print "" }' big.txt
awk 'length > max { max = length; line = $0 } END { print max ": " line }' file.txt
awk 'BEGIN { srand() } { print rand() "\t" $0 }' list.txt | sort -n | cut -f2-
awk '!seen[$0]++' duplicates.txt
awk 'NF { $1 = $1; print }' messy.txt
awk -v n=3 'NR <= n' file.txt
awk -v OFS='\t' '{ $1 = $1 } 1' spaced.txt
awk -F: '$3 >= 1000 && $7 !~ /nologin/ { print $1 }' /etc/passwd
awk 'FNR == 1 { print FILENAME } { total += $2 } END { print total }' a.txt b.txt
awk 'NR == FNR { keep[$1]; next } $1 in keep' ids.txt data.txt
awk '/^\[/{ section = $0; next } section == "[core]" && /=/ { print }' config.ini
awk 'BEGIN { FS = "," } NR > 1 { sum[$1] += $3 } END { for (k in sum) printf "%s,%d\n", k, sum[k] }' sales.csv
awk '{ lines[NR] = $0 } END { for (i = NR; i > 0; i--) print lines[i] }' file.txt
awk '{ gsub(/^[ \t]+|[ \t]+$/, ""); print }' padded.txt
awk 'match($0, /version="[^"]+"/) { print substr($0, RSTART + 9, RLENGTH - 10) }' pom.xml
awk '{ n = split($0, parts, "/"); print parts[n] }' paths.txt
awk 'BEGIN { while ((getline line < "/etc/hosts") > 0) n++; close("/etc/hosts"); print n }'
awk '{ "date +%s" | getline now; close("date +%s"); print now - $1 }' times.txt
awk '{ print | "sort -u" } END { close("sort -u") }' names.txt
awk 'function trim(s) { sub(/^ +/, "", s); sub(/ +$/, "", s); return s } { print trim($0) }' f
awk '/start/,/end/' file.txt
awk 'NR == 1, /^$/ { next } { print }' mail.txt
awk '{ printf "%-20s %8.2f\n", $1, $2 / 1024 }' sizes.txt
awk 'BEGIN { OFS = FS = "\t" } { tmp = $1; $1 = $2; $2 = tmp; print }' pairs.tsv
awk -v pat="$pattern" '$0 ~ pat { c++ } END { print c + 0 }' log.txt
awk "BEGIN { print \"literal\" }"
awk '{ $NF = ""; sub(/ $/, ""); print }' cols.txt
awk 'END { print NR }' file.txt
awk '{ s = s (NR > 1 ? "," : "") $0 } END { print s }' list.txt
gawk 'BEGINFILE { if (ERRNO) nextfile } { n++ } ENDFILE { print FILENAME, n; n = 0 }' *.log
gawk '{ print gensub(/([0-9]+)-([0-9]+)/, "\\2-\\1", "g") }' dates.txt
gawk 'BEGIN { PROCINFO["sorted_in"] = "@ind_str_asc" } { a[$1] = $2 } END { for (k in a) print k, a[k] }' kv.txt
total=$(awk '{ s += $1 } END { print s }' numbers.txt)
mapfile -t fields < <(awk -F, '{ print $2 }' data.csv)

awk -f /dev/stdin data.txt <<'AWK'
BEGIN { FS = ":" }
{
	for (i = 1; i <= NF; i++) {
		if ($i ~ /^[0-9]+$/)
			sum += $i
		else
			words++
	}
}
END {
	printf "sum=%d words=%d\n", sum, words
}
AWK

# Stream edits.
sed -i 's/^#\(Port\) .*/\1 2222/' /etc/ssh/sshd_config
sed -n '/^BEGIN/,/^END/p' cert.pem
sed '/^$/d; s/[[:space:]]\+$//' notes.txt
sed -e '1d' -e '$d' table.txt
sed 's/\x1b\[[0-9;]*m//g' colored.log
sed -E 's/([0-9]{4})-([0-9]{2})-([0-9]{2})/\3.\2.\1/' dates.txt
sed -r 's/(foo|bar)+/X/g' f
sed 's|/usr/local|/opt|g' paths.txt
sed '1!G;h;$!d' file.txt
sed -n '$=' file.txt
sed ':a;N;$!ba;s/\n/,/g' lines.txt
sed -e ':a' -e '$!N;s/\n//;ta' joined.txt
sed '/^#/!s/foo/bar/g' config
sed '0,/needle/s//replacement/' haystack.txt
sed '1~2d' alternate.txt
sed '/start/,+3d' block.txt
sed -n '2{p;q}' file.txt
sed 's/.*/"&"/' words.txt
sed 'y/abcdefghijklmnopqrstuvwxyz/ABCDEFGHIJKLMNOPQRSTUVWXYZ/' lower.txt
sed '/pattern/a\
appended line' file.txt
sed '/pattern/i inserted line' file.txt
sed '$a\' missing-newline.txt
sed -i.bak -e 's/http:/https:/g' urls.txt
sed --in-place --expression='s/a/b/' file
sed -z 's/\n$//' file
sed 's/[^[:alnum:]_-]//g' slug.txt
sed -n 's/^version = "\(.*\)"$/\1/p' Cargo.toml
sed "s/@VERSION@/1.2.3/" template.in
sed 's/\t/    /g' tabs.txt
sed -n '/^\s*$/!p' blanks.txt
sed '/^\s*#/d;/^\s*$/d' commented.conf
sed 's/ *$//;s/^ *//' trim.txt
version=$(sed -n 's/^VERSION=//p' Makefile)
command sed -e 's/a/b/' file

sed -f - input.txt <<'SED'
# drop comments and blank lines
/^#/d
/^$/d
s/[[:space:]]*=[[:space:]]*/=/
SED

# JSON.
jq '.' response.json
jq -r '.items[] | "\(.name)\t\(.status)"' items.json
jq -r '.[] | select(.active) | .email' users.json
jq '[.[] | {id, name: .full_name}]' users.json
jq --arg tag "$tag" '.releases[] | select(.tag == $tag) | .assets[].url' releases.json
jq --argjson n "$count" '.[:$n]' list.json
jq -n --arg name "$name" '{name: $name, created: now | todate}'
jq -s 'add' parts/*.json
jq -s 'map(.size) | add' sizes.json
jq -c '.[]' array.json
jq -e '.ok == true' status.json
jq '.dependencies | keys[]' package.json
jq '.scripts.build // "none"' package.json
jq 'to_entries | map(select(.value > 10)) | from_entries' counts.json
jq 'with_entries(.key |= ascii_downcase)' headers.json
jq '.a as $x | .b | map(. + $x)' nums.json
jq 'reduce .[] as $item ({}; .[$item.kind] += [$item.name])' things.json
jq 'group_by(.team) | map({team: .[0].team, count: length})' people.json
jq 'paths(type == "number")' tree.json
jq 'del(.metadata.annotations)' manifest.json
jq '.spec.template.spec.containers[0].image = $image' --arg image "$image" deploy.json
jq -r '@csv' rows.json
jq -r '.[] | [.a, .b] | @tsv' rows.json
jq -r '@sh "echo \(.cmd)"' cmd.json
jq 'if .type == "dir" then .children | length else .size end' node.json
jq 'try (.x | tonumber) catch "not a number"' x.json
jq 'def inc: . + 1; map(inc)' nums.json
jq '[limit(5; .[])]' long.json
jq 'sort_by(.date) | reverse | .[0]' events.json
jq '.[] | select(.name | test("^api-"; "i"))' services.json
jq 'walk(if type == "object" then del(.secret) else . end)' config.json
jq --raw-output '.token' auth.json
jq ".version" package.json
jq -r '.env | to_entries[] | "\(.key)=\(.value)"' config.json
jq 'label $done | foreach .[] as $x (0; . + $x; if . > 100 then ., break $done else empty end)' n.json
jq '.[] as [$k, $v] | {($k): $v}' pairs.json
jq 'input_filename' a.json b.json
jq -n '[inputs | .count] | add' parts.json
jq '$ENV.HOME' /dev/null
id=$(jq -r '.id' <<<"$response")
curl -s "$url" | jq -r '.data[] | .attributes.name'
