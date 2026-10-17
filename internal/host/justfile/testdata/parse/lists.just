set unstable
set lists

version := "1"
raw := "2"
logic := version && raw || "c"
negated := if !(version != "x") { "a" } else { "b" }

list:
    echo {{logic}} {{negated}}
