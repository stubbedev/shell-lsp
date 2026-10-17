shebang:
    #!/usr/bin/env bash
    set -euo pipefail
    for f in *; do
      echo "$f"
    done

[script('python3')]
python:
    print("hi")

continued:
    echo one \
      two \
      three
    @-echo quiet and ignored

escaped:
    echo '{{{{not an interpolation}}'

interp-cont:
    echo {{ \
      "x" \
    }}

blank-lines:
    echo a

    echo b
