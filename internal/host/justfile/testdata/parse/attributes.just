[doc('Overridden doc')]
[linux, macos]
[working-directory: 'sub']
[env("NAME", "value")]
[arg('target', long='target', help="what to build")]
[arg('verbose', long, value="true")]
build target verbose="false":
    echo {{target}} {{verbose}} $NAME

[windows]
build target verbose="false":
    echo windows

[unix]
[no-exit-message]
[positional-arguments]
[extension('.sh')]
[script]
other:
    echo $1
[doc(" x ")]
spaced:

[doc(" ")]
blank:
