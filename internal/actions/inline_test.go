package actions

import "testing"

func TestInlineVariable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src, want string
	}{
		{src: "«dir»=/var/log\nls \"$dir\" $dir\n", want: "ls \"/var/log\" /var/log\n"},
		{src: "msg='hello world'\necho \"${«msg»}!\"\n", want: "echo \"hello world!\"\n"},
		{src: "x='a\"$b`c\\d'\necho \"$«x»\"\n", want: "echo \"a\\\"\\$b\\`c\\\\d\"\n"},
		{src: "x='a;b'\necho $«x»\n", want: "echo 'a;b'\n"},
		{src: "x=\"it's\"\necho $«x»\n", want: "echo 'it'\\''s'\n"},
		{src: "n=3\necho $(( «n» + 1 )) $((n*2)) $(($n))\n", want: "echo $(( 3 + 1 )) $((3*2)) $((3))\n"},
		{src: "f() {\n\tlocal «name»=world\n\techo \"hi $name\"\n}\n", want: "f() {\n\techo \"hi world\"\n}\n"},
		{src: "f() {\n\t«name»=world\n\techo \"hi $name\"\n}\n", want: "f() {\n\techo \"hi world\"\n}\n"},
		{src: "v='$HOME \"q\"'\ncat <<EOF\n$«v»\nEOF\n", want: "cat <<EOF\n\\$HOME \"q\"\nEOF\n"},
		{src: "x=1; echo $«x»\n", want: "echo 1\n"},
		{src: "w=then\necho $«w»\n", want: "echo 'then'\n"},
		{src: "e=\necho \"[$«e»]\"\n", want: "echo \"[]\"\n"},
		{src: "x=1 # one\necho $«x»\n", want: "echo 1\n"},
		{src: "p=abc\n[[ $«p» == a* ]]\n", want: "[[ abc == a* ]]\n"},
		{src: "x=a\\ b\necho \"$«x»\"\n", want: "echo \"a b\"\n"},
		{src: "x=1\nif true; then\n\techo \"$«x»\"\nfi\n", want: "if true; then\n\techo \"1\"\nfi\n"},
	}
	for _, tc := range tests {
		got, title, ok := refactor(t, InlineVariable, tc.src)
		if !ok || got != tc.want {
			t.Errorf("inline %q = %q (%q, %t), want %q", tc.src, got, title, ok, tc.want)
		}
	}
	if _, title, _ := refactor(t, InlineVariable, "«dir»=/tmp\necho \"$dir\"\n"); title != "Inline variable dir" {
		t.Errorf("title = %q", title)
	}
	for _, src := range []string{
		"x=1\nx=2\necho $«x»\n",
		"echo $«x»\nx=1\n",
		"if true; then «x»=1; fi\necho $x\n",
		"«x»=$(date)\necho \"$x\"\n",
		"«x»=$HOME\necho \"$x\"\n",
		"export «x»=1\necho \"$x\"\n",
		"«x»=1\nexport x\necho \"$x\"\n",
		"readonly «x»=1\necho \"$x\"\n",
		"«x»=1\necho \"$x\"\nunset x\n",
		"«x»=1\neval 'echo $x'\necho \"$x\"\n",
		"«x»=1\n. ./lib.sh\necho \"$x\"\n",
		"«x»=1\nf() { echo \"$x\"; }\n",
		"«x»=1\necho \"$x\"\nf() { echo \"$x\"; }\n",
		"«IFS»=,\necho \"$IFS\"\n",
		"«LC_COLLATE»=C\necho \"$LC_COLLATE\"\n",
		"«x»=1\necho ${x:-2}\n",
		"«x»=1\necho ${#x}\n",
		"«x»='a b'\necho $x\n",
		"«x»='*.txt'\nls $x\n",
		"«x»=\necho $x\n",
		"«x»=abc\necho $((x))\n",
		"«x»=~/bin\necho \"$x\"\n",
		"«x»=1\n",
		"«x»=1 &\necho \"$x\"\n",
		"«x»=1 >f\necho \"$x\"\n",
		"«x»=(a b)\necho \"$x\"\n",
		"«x»+=1\necho \"$x\"\n",
		"«i»=1\necho \"${a[$i]}\"\n",
		"«x»=1\n[[ -v x ]] && echo \"$x\"\n",
		"f() { «x»=1; }\necho \"$x\"\n",
		"«x»='a;b'\n[[ $x == y ]]\n",
		"«x»=1 env\necho \"$x\"\n",
		"echo «a» b\n",
		"«x»=1 && echo \"$x\"\n",
		"«x»=1\necho $\"$x\"\n",
		"f() {\n\tlocal «x»=1 y=2\n\techo \"$x$y\"\n}\n",
		"f() {\n\tlocal «x»=1\n\tx=2\n\techo \"$x\"\n}\n",
		"f() {\n\tlocal «n»=1\n\t(( n++ ))\n\techo \"$n\"\n}\n",
		"f() {\n\tlocal «n»=1\n\tread -r n\n\techo \"$n\"\n}\n",
		"«n»=1\ng() { local n=2; }\necho \"$n\"\n",
	} {
		if got, _, ok := refactor(t, InlineVariable, src); ok {
			t.Errorf("inline %q offered: %q", src, got)
		}
	}
}
