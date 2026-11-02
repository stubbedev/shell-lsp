package syntax

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	grammarPrefix  = "x: y: z: "
	grammarError   = "ERROR"
	grammarAccepts = "OK"
	nixBinary      = "nix-instantiate"
)

type grammarCase struct{ src, want string }

func grammarCases(t testing.TB) []grammarCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "grammar.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var out []grammarCase
	for line := range strings.Lines(string(raw)) {
		src, want, ok := strings.Cut(strings.TrimSuffix(line, "\n"), "\t")
		if !ok {
			t.Fatalf("grammar line %q has no tab", line)
		}
		out = append(out, grammarCase{src, want})
	}
	return out
}

func wrapped(shape string) string { return "(x: (y: (z: " + shape + ")))" }

func TestGrammar(t *testing.T) {
	t.Parallel()

	for _, c := range grammarCases(t) {
		f := Parse(grammarPrefix + c.src)
		checkTree(t, f)
		switch c.want {
		case grammarError:
			if len(f.Errors) == 0 {
				t.Errorf("Parse(%q) reports no error, nix rejects it", c.src)
			}
			continue
		case grammarAccepts:
		default:
			if got := render(f.Root); got != wrapped(c.want) {
				t.Errorf("Parse(%q) has the shape\n%s\nnix has\n%s", c.src, got, wrapped(c.want))
			}
		}
		if len(f.Errors) > 0 {
			t.Errorf("Parse(%q) reports %+v, nix accepts it", c.src, f.Errors)
		}
	}
}

func nixParse(t testing.TB, src string) (string, string, bool) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), nixBinary, "--parse", "--extra-experimental-features", "pipe-operators", "-E", src)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), stderr.String(), err == nil
}

func requireNix(t testing.TB) {
	t.Helper()
	if _, err := exec.LookPath(nixBinary); err != nil {
		t.Skipf("comparing with nix needs %s on PATH: %v", nixBinary, err)
	}
	if _, stderr, ok := nixParse(t, "1"); !ok {
		t.Skipf("%s cannot parse here: %s", nixBinary, stderr)
	}
}

func TestGrammarAgreesWithNix(t *testing.T) {
	t.Parallel()

	requireNix(t)
	for _, c := range grammarCases(t) {
		out, stderr, ok := nixParse(t, grammarPrefix+c.src)
		switch c.want {
		case grammarError:
			if ok {
				t.Errorf("nix accepts %q, the table says it rejects it", c.src)
			}
		case grammarAccepts:
			if !ok {
				t.Errorf("nix rejects %q: %s", c.src, stderr)
			}
		default:
			if out != wrapped(c.want) {
				t.Errorf("nix prints %q for %q, the table says %q (%s)", out, c.src, wrapped(c.want), stderr)
			}
		}
	}
}

func children(n Node) []Node {
	var out []Node
	first := true
	Inspect(n, func(c Node) bool {
		if first {
			first = false
			return true
		}
		out = append(out, c)
		return false
	})
	return out
}

func checkNode(t *testing.T, text string, n Node) {
	t.Helper()
	s := n.Span()
	if s.Start < 0 || s.Start > s.End || s.End > len(text) {
		t.Fatalf("%T span %+v lies outside the %d bytes of %q", n, s, len(text), text)
	}
	at := s.Start
	for _, c := range children(n) {
		cs := c.Span()
		if cs.Start < at || cs.End > s.End {
			t.Fatalf("%T span %+v of %q is not in order inside %T %+v (after %d)", c, cs, text, n, s, at)
		}
		at = cs.End
		checkNode(t, text, c)
	}
}

func checkTree(t *testing.T, f *File) {
	t.Helper()
	checkNode(t, f.Text, f.Root)
	for _, e := range f.Stray {
		checkNode(t, f.Text, e)
	}
	for _, e := range f.Errors {
		if e.Span.Start < 0 || e.Span.Start > e.Span.End || e.Span.End > len(f.Text) {
			t.Fatalf("error %+v lies outside %q", e, f.Text)
		}
	}
	end := 0
	for _, c := range f.Comments {
		if c.Span.Start < end || c.Span.End > len(f.Text) || c.Body.Start < c.Span.Start || c.Body.End > c.Span.End {
			t.Fatalf("comment %+v of %q is out of order", c, f.Text)
		}
		end = c.Span.End
	}
	checkLossless(t, f)
}

