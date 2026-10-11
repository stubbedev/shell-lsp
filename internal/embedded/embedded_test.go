package embedded

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func find(t *testing.T, src string) []Program {
	t.Helper()

	tree := parser.Parse(src, dialect.Bash)
	if len(tree.Errors) > 0 {
		t.Fatalf("shell syntax errors in %q: %+v", src, tree.Errors)
	}
	return Find(tree)
}

func TestFindsPrograms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src      string
		language Language
		text     string
	}{
		{"awk '{print $1}' table.txt\n", Awk, "{print $1}"},
		{"gawk -F: -v OFS=, '{print $1}' /etc/passwd\n", Awk, "{print $1}"},
		{"awk -F\"$sep\" -v x=\"$y\" 'BEGIN { print x }'\n", Awk, "BEGIN { print x }"},
		{"awk -F '\\t' -- '{ print }' file\n", Awk, "{ print }"},
		{"awk \"{print \\$1 \\\"x\\\"}\" file\n", Awk, `{print $1 "x"}`},
		{"awk -e 'BEGIN {' -e 'x = 1 }'\n", Awk, "BEGIN {\nx = 1 }"},
		{"/usr/bin/awk 'NR>1' f\n", Awk, "NR>1"},
		{"busybox awk '{print $1}' f\n", Awk, "{print $1}"},
		{"command -p sed -n '$p' f\n", Sed, "$p"},
		{"sed -n -e '/start/,${' -e 'p' -e '}' notes.txt\n", Sed, "/start/,${\np\n}"},
		{"sed -es/a/b/g f\n", Sed, "s/a/b/g"},
		{"sed --expression='s/a/b/' f\n", Sed, "s/a/b/"},
		{"sed --expression 's/a/b/' f\n", Sed, "s/a/b/"},
		{"sed 's/a/b/' -i.bak file\n", Sed, "s/a/b/"},
		{"sed -i -e 's/a/b/' \"$file\"\n", Sed, "s/a/b/"},
		{"sed s/a/b/ file\n", Sed, "s/a/b/"},
		{"sed 's/old/'new'/g' config.ini\n", Sed, "s/old/new/g"},
		{"sed \"s/a/b/\" f\n", Sed, "s/a/b/"},
		{"jq -r '.items[]' f.json\n", Jq, ".items[]"},
		{"jq --arg k \"$v\" --argjson n 1 '.[$k] + $n'\n", Jq, ".[$k] + $n"},
		{"jq '.a' -r file\n", Jq, ".a"},
		{"exec jq '$ENV.HOME' config.json\n", Jq, "$ENV.HOME"},
		{"x=$(awk '{print $1}' f)\n", Awk, "{print $1}"},
		{"awk -f - <<'EOF'\n{ print $1 }\nEOF\n", Awk, "{ print $1 }\n"},
		{"sed -f /dev/stdin f <<-\"EOF\"\n\t\ts/a/b/\n\tp\n\tEOF\n", Sed, "s/a/b/\np\n"},
		{"sudo -u root sed -i 's/a/b/' /etc/x\n", Sed, "s/a/b/"},
		{"doas awk 'NR==1' f\n", Awk, "NR==1"},
		{"env LC_ALL=C awk '{print $1}' f\n", Awk, "{print $1}"},
		{"nice -n 5 nohup jq -c . f\n", Jq, "."},
		{"timeout 10 jq '.a' f\n", Jq, ".a"},
		{"printf '%s\\n' a b | xargs -n1 sed 's/a/b/'\n", Sed, "s/a/b/"},
		{"xargs -I {} sed 's/x/y/' {}\n", Sed, "s/x/y/"},
		{"find . -name '*.log' -exec awk '{n++} END {print n}' {} +\n", Awk, "{n++} END {print n}"},
		{"find . -type f -execdir sed -n '1p' {} \\;\n", Sed, "1p"},
		{"sudo awk -f - <<'EOF'\n{ print }\nEOF\n", Awk, "{ print }\n"},
	}
	for _, tt := range tests {
		programs := find(t, tt.src)
		if len(programs) != 1 {
			t.Errorf("Find(%q) = %d programs, want 1", tt.src, len(programs))
			continue
		}
		if p := programs[0]; p.Language != tt.language || p.Text != tt.text {
			t.Errorf("Find(%q) = %s %q, want %s %q", tt.src, p.Language, p.Text, tt.language, tt.text)
		}
	}
}

func TestSkipsProgramsItCannotSee(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"awk \"$prog\" f\n",
		"jq \".a | $x\" f\n",
		"jq -r \"$filter\"\n",
		"awk -f prog.awk f\n",
		"awk -f - -e 'x' <<'EOF'\n{\nEOF\n",
		"awk -f - <<EOF\n{ print $x }\nEOF\n",
		"sed -f script.sed f\n",
		"jq -f filter.jq f\n",
		"jq --from-file filter.jq f\n",
		"sed \"$opts\" 's/a/b/'\n",
		"sed s/a*/b/ f\n",
		"awk $'{print}' f\n",
		"sed -i '' 's/a/b/' f\n",
		"awk --unknown-option '{x' f\n",
		"awk\n",
		"sed -e\n",
		"jq --arg k\n",
		"echo awk '{x'\n",
		"sed -i '' f\n",
		"find . -exec awk '{x' {}\n",
		"find . -exec awk '/{}/' \\;\n",
		"xargs -I % sed 's/%/x/'\n",
		"xargs -i jq '.{}'\n",
		"xargs awk -f - <<'EOF'\n{ print }\nEOF\n",
		"env -S 'awk {' f\n",
	} {
		if programs := find(t, src); len(programs) != 0 {
			t.Errorf("Find(%q) = %+v, want nothing", src, programs)
		}
	}
}

