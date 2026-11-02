_: {
  harbor.builders.probe =
    {
      final,
      lib,
      ...
    }:
    {
      label,
      script,
      vars ? { },
    }:
    final.runCommand "probe-${label}" ({ inherit label; } // vars) ''
      ${final.bash}/bin/bash -c ${lib.escapeShellArg script}
    '';

  harbor.builders.quiet =
    { harbor, ... }:
    {
      label,
      script,
    }:
    harbor.probe { inherit label script; };
}
