# The default recipe
default: build test

# Build the project
build target="debug" +flags="": (compile target) && notify
    cargo build --profile {{target}} {{flags}}

compile profile:
    echo compiling {{profile}}

@test *args:
    cargo test {{args}}

notify:
    echo done

[private]
_helper $token:
    echo $token

[group('ci'), no-cd]
[confirm("really?")]
deploy env='staging' region=(env + "-east"):
    echo {{env}} {{region}}

empty:

params-with-defaults a b='x' c=`echo y` d=(a + b) *rest:
    echo {{a}} {{b}} {{c}} {{d}} {{rest}}
