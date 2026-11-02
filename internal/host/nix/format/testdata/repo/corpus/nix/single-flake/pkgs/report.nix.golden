{
  lib,
  writeShellScript,
  writers,
  writeText,
  jq,
  gawk,
  gnused,
  threshold,
}:
let
  columns = writeText "columns.conf" ''
    size
    name
  '';
in
{
  summary = writeShellScript "logship-report" ''
    limit=${lib.escapeShellArg (toString threshold)}
    ${jq}/bin/jq --arg limit "$limit" '.[] | select(.size > ($limit | tonumber))' "$1"
    ${gawk}/bin/awk -v limit="$limit" '$1 > limit { print $2 }' "$2"
    ${lib.getExe gnused} -n '/^${lib.escapeShellArg "size"}/p' ${columns}
  '';

  dashed = writers.writeDash "logship-report-dash" ''
    echo "$1"
  '';

  python = writers.writePython3 "logship-report-python" { } ''
    print("not a shell")
  '';

  makefile = writeText "Makefile" ''
    all:
    	echo logship
  '';
}
