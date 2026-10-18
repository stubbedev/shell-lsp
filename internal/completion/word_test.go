package completion

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestReplacedWordStopsAtTheTokenEnd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, src, label, rest string
	}{
		{name: "variable stops at the brace", src: "greeting=hi\necho \"${gr‸eet}x\"", label: "greeting", rest: "eet"},
		{name: "variable stops at punctuation", src: "greeting=hi\necho $gr‸eet/x", label: "greeting", rest: "eet"},
		{name: "array key stops at the bracket", src: "declare -A m=([alpha]=1)\necho ${m[al‸ph]}", label: "alpha", rest: "ph"},
		{name: "command word stops at a separator", src: "gr‸ep; ls", label: "grep", rest: "ep"},
		{name: "operator", src: "echo ${x:‸-y}", label: ":-", rest: "-"},
		{name: "nothing after the cursor", src: "gr‸ ls", label: "grep", rest: ""},
		{name: "command word ends before an expansion", src: "gr‸ep$(date)", label: "grep", rest: "ep"},
		{name: "command word keeps an escaped space", src: "gr‸ep\\ x", label: "grep", rest: "ep\\ x"},
		{name: "variable ends before a subscript", src: "greeting=hi\necho ${gr‸eet[0]}", label: "greeting", rest: "eet"},
	}
	for _, tc := range tests {
		offset := strings.Index(tc.src, cursor)
		text := strings.Replace(tc.src, cursor, "", 1)
		got := complete(t, dialect.Bash, tc.src)
		it, ok := find(got, tc.label)
		if !ok {
			t.Errorf("%s: %q not offered: %v", tc.name, tc.label, labels(got))
			continue
		}
		if it.Replace.End != offset || text[it.Word.Start:it.Replace.End]+tc.rest != text[it.Word.Start:it.Word.End] {
			t.Errorf("%s: insert %+v word %+v, want %q after the cursor", tc.name, it.Replace, it.Word, tc.rest)
		}
	}
}
