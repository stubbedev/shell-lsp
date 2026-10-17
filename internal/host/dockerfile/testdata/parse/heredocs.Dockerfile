FROM alpine
RUN <<EOF
set -e
echo one
EOF
RUN <<-"EOT" bash
	echo $x
	EOT
COPY <<A <<B /dst/
a
A
b
B
RUN python3 <<PY
print(1)
PY
