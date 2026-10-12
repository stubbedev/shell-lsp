package getopt

import (
	"strings"
	"testing"
)

func TestWords(t *testing.T) {
	t.Parallel()

	documented := map[string]Arity{"-C": Valued, "--git-dir": Valued, "-p": Flag, "--paginate": Flag, "-v": Flag, "-n": Valued, "-netns": Valued}
	arity := func(name string) Arity { return documented[name] }
	for _, tc := range []struct {
		option string
		words  int
		known  bool
	}{
		{"-C", 2, true},
		{"-Cdir", 1, true},
		{"--git-dir", 2, true},
		{"--git-dir=.git", 1, true},
		{"--paginate", 1, true},
		{"--unknown", 1, false},
		{"--unknown=x", 1, true},
		{"-p", 1, true},
		{"-pv", 1, true},
		{"-pvn", 2, true},
		{"-pn5", 1, true},
		{"-netns", 2, true},
		{"-px", 1, false},
		{"-x", 1, false},
	} {
		words, known := Words(tc.option, arity)
		if words != tc.words || known != tc.known {
			t.Errorf("Words(%q) = %d, %t; want %d, %t", tc.option, words, known, tc.words, tc.known)
		}
	}
}

func TestIsOption(t *testing.T) {
	t.Parallel()

	for word, want := range map[string]bool{"-v": true, "--all": true, "-": false, "--": false, "file": false, "": false} {
		if got := IsOption(word); got != want {
			t.Errorf("IsOption(%q) = %t, want %t", word, got, want)
		}
	}
}

func render(p Parsed[string]) string {
	var parts []string
	for _, opt := range p.Options {
		entry := opt.Name
		if opt.Argument != nil {
			entry += "=" + opt.Argument.Word[opt.Argument.Offset:]
		}
		parts = append(parts, entry)
	}
	for _, operand := range p.Operands {
		parts = append(parts, "["+operand+"]")
	}
	if !p.Complete {
		parts = append(parts, "incomplete")
	}
	if p.Missing {
		parts = append(parts, "missing")
	}
	return strings.Join(parts, " ")
}

func TestParse(t *testing.T) {
	t.Parallel()

	foo := map[string]Arity{"foo": Valued, "bar": Flag, "baz": Optional}
	for _, tc := range []struct {
		grammar Grammar
		args    string
		want    string
	}{
		{Grammar{Short: "f:"}, "-f x", "-f=x"},
		{Grammar{Short: "f:"}, "-fx", "-f=x"},
		{Grammar{Short: "fx"}, "-f -x", "-f -x"},
		{Grammar{Short: "f:"}, "-f -x", "-f=-x"},
		{Grammar{Short: "fx:"}, "-fx", "-f -x incomplete missing"},
		{Grammar{Short: "rp:t:"}, "-rp Name -t5 name", "-r -p=Name -t=5 [name]"},
		{Grammar{Short: "ra:"}, "-ar x", "-a=r [x]"},
		{Grammar{Short: "i::"}, "-i cmd", "-i [cmd]"},
		{Grammar{Short: "i::"}, "-iR cmd", "-i=R [cmd]"},
		{Grammar{Long: foo}, "--foo=bar baz", "--foo=bar [baz]"},
		{Grammar{Long: foo}, "--foo bar baz", "--foo=bar [baz]"},
		{Grammar{Long: foo}, "--baz x", "--baz [x]"},
		{Grammar{Long: foo}, "--baz=x", "--baz=x"},
		{Grammar{Long: foo}, "--bar=x", "--bar=x incomplete"},
		{Grammar{Long: foo}, "--foo", "--foo incomplete missing"},
		{Grammar{}, "--other baz", "--other [baz] incomplete"},
		{Grammar{}, "-q -", "-q [-] incomplete"},
		{Grammar{Short: "ab"}, "-a x -b", "-a -b [x]"},
		{Grammar{Short: "ab", StopAtOperand: true}, "-a x -b", "-a [x] [-b]"},
		{Grammar{Short: "ab"}, "-a -- -b", "-a [-b]"},
		{Grammar{Short: "a:b"}, "-a -- -b", "-a=-- -b"},
		{Grammar{Short: "o:c", StopAtOperand: true}, "+o x -c y", "[+o] [x] [-c] [y]"},
		{Grammar{Short: "o:c", StopAtOperand: true, Plus: true}, "+o x -c y", "+o=x -c [y]"},
	} {
		got := render(Parse(tc.grammar, strings.Fields(tc.args), func(s string) (string, bool) { return s, true }))
		if got != tc.want {
			t.Errorf("%+v Parse(%q) = %q, want %q", tc.grammar, tc.args, got, tc.want)
		}
	}
}

func TestParseTakesUnliteralWordsAsOperands(t *testing.T) {
	t.Parallel()

	literal := func(s string) (string, bool) { return s, !strings.HasPrefix(s, "$") }
	got := render(Parse(Grammar{Short: "a:", StopAtOperand: true}, []string{"-a", "$x", "$y", "-b"}, literal))
	if want := "-a=$x [$y] [-b]"; got != want {
		t.Errorf("Parse = %q, want %q", got, want)
	}
}

func TestArity(t *testing.T) {
	t.Parallel()

	g := Grammar{Short: "ab:c::", Long: map[string]Arity{"long": Valued}}
	for option, want := range map[string]Arity{"-a": Flag, "-b": Valued, "-c": Optional, "-d": Unknown, "-:": Unknown, "--long": Valued, "--other": Unknown, "-ab": Unknown} {
		if got := g.Arity(option); got != want {
			t.Errorf("Arity(%q) = %d, want %d", option, got, want)
		}
	}
}

func TestCommandGrammarsAreWellFormed(t *testing.T) {
	t.Parallel()

	for name, g := range Commands {
		seen := map[byte]bool{}
		for _, letter := range []byte(g.Short) {
			if letter == ':' {
				continue
			}
			if seen[letter] {
				t.Errorf("%s: -%c is listed twice in %q", name, letter, g.Short)
			}
			seen[letter] = true
		}
		for long, arity := range g.Long {
			if arity == Unknown {
				t.Errorf("%s: --%s has no arity", name, long)
			}
		}
	}
}
