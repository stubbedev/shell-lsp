#!/usr/bin/env just --justfile
# top comment



set   shell:=["bash","-c"]
set export
set dotenv-load := true
set positional-arguments := false
alias   b:=build
export   TOKEN:='x'
x:="a"+'b'   # trailing
y := if x=="ab"{"1"}else if x =~ 'c' {"2"} else {"3"}
z := ( "a"
  + "b" )
p := "a" / "b"
q := / "abs"
l := `ls`
m := '''
  indented
'''
cont := "a" + \
    "b"
[private,no-cd]
[group: "g"]
@build   target="debug"  +rest="":   dep   (dep2  "a"  "b") && post
  echo {{target}}   {{  rest  }}
  @echo hi

dep:
	echo dep
dep2 a b:
    #!/usr/bin/env bash
    echo {{a}}{{{{literal}}
      indented more
post:
    echo one


    echo two
mod? foo   'foo.just'
import?   'x.just'
unexport FOO
# closing comment
