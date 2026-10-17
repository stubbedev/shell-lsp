#!/usr/bin/env just
#documents a
a:
    echo a

#	documents b  
[private]
b:
    echo b

#not a doc

c:
    echo c

#first
#second
d:

#   
e:

#ignored
[doc("explicit")]
f:

#a module
mod? tools

#a variable  
x := "1" # trailing  