func checkLossless(t *testing.T, f *File) {
	t.Helper()
	var sb strings.Builder
	at := 0
	for _, tok := range f.Tokens {
		if tok.Span.Start != at || tok.Span.End <= at || tok.Span.End > len(f.Text) {
			t.Fatalf("token %+v of %q does not follow offset %d", tok, f.Text, at)
		}
		sb.WriteString(f.Text[tok.Span.Start:tok.Span.End])
		at = tok.Span.End
	}
	if sb.String() != f.Text {
		t.Fatalf("the tokens spell %q, not %q", sb.String(), f.Text)
	}
}

func TestParseForms(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		check     func(e Expr) bool
	}{
		{"an integer", "42", func(e Expr) bool { n, ok := e.(*Int); return ok && n.Text == "42" }},
		{"a float", "1.5e3", func(e Expr) bool { n, ok := e.(*Float); return ok && n.Text == "1.5e3" }},
		{"a uri", "a:b", func(e Expr) bool { n, ok := e.(*URI); return ok && n.Text == "a:b" }},
		{"a relative path", "./a/b", pathOf(RelativePath, "./a/b")},
		{"a bare relative path", "a/b", pathOf(RelativePath, "a/b")},
		{"an absolute path", "/a/b", pathOf(AbsolutePath, "/a/b")},
		{"a home path", "~/a", pathOf(HomePath, "~/a")},
		{"a search path", "<nixpkgs/lib>", pathOf(SearchPath, "nixpkgs/lib")},
		{"an interpolated path", "./a${b}/c", pathOf(RelativePath, "./a/c")},
		{"a rec set", "rec { a = 1; }", func(e Expr) bool { s, ok := e.(*AttrSet); return ok && s.Rec && s.Closed }},
		{"a plain set", "{ a = 1; }", func(e Expr) bool { s, ok := e.(*AttrSet); return ok && !s.Rec }},
		{"a legacy let", "let { a = 1; body = a; }", func(e Expr) bool { _, ok := e.(*LegacyLet); return ok }},
		{"a named lambda", "a: a", func(e Expr) bool {
			l, ok := e.(*Lambda)
			if !ok {
				return false
			}
			p, ok := l.Param.(*IdentParam)
			return ok && p.Name.Name == "a"
		}},
		{"a pattern with an alias first", "a@{ b ? 1, ... }: a", func(e Expr) bool {
			p := patternOf(e)
			return p != nil && p.Alias != nil && p.AliasFirst && p.Ellipsis && len(p.Formals) == 1 && p.Formals[0].Default != nil
		}},
		{"a pattern with an alias last", "{ b }@a: a", func(e Expr) bool {
			p := patternOf(e)
			return p != nil && p.Alias != nil && !p.AliasFirst && !p.Ellipsis && p.Formals[0].Default == nil
		}},
		{"a dotted, quoted and dynamic path", `{ a."b".${c} = 1; }`, func(e Expr) bool {
			s, ok := e.(*AttrSet)
			if !ok || len(s.Bindings) != 1 {
				return false
			}
			a, ok := s.Bindings[0].(*Assign)
			if !ok || len(a.Path) != 3 {
				return false
			}
			_, first := a.Path[0].(*IdentAttr)
			_, second := a.Path[1].(*StringAttr)
			_, third := a.Path[2].(*DynamicAttr)
			return first && second && third
		}},
		{"inherit from", "{ inherit (a) b c; }", func(e Expr) bool {
			s, ok := e.(*AttrSet)
			if !ok {
				return false
			}
			in, ok := s.Bindings[0].(*Inherit)
			return ok && in.From != nil && len(in.Attrs) == 2
		}},
		{"select with a default", "a.b or c", func(e Expr) bool { s, ok := e.(*Select); return ok && s.Default != nil }},
		{"has attribute", "a ? b.c", func(e Expr) bool { h, ok := e.(*HasAttr); return ok && len(h.Path) == 2 }},
	} {
		f := Parse(tt.src)
		checkTree(t, f)
		if len(f.Errors) > 0 {
			t.Errorf("%s: %q reports %+v", tt.name, tt.src, f.Errors)
		}
		if !tt.check(f.Root) {
			t.Errorf("%s: %q parsed as %T", tt.name, tt.src, f.Root)
		}
	}
}

func pathOf(kind PathKind, literal string) func(Expr) bool {
	return func(e Expr) bool {
		p, ok := e.(*Path)
		return ok && p.Kind == kind && literalText(p.Parts) == literal
	}
}

func patternOf(e Expr) *PatternParam {
	l, ok := e.(*Lambda)
	if !ok {
		return nil
	}
	p, _ := l.Param.(*PatternParam)
	return p
}

