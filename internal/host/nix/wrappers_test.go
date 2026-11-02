package nix

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

func TestWrappersMakeStringsShell(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, want string
	}{
		{
			name: "a let lambda that inherits into writeShellApplication",
			src:  "let\n  mk = { name, text }: writeShellApplication { inherit name text; };\nin\n  mk { name = \"x\"; text = ''\n    echo wrapped\n  ''; }\n",
			want: "echo wrapped",
		},
		{
			name: "positional arguments",
			src:  "let\n  w = name: text: writeShellScript name text;\nin\n  w \"x\" ''\n    echo wrapped\n  ''\n",
			want: "echo wrapped",
		},
		{
			name: "through a second function",
			src:  "let\n  a = { text }: writeShellApplication { name = \"n\"; inherit text; };\n  b = { body }: a { text = body; };\nin\n  b { body = ''\n    echo wrapped\n  ''; }\n",
			want: "echo wrapped",
		},
		{
			name: "an attribute of a set in the file",
			src:  "let\n  tools = { w = { text }: runCommand \"n\" { } text; };\nin\n  tools.w { text = ''\n    echo wrapped\n  ''; }\n",
			want: "echo wrapped",
		},
		{
			name: "bash -c of an escaped argument",
			src:  "let\n  w = { text }: runCommand \"n\" { } ''\n    bash -c ${lib.escapeShellArg text}\n  '';\nin\n  w { text = ''\n    echo wrapped\n  ''; }\n",
			want: "echo wrapped",
		},
		{
			name: "a script of its own line behind a shebang",
			src:  "let\n  w = { text }: writeTextFile {\n    name = \"n\";\n    text = ''\n      #!${runtimeShell}\n      ${text}\n    '';\n  };\nin\n  w { text = ''\n    echo wrapped\n  ''; }\n",
			want: "echo wrapped",
		},
		{
			name: "an alias of a writer",
			src:  "let\n  w = writeShellScript;\nin\n  w \"n\" ''\n    echo wrapped\n  ''\n",
			want: "echo wrapped",
		},
		{
			name: "an argument only printed is not a script",
			src:  "let\n  w = { text, note }: writeShellScript \"n\" ''\n    ${text}\n    echo ${note}\n  '';\nin\n  w { text = ''\n    echo wrapped\n  ''; note = ''\n    not (shell\n  ''; }\n",
			want: "echo wrapped",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Analyze(tt.src).Map.Text()
			if !strings.Contains(got, tt.want) {
				t.Errorf("projection = %q, want it to hold %q", got, tt.want)
			}
			if strings.Contains(got, "not (shell") {
				t.Errorf("projection %q holds a string that is no script", got)
			}
		})
	}
}

func TestStringsNoWriterReachesStayNix(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src string }{
		{"a function that does something else", "let\n  w = { text }: builtins.trace text 1;\nin\n  w { text = ''\n    echo\n  ''; }\n"},
		{"functions calling each other", "let\n  a = { text }: b { inherit text; };\n  b = { text }: a { inherit text; };\nin\n  a { text = ''\n    echo\n  ''; }\n"},
		{"a function that calls itself", "let\n  a = { text }: a { inherit text; };\nin\n  a { text = ''\n    echo\n  ''; }\n"},
		{"a parameter that may be anything", "f: f { text = ''\n  echo\n''; }\n"},
		{"a set that holds itself", "let\n  s = { w = s.w; };\nin\n  s.w { text = ''\n    echo\n  ''; }\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Analyze(tt.src).Map.Text(); got != "" {
				t.Errorf("projection = %q", got)
			}
		})
	}
}

func TestWrapperEnvReachesTheShell(t *testing.T) {
	t.Parallel()

	src := "let\n  w = { text, env ? { } }: runCommand \"n\" env text;\nin\n  w {\n    env.marker = \"x\";\n    text = ''\n      echo $marker > $out\n    '';\n  }\n"
	a := Analyze(src)
	if len(a.Index.Shells) != 1 {
		t.Fatalf("shells = %+v", a.Index.Shells)
	}
	i := symbolNamed(t, a.Index, "marker")
	if !a.Index.Symbols[i].Exported {
		t.Errorf("env.marker = %+v, want it exported", a.Index.Symbols[i])
	}
}

