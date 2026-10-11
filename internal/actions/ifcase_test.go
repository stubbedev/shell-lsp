package actions

import "testing"

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
		"«if» [ \"$x\" = a ]; then echo a; elif [ \"$x\" = b ]; then echo b; fi\n",
		"if [ \"$x\" = a ]; then\n\t«echo» a\nelif [ \"$x\" = b ]; then\n\techo b\nfi\n",
		"«if» [[ $x == @(a|b) ]]; then\n\techo a\nelif [[ $x == c ]]; then\n\techo b\nfi\n",
		"«if» [ \"$x\" = a -a \"$y\" = b ]; then\n\techo a\nelif [ \"$x\" = b ]; then\n\techo b\nfi\n",
		"«if» grep -q a f; then\n\techo a\nelif [ \"$x\" = b ]; then\n\techo b\nfi\n",
		"«if» [ \"$x\" = a ]; then # first\n\techo a\nelif [ \"$x\" = b ]; then\n\techo b\nfi\n",
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
