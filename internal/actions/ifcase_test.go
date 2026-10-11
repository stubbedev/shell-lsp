package actions

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestIfToCase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src, want string
	}{
		{
			src:  "if «[» \"$x\" = a ]; then\n\techo a\nelif [ \"$x\" = 'b c' ] || [ \"${x}\" = \"$y\" ]; then\n\techo b\nelse\n\techo other\nfi\n",
			want: "case \"$x\" in\na)\n\techo a\n\t;;\n'b c'|\"$y\")\n\techo b\n\t;;\n*)\n\techo other\n\t;;\nesac\n",
		},
		{
			src:  "f() {\n\t«if» [[ $mode == dev* || $mode == test ]]; then\n\t\trun dev\n\n\t\t# comment\n\t\trun more\n\telif [[ $mode = \"prod\" ]]; then\n\t\tdeploy\n\tfi >log\n}\n",
			want: "f() {\n\tcase $mode in\n\tdev*|test)\n\t\trun dev\n\n\t\t# comment\n\t\trun more\n\t\t;;\n\t\"prod\")\n\t\tdeploy\n\t\t;;\n\tesac >log\n}\n",
		},
		{
			src:  "if test \"$1\" = start; then\n  up\nelif test \"$1\" «=» stop; then\n  down\nfi\n",
			want: "case \"$1\" in\nstart)\n  up\n  ;;\nstop)\n  down\n  ;;\nesac\n",
		},
		{
			src:  "if [ \"$x\" = a -o \\( \"$x\" = b \\) ]; then\n\tab\nelif [[ «$x» == c ]]; then\n\tc\nfi\n",
			want: "case \"$x\" in\na|b)\n\tab\n\t;;\nc)\n\tc\n\t;;\nesac\n",
		},
		{
			src:  "«if» [ \"$x\" = a ]; then foo; elif [ \"$x\" = b ]; then bar; else baz; fi\n",
			want: "case \"$x\" in\na)\n\tfoo\n\t;;\nb)\n\tbar\n\t;;\n*)\n\tbaz\n\t;;\nesac\n",
		},
		{
			src:  "«if» [ \"$x\" = a ]; then a1; a2 & elif [ \"$x\" = b ]; then b1;fi # done\n",
			want: "case \"$x\" in\na)\n\ta1; a2 &\n\t;;\nb)\n\tb1\n\t;;\nesac # done\n",
		},
		{
			src:  "main() {\n  «if» [ \"$1\" = a ]; then foo\n    bar\n  elif [ \"$1\" = b ]; then\n    baz; fi\n}\n",
			want: "main() {\n  case \"$1\" in\n  a)\n    foo\n    bar\n    ;;\n  b)\n    baz\n    ;;\n  esac\n}\n",
		},
		{
			src:  "«if» [ \"$x\" = a ]\nthen\n\tfoo\nelif [ \"$x\" = b ]\nthen\n\tbar\nfi; echo done\n",
			want: "case \"$x\" in\na)\n\tfoo\n\t;;\nb)\n\tbar\n\t;;\nesac; echo done\n",
		},
		{
			src:  "«if» [ \"$x\" = a ]; then # first\n\techo a\nelif [ \"$x\" = b ]; then\n\techo b\nelse # other\n\techo c\nfi\n",
			want: "case \"$x\" in\na)\n\t# first\n\techo a\n\t;;\nb)\n\techo b\n\t;;\n*)\n\t# other\n\techo c\n\t;;\nesac\n",
		},
		{
			src:  "«if» [ \"$x\" = a ]; then cat <<EOF\n  body\nEOF\nelif [ \"$x\" = b ]; then echo \"b\n  c\"; fi\n",
			want: "case \"$x\" in\na)\n\tcat <<EOF\n  body\nEOF\n\t;;\nb)\n\techo \"b\n  c\"\n\t;;\nesac\n",
		},
	}
	for _, tc := range tests {
		got, title, ok := refactor(t, IfToCase, tc.src)
		if !ok || got != tc.want || title != "Convert to case" {
			t.Errorf("convert %q = %q (%q, %t)\nwant %q", tc.src, got, title, ok, tc.want)
		}
	}
	for _, src := range []string{
		"«if» [ \"$x\" = a ]; then\n\techo a\nfi\n",
		"«if» [ \"$x\" = a ]; then\n\techo a\nelse\n\techo b\nfi\n",
		"«if» [ \"$x\" = a ]; then\n\techo a\nelif [ \"$y\" = b ]; then\n\techo b\nfi\n",
		"«if» [ \"$x\" != a ]; then\n\techo a\nelif [ \"$x\" = b ]; then\n\techo b\nfi\n",
		"«if» [ $x = a ]; then\n\techo a\nelif [ $x = b ]; then\n\techo b\nfi\n",
		"«if» [ \"$x\" = a* ]; then\n\techo a\nelif [ \"$x\" = b ]; then\n\techo b\nfi\n",
		"«if» [ \"$x\" = $y ]; then\n\techo a\nelif [ \"$x\" = b ]; then\n\techo b\nfi\n",
		"x=1; «if» [ \"$x\" = a ]; then echo a; elif [ \"$x\" = b ]; then echo b; fi\n",
		"«if» [ \"$x\" = a ]; then cat <<EOF; elif [ \"$x\" = b ]; then echo b; fi\nbody\nEOF\n",
		"«if» [ \"$x\" = a ]; then echo a; elif [ \"$x\" = b ]; then cat <<EOF; fi\nbody\nEOF\n",
		"«if» [ \"$x\" = a ] # first\nthen\n\techo a\nelif [ \"$x\" = b ]; then\n\techo b\nfi\n",
		"«if» [ \"$x\" = a ]; then\n\techo a\nelif # second\n[ \"$x\" = b ]; then\n\techo b\nfi\n",
		"if [ \"$x\" = a ]; then\n\t«echo» a\nelif [ \"$x\" = b ]; then\n\techo b\nfi\n",
		"«if» [[ $x == @(a|b) ]]; then\n\techo a\nelif [[ $x == c ]]; then\n\techo b\nfi\n",
		"«if» [ \"$x\" = a -a \"$y\" = b ]; then\n\techo a\nelif [ \"$x\" = b ]; then\n\techo b\nfi\n",
		"«if» grep -q a f; then\n\techo a\nelif [ \"$x\" = b ]; then\n\techo b\nfi\n",
		"«if» [ \"$x\" = esac ]; then\n\techo a\nelif [ \"$x\" = b ]; then\n\techo b\nfi\n",
		"«if» ! [ \"$x\" = a ]; then\n\techo a\nelif [ \"$x\" = b ]; then\n\techo b\nfi\n",
		"«if» [ \"$(cmd)\" = a ]; then\n\techo a\nelif [ \"$(cmd)\" = b ]; then\n\techo b\nfi\n",
		"«if» [ \"${x:-d}\" = a ]; then\n\techo a\nelif [ \"${x:-d}\" = b ]; then\n\techo b\nfi\n",
		"«if» [ \"$x\" = a ] 2>/dev/null; then\n\techo a\nelif [ \"$x\" = b ]; then\n\techo b\nfi\n",
	} {
		if got, _, ok := refactor(t, IfToCase, src); ok {
			t.Errorf("convert %q offered: %q", src, got)
		}
	}
}

func TestIfToCaseLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		opts      format.Options
		src, want string
	}{
		{
			opts: format.Options{SwitchCaseIndent: true},
			src:  "f() {\n    «if» [ \"$1\" = a ]; then\n        x\n    elif [ \"$1\" = b ]; then y; fi\n}\n",
			want: "f() {\n    case \"$1\" in\n        a)\n            x\n            ;;\n        b)\n            y\n            ;;\n    esac\n}\n",
		},
		{
			opts: format.Options{SwitchCaseIndent: true},
			src:  "«if» [ \"$x\" = a ]; then\n\techo \"1\n2\"\n\tcat <<-EOF\n\tbody\n\tEOF\nelif [ \"$x\" = b ]; then\n\tb\nfi\n",
			want: "case \"$x\" in\n\ta)\n\t\techo \"1\n2\"\n\t\tcat <<-EOF\n\tbody\n\tEOF\n\t\t;;\n\tb)\n\t\tb\n\t\t;;\nesac\n",
		},
		{
			opts: format.Options{Indent: 2},
			src:  "«if» [ \"$x\" = a ]; then foo; elif [ \"$x\" = b ]; then bar; fi\n",
			want: "case \"$x\" in\na)\n  foo\n  ;;\nb)\n  bar\n  ;;\nesac\n",
		},
		{
			opts: format.Options{Indent: 2, SwitchCaseIndent: true},
			src:  "g() {\n\ttrue\n}\n«if» [ \"$x\" = a ]; then foo; elif [ \"$x\" = b ]; then bar; fi\n",
			want: "g() {\n\ttrue\n}\ncase \"$x\" in\n\ta)\n\t\tfoo\n\t\t;;\n\tb)\n\t\tbar\n\t\t;;\nesac\n",
		},
		{
			src:  "a() {\n   one\n}\nb() {\n   two\n   if true; then\n      three\n   fi\n}\n«if» [ \"$x\" = a ]; then foo; elif [ \"$x\" = b ]; then bar; fi\n",
			want: "a() {\n   one\n}\nb() {\n   two\n   if true; then\n      three\n   fi\n}\ncase \"$x\" in\na)\n   foo\n   ;;\nb)\n   bar\n   ;;\nesac\n",
		},
		{
			src:  "cat <<EOF\n  heredoc text is not indentation\nEOF\n«if» [ \"$x\" = a ]; then foo; elif [ \"$x\" = b ]; then bar; fi\n",
			want: "cat <<EOF\n  heredoc text is not indentation\nEOF\ncase \"$x\" in\na)\n\tfoo\n\t;;\nb)\n\tbar\n\t;;\nesac\n",
		},
	}
	for _, tc := range tests {
		text, sel := selection(t, tc.src)
		change, ok := Compute(Document{Tree: parser.Parse(text, dialect.Bash), Format: tc.opts}, Refactor{Kind: IfToCase, Start: sel.Start, End: sel.End})
		if got := textedit.Apply(text, change.Edits); !ok || got != tc.want {
			t.Errorf("convert %q with %+v = %q (%t)\nwant %q", tc.src, tc.opts, got, ok, tc.want)
		}
	}
}
