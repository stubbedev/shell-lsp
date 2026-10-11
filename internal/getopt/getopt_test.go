package getopt

import "testing"

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