func TestStringParts(t *testing.T) {
	t.Parallel()

	type part struct {
		kind string
		text string
	}
	for _, tt := range []struct {
		src    string
		kind   StringKind
		closed bool
		want   []part
	}{
		{`"a\n${b}$${c}\${d}"`, Quoted, true, []part{{"literal", "a"}, {"escape", "\n"}, {"value", "${b}"}, {"literal", "$${c}"}, {"escape", "$"}, {"literal", "{d}"}}},
		{`"$"`, Quoted, true, []part{{"literal", "$"}}},
		{`"${"${x}"}"`, Quoted, true, []part{{"value", `${"${x}"}`}}},
		{`"open`, Quoted, false, []part{{"literal", "open"}}},
		{"''a'''b''$c''\\nd${e}f'g''", Indented, true, []part{{"literal", "a"}, {"escape", "''"}, {"literal", "b"}, {"escape", "$"}, {"literal", "c"}, {"escape", "\n"}, {"literal", "d"}, {"value", "${e}"}, {"literal", "f'g"}}},
		{"''$$''", Indented, true, []part{{"literal", "$$"}}},
		{"''open", Indented, false, []part{{"literal", "open"}}},
	} {
		f := Parse(tt.src)
		checkTree(t, f)
		s, ok := f.Root.(*String)
		if !ok || s.Kind != tt.kind || s.Closed != tt.closed {
			t.Errorf("%q parsed as %+v", tt.src, f.Root)
			continue
		}
		var got []part
		for _, p := range s.Parts {
			switch p := p.(type) {
			case *Literal:
				got = append(got, part{"literal", p.Text})
			case *Escape:
				got = append(got, part{"escape", p.Text})
			case *Interpolation:
				got = append(got, part{"value", tt.src[p.Span().Start:p.Span().End]})
			}
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%q has the parts %+v, want %+v", tt.src, got, tt.want)
		}
	}
}

func TestStaticName(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src, name string
		ok        bool
	}{
		{"a", "a", true},
		{`"a b"`, "a b", true},
		{`""`, "", true},
		{`"a\n"`, "", false},
		{`"${b}"`, "", false},
		{"${b}", "", false},
	} {
		src := "{ " + tt.src + " = 1; }"
		set, ok := Parse(src).Root.(*AttrSet)
		if !ok {
			t.Fatalf("%q did not parse as a set", src)
		}
		name, span, ok := StaticName(set.Bindings[0].(*Assign).Path[0])
		if ok != tt.ok || name != tt.name || ok && src[span.Start:span.End] != tt.name {
			t.Errorf("StaticName(%q) = %q, %+v, %v", tt.src, name, span, ok)
		}
	}
}

func TestErrorsArePositioned(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src   string
		at    int
		count int
	}{
		{"{ a = ; }", 6, 1},
		{"{ a = 1 }", 8, 1},
		{"[ 1 ", 0, 1},
		{"( 1", 0, 1},
		{`"abc`, 0, 1},
		{"''abc", 0, 1},
		{"1 + ", 4, 1},
		{"x == y == z", 7, 1},
		{"%", 0, 1},
		{")))))", 0, 1},
		{"{ inherit ${a}; }", 10, 1},
		{"let ${a} = 1; in 1", 4, 1},
		{"./a/", 0, 1},
		{"a |> b <| c", 7, 1},
		{"1 /* abc", 2, 1},
	} {
		f := Parse(tt.src)
		checkTree(t, f)
		if len(f.Errors) != tt.count || f.Errors[0].Span.Start != tt.at {
			t.Errorf("Parse(%q) reports %+v, want %d errors starting at %d", tt.src, f.Errors, tt.count, tt.at)
		}
	}
}

func TestParseKeepsGoingAfterErrors(t *testing.T) {
	t.Parallel()

	src := "{ a = ; b = (1; c = [ 1 ; d = %; e = 1 +; f = g.; h = if i; j = 1\n k.l = 2; 5 = 6; m = 7; }"
	f := Parse(src)
	checkTree(t, f)
	set, ok := f.Root.(*AttrSet)
	if !ok || !set.Closed {
		t.Fatalf("%q parsed as %+v", src, f.Root)
	}
	var got []string
	for _, b := range set.Bindings {
		name, _, _ := StaticName(b.(*Assign).Path[0])
		got = append(got, name)
	}
	if want := []string{"a", "b", "c", "d", "e", "f", "h", "j", "k", "m"}; !slices.Equal(got, want) {
		t.Errorf("bindings = %q, want %q", got, want)
	}
	if len(f.Errors) < 8 {
		t.Errorf("only %d errors: %+v", len(f.Errors), f.Errors)
	}
}

