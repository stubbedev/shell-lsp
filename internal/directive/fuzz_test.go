package directive

import "testing"

func checkDirective(t *testing.T, line string, d Directive) {
	t.Helper()

	previousEnd := 0
	for _, pair := range d.Pairs {
		if pair.KeyOffset < previousEnd || pair.KeyOffset >= pair.ValueOffset || pair.ValueOffset > pair.ValueEnd || pair.ValueEnd > len(line) {
			t.Fatalf("pair %+v of %q has offsets out of order", pair, line)
		}
		if line[pair.KeyOffset:pair.ValueOffset-1] != pair.Name || len(pair.Value) > pair.ValueEnd-pair.ValueOffset {
			t.Fatalf("pair %+v does not match its text in %q", pair, line)
		}
		previousEnd = pair.ValueEnd
	}
	if d.Problem != nil && (d.Problem.Offset < previousEnd || d.Problem.Offset > len(line)) {
		t.Fatalf("problem %+v of %q is out of bounds", *d.Problem, line)
	}
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{"# shellcheck disable=SC2034", `# shellcheck source="a b".sh shell=bash`, "#shellcheck disable", "# shellcheck x='", "disable=SC1000,SC2000 enable=all", `source-path="a"'b'c`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, line string) {
		if d, ok := Parse(line); ok {
			checkDirective(t, line, d)
		}
		if d, ok := ParseSettings(line); ok {
			checkDirective(t, line, d)
		}
	})
}
