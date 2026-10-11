package actions

import "testing"

func TestConvertBackticks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src, want string
	}{
		{src: "now=«`date`»\n", want: "now=$(date)\n"},
		{src: "echo `ls «-l»` done\n", want: "echo $(ls -l) done\n"},
		{src: "x=`echo \\$HOME \\\\ \\`i«d»\\``\n", want: "x=$(echo $HOME \\ `id`)\n"},
		{src: "echo \"`echo \\\"«hi»\\\" \\n`\"\n", want: "echo \"$(echo \"hi\" \\n)\"\n"},
		{src: "echo `echo \\\"«hi»\\\"`\n", want: "echo $(echo \\\"hi\\\")\n"},
		{src: "x=`«(»cd /; pwd)`\n", want: "x=$( (cd /; pwd))\n"},
		{src: "echo \"$(echo `«d»ate`)\"\n", want: "echo \"$(echo $(date))\"\n"},
		{src: "x=`echo '\\`' «a»`\n", want: "x=$(echo '`' a)\n"},
		{src: "x=`echo a\\\n«b»`\n", want: "x=$(echo a\\\nb)\n"},
		{src: "x=\\\\`«d»ate`\n", want: "x=\\\\$(date)\n"},
	}
	for _, tc := range tests {
		got, title, ok := refactor(t, ConvertBackticks, tc.src)
		if !ok || got != tc.want || title != "Convert backticks to $(...)" {
			t.Errorf("convert %q = %q (%q, %t), want %q", tc.src, got, title, ok, tc.want)
		}
	}
	for _, src := range []string{
		"echo «$(date)»\n",
		"echo `date # «c»omment`\n",
		"echo '`«d»ate`'\n",
		"echo \\`«d»ate\\`\n",
		"x=`cat <<EOF\n«h»i\nEOF\n`\n",
		"echo `da«t»e\n",
	} {
		if got, _, ok := refactor(t, ConvertBackticks, src); ok {
			t.Errorf("convert %q offered: %q", src, got)
		}
	}
}