func TestDepthLimitReportsInsteadOfOverflowing(t *testing.T) {
	t.Parallel()

	const n = 100000
	for _, unit := range []string{
		"(", "[ ", "{a=", `"${`, "''${", "./${", "-", "!", "x: ", "let in ", "if a then a else ", "a ++ ", "a + ", "a // ", "a -> ",
		"with a; ", "assert a; ", "a.b or ", "{ a ? ", "a@{ b ? ", "{ ${", "a.${", "a ? ${",
	} {
		f := Parse(strings.Repeat(unit, n))
		checkTree(t, f)
		if !slices.ContainsFunc(f.Errors, func(e Error) bool { return strings.Contains(e.Message, "nested too deeply") }) {
			t.Errorf("%d times %q does not hit the depth limit: %d errors", n, unit, len(f.Errors))
		}
	}
	f := Parse("a" + strings.Repeat(" ? b", n))
	checkTree(t, f)
	if len(f.Errors) != 1 {
		t.Errorf("a long has-attribute chain reports %d errors", len(f.Errors))
	}
}

func TestDeepButReasonableNestingParses(t *testing.T) {
	t.Parallel()

	const n = 1000
	for _, src := range []string{
		strings.Repeat("(", n) + "a" + strings.Repeat(")", n),
		"a" + strings.Repeat(" + a", n),
		strings.Repeat("if a then a else ", n) + "a",
	} {
		if f := Parse(src); len(f.Errors) > 0 {
			t.Errorf("%d levels report %+v", n, f.Errors[0])
		}
	}
}

func TestHostileInputStaysLinear(t *testing.T) {
	t.Parallel()

	const n = 1 << 18
	for _, src := range []string{
		"[ " + strings.Repeat("a+", n),
		strings.Repeat("a+", n),
		"[ " + strings.Repeat("a-b+c.", n),
		"[ " + strings.Repeat("a+a:", n),
		strings.Repeat("/*", n),
		strings.Repeat("<a/", n),
		strings.Repeat("a.", n),
		"[ " + strings.Repeat("f a.b ", n) + "]",
		"{ " + strings.Repeat("a.a.a = 1; ", n) + "}",
		"{ 5 " + strings.Repeat("a.", n),
		"{ " + strings.Repeat("inherit a.", n),
		strings.Repeat(`"`, n),
		strings.Repeat("''", n),
		strings.Repeat("${", n),
		strings.Repeat("}", n),
		strings.Repeat("# c\n", n) + "a",
	} {
		checkLossless(t, Parse(src))
	}
}

func TestCommentBefore(t *testing.T) {
	t.Parallel()

	src := "{ a = /* one */ 1; b = # two\n  2; c = 3; /* three */ }"
	f := Parse(src)
	for _, tt := range []struct {
		before, want string
		ok           bool
	}{
		{"1;", " one ", true},
		{"2;", " two", true},
		{"3;", "", false},
		{"}", " three ", true},
		{"{", "", false},
	} {
		c, ok := f.CommentBefore(strings.Index(src, tt.before))
		if ok != tt.ok || ok && src[c.Body.Start:c.Body.End] != tt.want {
			t.Errorf("CommentBefore(%q) = %+v, %v", tt.before, c, ok)
		}
	}
}

func TestTokensSpellEveryKeywordAndPunctuation(t *testing.T) {
	t.Parallel()

	src := "let a = 1; in # c\n\n  a"
	want := []TokenKind{TokLet, TokWhitespace, TokIdent, TokWhitespace, TokAssign, TokWhitespace, TokInt, TokSemicolon, TokWhitespace, TokIn, TokWhitespace, TokLineComment, TokWhitespace, TokIdent}
	var got []TokenKind
	f := Parse(src)
	for _, tok := range f.Tokens {
		got = append(got, tok.Kind)
	}
	if !slices.Equal(got, want) {
		t.Errorf("token kinds = %v, want %v", got, want)
	}
}