func TestNixHeredocFindings(t *testing.T) {
	t.Parallel()

	src := "writeShellScript \"gen\" ''\n  cat > out.nix <<EOF\n  let one = ; in one\n  EOF\n''\n"
	syntaxErrors := func(text string) []lint.Finding {
		return slices.DeleteFunc(slices.Clone(Analyze(text).Index.Findings), func(f lint.Finding) bool { return f.Code != lint.CodeNixSyntax })
	}
	found := syntaxErrors(src)
	if len(found) == 0 {
		t.Fatal("a broken heredoc reports nothing")
	}
	if line := strings.Count(src[:found[0].Span.Start], "\n"); line != 2 {
		t.Errorf("the finding is on line %d of the source, want 2", line)
	}
	for _, unmarked := range []string{
		"writeShellScript \"gen\" ''\n  cat > out.txt <<EOF\n  let one = ; in one\n  EOF\n''\n",
		"writeShellScript \"gen\" ''\n  cat > out.nix <<EOF\n  let one = ${x}; in one\n  EOF\n''\n",
	} {
		if got := syntaxErrors(unmarked); len(got) != 0 {
			t.Errorf("%q reports %+v", unmarked, got)
		}
	}
}

func TestReadsLeadIntoALockedDependency(t *testing.T) {
	t.Parallel()

	flake := filepath.Join("/w", FlakeFile)
	self := Analyze("{\n  inputs.nixpkgs.url = \"github:NixOS/nixpkgs\";\n  outputs = { nixpkgs, ... }: {\n    v = nixpkgs.lib.answer;\n  };\n}\n").Index
	source := memorySource{
		filepath.Join("/w", lockFile):          lockedInputs,
		filepath.Join(lockedStore, FlakeFile): "{\n  outputs = { self }: {\n    lib = import ./lib.nix;\n  };\n}\n",
		filepath.Join(lockedStore, "lib.nix"): "{\n  answer = 42;\n}\n",
	}
	p := Link(Request{Self: self, SelfPath: flake, Flake: flake}, source)
	for _, r := range self.Refs {
		if r.Name != "answer" {
			continue
		}
		homes, linked := p.Homes(0, r.Scope, r.Name)
		if !linked || len(homes) != 1 || p.paths[homes[0].File] != filepath.Join(lockedStore, "lib.nix") {
			t.Fatalf("homes of nixpkgs.lib.answer = %+v, %v", homes, linked)
		}
		symbol := symbolNamed(t, p.units[homes[0].File].ix, "answer")
		reason, decided := p.Refusal(homes[0].File, symbol)
		if !decided || !strings.Contains(reason, "dependency") {
			t.Errorf("renaming inside the store: %q, %v", reason, decided)
		}
		return
	}
	t.Fatal("no read of answer")
}

func TestASetThatHoldsItselfIsCut(t *testing.T) {
	t.Parallel()

	src := "{ lib }: lib.nixosSystem {\n  modules = [ (let s = { a = s; }; in s) ];\n}\n"
	a := Analyze(src)
	p := Link(Request{Self: a.Index}, localSource{})
	if reason := p.incomplete(); !strings.Contains(reason, "deeper") {
		t.Errorf("incomplete = %q", reason)
	}
}

func TestRebasedStringsFollowAnEdit(t *testing.T) {
	t.Parallel()

	const before = "a ''one'' b ''two'' c"
	proven := []ProvenString{{Start: strings.Index(before, "''one"), Writer: "w"}, {Start: strings.Index(before, "''two"), Writer: "w"}}
	for _, tt := range []struct {
		name, after string
		want        []int
	}{
		{"nothing changed", before, []int{2, 12}},
		{"before both", "xx a ''one'' b ''two'' c", []int{5, 15}},
		{"inside the first", "a ''onexx'' b ''two'' c", []int{2, 14}},
		{"after both", "a ''one'' b ''two'' cxx", []int{2, 12}},
		{"over the first", "axx b ''two'' c", []int{6}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []int
			for _, s := range RebasedStrings(proven, before, tt.after) {
				got = append(got, s.Start)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("starts = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProjectedStringsKeepTheirWriter(t *testing.T) {
	t.Parallel()

	src := "wrapped ''\n  echo one\n''\n"
	a, ok := ProjectedStrings(src, []ProvenString{{Start: strings.Index(src, "''"), Writer: "writeDash"}})
	if !ok || a.Detection.Dialect != dialect.Dash {
		t.Errorf("detection = %+v, %v", a.Detection, ok)
	}
	if _, ok := ProjectedStrings(src, []ProvenString{{Start: strings.Index(src, "''"), Writer: "writeScript"}}); ok {
		t.Error("writeScript without a shebang projected a string")
	}
}
