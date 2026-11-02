{
  lib,
  writeShellApplication,
  coreutils,
  findutils,
  zstd,
  retentionDays,
}:
writeShellApplication {
  name = "logship-rotate";
  runtimeInputs = [coreutils findutils zstd];
  text = ''
    spool="''${1:?usage: logship-rotate SPOOL}"
    find "$spool" -name '*.log' -mtime +1 -print0 |
      while IFS= read -r -d "" log; do
        zstd --rm --quiet "$log"
      done
    find "$spool" -name '*.zst' -mtime +${lib.escapeShellArg (toString retentionDays)} -delete
  '';
}
