set unstable
set lists
lst := ["a","b"]
e := "1"
f := f'x{{e}}y{{{{z'
neg := !( "a" == "b" )
both := "a" && "b" || "c"
cat := lst ++ ["c"]
h := if "a" == "b" { "c" }
fun(a,b):=a+b
[arg("x", long="y", help='z')]
[env("A","b")]
[script("python3")]
d x: *(c *lst) && (post)
    print(1)
c v:
    echo {{v}}
post:
