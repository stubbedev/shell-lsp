version := "1.0.0"
export TOKEN := 'secret'
eager computed := `date`
raw := '''
  indented
  raw
'''
cooked := """
  line one\tx
"""
joined := version + "-" + raw
path := "a" / "b"
root := / "usr"
choice := if version == "1.0.0" { "one" } else if version =~ '^2' { "two" } else { "other" }
called := uppercase(version)
nested := replace(trim(version), ".", "_")
formatted := f'v{{version}}!'
multi := ('a' +
  'b')

show:
    echo {{version}} {{joined}} {{called}} {{formatted}}
