set unstable
set lists

root := if / "a" == "/a" { "y" } else { "n" }
concat := if "a" + "b" == "ab" { "y" } else { "n" }
join := if "a" / "b" == "a/b" { "y" } else { "n" }
lists := if "a" ++ "b" == "ab" { "y" } else { "n" }
rooted := / "a" + "b"
nested := "a" + / "b"
chain := "a" / "b" + "c"
logic := if "a" == "a" && "b" == "b" || "c" != "d" { "y" } else { "n" }