func TestTokensFindCommentsInAwkwardPlaces(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"{ a /*1*/ . /*2*/ b /*3*/ = /*4*/ 1 /*5*/ ; }",
		"x: /*1*/ x",
		"x /*1*/ : x",
		"{ a /*1*/ ? /*2*/ 1 /*3*/ , /*4*/ ... /*5*/ }: a",
		"a /*1*/ @ /*2*/ { /*3*/ }: a",
		"[ /*1*/ a /*2*/ b /*3*/ ]",
		"if /*1*/ a /*2*/ then /*3*/ b /*4*/ else /*5*/ c",
		"f /*1*/ a /*2*/ . /*3*/ b /*4*/ or /*5*/ c",
		"\"${ /*1*/ a /*2*/ }\"",
		"./a${ /*1*/ a /*2*/ }",
		"{ inherit /*1*/ ( /*2*/ a /*3*/ ) /*4*/ b /*5*/ ; }",
	} {
		f := Parse(src)
		checkTree(t, f)
		if len(f.Errors) > 0 {
			t.Errorf("%q reports %+v", src, f.Errors)
		}
		found := 0
		for _, tok := range f.TokensIn(f.Root.Span()) {
			if tok.Kind == TokBlockComment {
				found++
			}
		}
		if want := strings.Count(src, "/*"); found != want || len(f.Comments) != want {
			t.Errorf("%q: TokensIn finds %d of %d comments", src, found, want)
		}
	}
}

func TestTokensKeepStringsVerbatim(t *testing.T) {
	t.Parallel()

	src := "''\n    a '''\\b''\\n ${c}\n\n  d\n''"
	f := Parse(src)
	checkTree(t, f)
	var text []string
	for _, tok := range f.Tokens {
		if tok.Kind == TokStringText || tok.Kind == TokEscape {
			text = append(text, src[tok.Span.Start:tok.Span.End])
		}
	}
	if want := []string{"\n    a ", "'''", "\\b", "''\\n", " ", "\n\n  d\n"}; !slices.Equal(text, want) {
		t.Errorf("string tokens = %q, want %q", text, want)
	}
}

const fuzzInputLimit = 1 << 16

func FuzzParse(f *testing.F) {
	for _, c := range grammarCases(f) {
		f.Add(grammarPrefix + c.src)
	}
	for _, seed := range []string{
		"{ a = ; b = (1; c = [ 1 ; }",
		"''\n  ${a}\n''\t\"${\"",
		"\x00\xff/*",
		strings.Repeat("(", 5000),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > fuzzInputLimit {
			t.Skip("inputs over the limit say nothing the smaller ones do not")
		}
		checkTree(t, Parse(src))
	})
}

func FuzzMatchesNix(f *testing.F) {
	for _, seed := range []string{"x", "x + y", "{ a = 1", "./a/", "x: x", "''${x}''"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		requireNix(t)
		if len(src) > fuzzInputLimit {
			t.Skip("inputs over the limit say nothing the smaller ones do not")
		}
		if strings.ContainsRune(src, 0) {
			t.Skip("a command line argument cannot hold a NUL")
		}
		_, stderr, accepted := nixParse(t, grammarPrefix+src)
		for _, semantic := range []string{"undefined variable", "already defined", "duplicate formal", "invalid integer", "invalid float", "out of range"} {
			if strings.Contains(stderr, semantic) {
				t.Skip("nix rejects this for a reason other than its syntax")
			}
		}
		errs := Parse(grammarPrefix + src).Errors
		if accepted != (len(errs) == 0) {
			t.Errorf("nix accepts %q: %v, we report %+v (%s)", src, accepted, errs, stderr)
		}
	})
}

func TestAttrName(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src, name   string
		named, safe bool
	}{
		{"a", "a", true, true},
		{`"a b"`, "a b", true, true},
		{`""`, "", true, true},
		{`"a\n"`, "a\n", true, true},
		{`"\a"`, "a", true, true},
		{`"${b}"`, "", false, false},
		{`"a${b}"`, "", false, false},
		{"${b}", "", false, false},
		{`${"a"}`, "a", true, true},
		{`${("a")}`, "a", true, true},
		{`${"a${b}"}`, "", false, false},
		{"${''a''}", "a", true, true},
		{"${'' a''}", "a", true, true},
		{"${''a\nb''}", "", false, true},
		{`${"a" + "b"}`, "", false, false},
	} {
		src := "{ " + tt.src + " = 1; }"
		attr := Parse(src).Root.(*AttrSet).Bindings[0].(*Assign).Path[0]
		if name, ok := AttrName(attr); name != tt.name || ok != tt.named {
			t.Errorf("AttrName(%q) = %q, %v", tt.src, name, ok)
		}
		if got := StaticAttr(attr); got != tt.safe {
			t.Errorf("StaticAttr(%q) = %v", tt.src, got)
		}
	}
}