func TestFindsEveryProgramOfFindExec(t *testing.T) {
	t.Parallel()

	var got []string
	for _, p := range find(t, "find . -exec awk '{print}' {} \\; -exec sed -n '$p' {} \\; -print\n") {
		got = append(got, p.Language.String()+" "+p.Text)
	}
	if want := []string{"awk {print}", "sed $p"}; !slices.Equal(got, want) {
		t.Errorf("programs = %q, want %q", got, want)
	}
}

func TestSkipsUnclosedQuotes(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"awk '{print $1\necho hi\n",
		"jq \".a | \necho x\n",
		"sed -e 'p' -e 's/a/b/\n",
	} {
		if programs := Find(parser.Parse(src, dialect.Bash)); len(programs) != 0 {
			t.Errorf("Find(%q) = %+v, want nothing while the quote is open", src, programs)
		}
	}
}

func errorAt(t *testing.T, src string) (string, parser.Span) {
	t.Helper()

	programs := find(t, src)
	if len(programs) != 1 || programs[0].Result.Error == nil {
		t.Fatalf("Find(%q) = %+v, want one program with an error", src, programs)
	}
	p := programs[0]
	return p.Result.Error.Message, p.Span(p.Result.Error.Start, p.Result.Error.End)
}

func TestErrorPositionsMapToTheShellSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src     string
		at      string
		width   int
		message string
	}{
		{"awk '{ print \"x }' f\n", "\"x }", 4, "unterminated string"},
		{"sed -e 's/a/b' f\n", "s/a", 1, "unterminated `s` command"},
		{"jq -r '.a |' f\n", "' f", 0, "unexpected end of program"},
		{"awk \"{print \\$1 & 1}\" f\n", "&", 1, "unexpected character `&`"},
		{"awk \"{x = \\\"a\\\" & 1}\" f\n", "&", 1, "unexpected character `&`"},
		{"sed -es/a/b/x f\n", "x f", 1, "unknown option to `s`"},
		{"sed --expression='p;k' f\n", "k'", 1, "unknown command `k`"},
		{"sed -e 'p' -e 'k' f\n", "k'", 1, "unknown command `k`"},
		{"sed -e '/x/{' -e 'p' f\n", "{'", 1, "`{` is never closed"},
		{"sed 's/old/'new'/x' f\n", "x'", 1, "unknown option to `s`"},
		{"awk -f - <<'EOF'\n{ print $1\nEOF\n", "{ print", 1, "`{` is never closed"},
		{"sed -f - <<-'EOF'\n\t\tp\n\t\tk\n\tEOF\n", "k\n", 1, "unknown command `k`"},
		{"x=$(jq '.a | | .b' f)\n", "| .b", 1, "unexpected `|`"},
	}
	for _, tt := range tests {
		message, span := errorAt(t, tt.src)
		at := strings.Index(tt.src, tt.at)
		if message != tt.message || span != (parser.Span{Start: at, End: at + tt.width}) {
			t.Errorf("error in %q = %q at %+v, want %q at %d+%d", tt.src, message, span, tt.message, at, tt.width)
		}
	}
}

func TestSpansSplitAcrossScripts(t *testing.T) {
	t.Parallel()

	src := "sed -e 'a\\' -e 'line' f\n"
	p := find(t, src)[0]
	var got []string
	p.Spans(0, len(p.Text), func(s parser.Span) { got = append(got, src[s.Start:s.End]) })
	if want := []string{`a\`, "line"}; !slices.Equal(got, want) {
		t.Errorf("Spans = %q, want %q", got, want)
	}
	var regions []string
	p.Regions(func(s parser.Span) { regions = append(regions, src[s.Start:s.End]) })
	if want := []string{`a\`, "line"}; !slices.Equal(regions, want) {
		t.Errorf("Regions = %q, want %q", regions, want)
	}
}

func TestSpansCoverEscapes(t *testing.T) {
	t.Parallel()

	src := "awk \"{print \\\"a\\\"}\" f\n"
	p := find(t, src)[0]
	tok, ok := p.Result.TokenAt(strings.Index(p.Text, `"a"`))
	if !ok {
		t.Fatalf("no token at the string in %q", p.Text)
	}
	var got []string
	p.Spans(tok.Start, tok.End, func(s parser.Span) { got = append(got, src[s.Start:s.End]) })
	if want := []string{`\"a\"`}; !slices.Equal(got, want) {
		t.Errorf("Spans of %q = %q, want %q", p.Text[tok.Start:tok.End], got, want)
	}
	var regions []string
	p.Regions(func(s parser.Span) { regions = append(regions, src[s.Start:s.End]) })
	if want := []string{`{print \"a\"}`}; !slices.Equal(regions, want) {
		t.Errorf("Regions = %q, want %q", regions, want)
	}
}

func TestHover(t *testing.T) {
	t.Parallel()

	src := "awk '{ print length($0), NR, x }' f | jq 'map(.a) | @base64' | sed p\n"
	programs := find(t, src)
	for word, want := range map[string]string{
		"length":  "length([s])",
		"NR":      "Number of input records",
		"map":     "map(f)",
		"@base64": "Encodes the input string as base64",
	} {
		at := strings.Index(src, word)
		h, ok := HoverAt(programs, at+1)
		if !ok || !strings.Contains(h.Markdown, want) || h.Span != (parser.Span{Start: at, End: at + len(word)}) {
			t.Errorf("HoverAt(%q) = %+v, %v, want %q", word, h, ok, want)
		}
	}
	for _, word := range []string{"print", "x }", ".a", "p\n", "| jq"} {
		if h, ok := HoverAt(programs, strings.Index(src, word)); ok {
			t.Errorf("HoverAt(%q) = %+v, want nothing", word, h)
		}
	}
}
