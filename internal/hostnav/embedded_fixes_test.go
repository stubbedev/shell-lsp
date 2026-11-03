package hostnav

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/projection"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func embeddedFixOf(t *testing.T, source, title string) (string, bool) {
	t.Helper()
	tr := newTree(t, map[string]string{"host.nix": source})
	g := tr.group("host.nix")
	var edited []string
	for _, fix := range g.Fixes(parser.Span{End: len(source)}) {
		if strings.HasPrefix(fix.Title, title) {
			edited = append(edited, textedit.Apply(source, fix.Edits))
		}
	}
	switch len(edited) {
	case 0:
		return "", false
	case 1:
		return edited[0], true
	}
	t.Fatalf("%d fixes titled %q in %q: %q", len(edited), title, source, edited)
	return "", false
}

func decodedText(t *testing.T, source string) string {
	t.Helper()
	tr := newTree(t, map[string]string{"host.nix": source})
	hosted := tr.group("host.nix").Self.Native.Nix().Hosted()
	if len(hosted) != 1 {
		t.Fatalf("hosted texts of %q = %+v, want one", source, hosted)
	}
	return hosted[0].Via.Text()
}

func TestFixesInEmbeddedTextReachTheHost(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, source, title, want, decoded string
	}{
		{
			"a misspelt name in a nix file",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  let alpha = 1; in alpho\n''\n",
			"Change to `alpha`",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  let alpha = 1; in alpha\n''\n",
			"let alpha = 1; in alpha\n",
		},
		{
			"a missing argument on one line",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { a }: a + other\n''\n",
			"Add `other` to the arguments",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { a, other }: a + other\n''\n",
			"{ a, other }: a + other\n",
		},
		{
			"a missing argument on its own line keeps both indents",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  {\n    a,\n    b,\n  }:\n  a + b + other\n''\n",
			"Add `other` to the arguments",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  {\n    a,\n    b,\n    other,\n  }:\n  a + b + other\n''\n",
			"{\n  a,\n  b,\n  other,\n}:\na + b + other\n",
		},
		{
			"a home-manager file",
			"{\n  home.file.\"x.nix\".text = ''\n    let alpha = 1; in alpho\n  '';\n}\n",
			"Change to `alpha`",
			"{\n  home.file.\"x.nix\".text = ''\n    let alpha = 1; in alpha\n  '';\n}\n",
			"let alpha = 1; in alpha\n",
		},
		{
			"a quoted string takes the host's escapes",
			"{ writeText }:\nwriteText \"x.nix\" \"{ a = https://example.org; }\"\n",
			"Quote",
			"{ writeText }:\nwriteText \"x.nix\" \"{ a = \\\"https://example.org\\\"; }\"\n",
			"{ a = \"https://example.org\"; }",
		},
		{
			"a removed line leaves no indentation behind",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  let\n    a = 1;\n    b = 2;\n  in\n  b\n''\n",
			"Remove `a`",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  let\n    b = 2;\n  in\n  b\n''\n",
			"let\n  b = 2;\nin\nb\n",
		},
		{
			"a removal across indented lines",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  let\n    a = [\n      1\n    ];\n    b = 2;\n  in\n  b\n''\n",
			"Remove `a`",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  let\n    b = 2;\n  in\n  b\n''\n",
			"let\n  b = 2;\nin\nb\n",
		},
		{
			"beside an antiquotation",
			"{ writeText, v }:\nwriteText \"x.nix\" ''\n  let alpha = ${v}; in alpho\n''\n",
			"Change to `alpha`",
			"{ writeText, v }:\nwriteText \"x.nix\" ''\n  let alpha = ${v}; in alpha\n''\n",
			"let alpha = __nix__; in alpha\n",
		},
		{
			"a justfile dependency",
			"{ writeText }:\nwriteText \"justfile\" ''\n  deploy: buil\n    echo deploy\n  build:\n    echo build\n''\n",
			"Change to `build`",
			"{ writeText }:\nwriteText \"justfile\" ''\n  deploy: build\n    echo deploy\n  build:\n    echo build\n''\n",
			"deploy: build\n  echo deploy\nbuild:\n  echo build\n",
		},
		{
			"a new recipe goes inside the string",
			"{ writeText }:\nwriteText \"justfile\" ''\n  deploy: missing\n    echo deploy\n''\n",
			"Add recipe `missing`",
			"{ writeText }:\nwriteText \"justfile\" ''\n  deploy: missing\n    echo deploy\n\n  missing:\n''\n",
			"deploy: missing\n  echo deploy\n\nmissing:\n",
		},
		{
			"a new recipe in a quoted string",
			"{ writeText }:\nwriteText \"justfile\" \"deploy: missing\\n  echo deploy\\n\"\n",
			"Add recipe `missing`",
			"{ writeText }:\nwriteText \"justfile\" \"deploy: missing\\n  echo deploy\\n\nmissing:\n\"\n",
			"deploy: missing\n  echo deploy\n\nmissing:\n",
		},
		{
			"a makefile variable",
			"{ writeText }:\nwriteText \"Makefile\" ''\n  COMPILER := cc\n  all:\n  \t$(COMPILEX) a.c\n''\n",
			"Change to `COMPILER`",
			"{ writeText }:\nwriteText \"Makefile\" ''\n  COMPILER := cc\n  all:\n  \t$(COMPILER) a.c\n''\n",
			"COMPILER := cc\nall:\n\t$(COMPILER) a.c\n",
		},
		{
			"a makefile recipe prefix",
			"{ writeText }:\nwriteText \"Makefile\" ''\n  all:\n  \ttrue\n      echo spaces\n''\n",
			"Start the recipe line with a tab",
			"{ writeText }:\nwriteText \"Makefile\" ''\n  all:\n  \ttrue\n  \techo spaces\n''\n",
			"all:\n\ttrue\n\techo spaces\n",
		},
		{
			"a makefile phony target",
			"{ writeText }:\nwriteText \"Makefile\" ''\n  all:\n  \ttrue\n  clean:\n  \trm -f app\n''\n",
			"Add `clean` to .PHONY",
			"{ writeText }:\nwriteText \"Makefile\" ''\n  all:\n  \ttrue\n  .PHONY: clean\n  clean:\n  \trm -f app\n''\n",
			"all:\n\ttrue\n.PHONY: clean\nclean:\n\trm -f app\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, offered := embeddedFixOf(t, tt.source, tt.title)
			if !offered {
				tr := newTree(t, map[string]string{"host.nix": tt.source})
				t.Fatalf("no fix %q; findings %+v", tt.title, tr.group("host.nix").Findings())
			}
			if got != tt.want {
				t.Errorf("edited host\n got %q\nwant %q", got, tt.want)
			}
			if decoded := decodedText(t, got); decoded != tt.decoded {
				t.Errorf("the string now holds\n got %q\nwant %q", decoded, tt.decoded)
			}
		})
	}
}

func TestAFixThatCannotBeMappedIsWithheld(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, source, title string
		code                lint.Code
	}{
		{
			"its edit covers an antiquotation",
			"{ writeText, v }:\nwriteText \"x.nix\" ''\n  let\n    a = ${v};\n  in\n  1\n''\n",
			"Remove `a`",
			lint.CodeNixUnusedLet,
		},
		{
			"removing the shallowest line would change what the others hold",
			"{ writeText }:\nwriteText \"x.nix\" ''\n    let\n  a = 1;\n    in\n    2\n''\n",
			"Remove `a`",
			lint.CodeNixUnusedLet,
		},
		{
			"its new text would write a stand-in into the host",
			"{ writeText, v }:\nwriteText \"x.nix\" ''\n  let ${v} = 1; in __nix_\n''\n",
			"Change to",
			lint.CodeNixUndefinedVariable,
		},
		{
			"creating a file beside a virtual one",
			"{ writeText }:\nwriteText \"justfile\" ''\n  import 'missing.just'\n  all:\n    echo hi\n''\n",
			"Create",
			lint.CodeJustMissingFile,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := newTree(t, map[string]string{"host.nix": tt.source})
			findings := tr.group("host.nix").Findings()
			if !slices.ContainsFunc(findings, func(f lint.Finding) bool { return f.Code == tt.code }) {
				t.Fatalf("no %s to withhold a fix for: %+v", tt.code, findings)
			}
			if got, offered := embeddedFixOf(t, tt.source, tt.title); offered {
				t.Errorf("fix %q was offered and gives %q", tt.title, got)
			}
		})
	}
}

func TestHostFixesAreNotAppliedToEmbeddedFindings(t *testing.T) {
	t.Parallel()

	source := "{ writeText }:\nwriteText \"justfile\" ''\n  deploy: missing\n    echo deploy\n''\n"
	got, offered := embeddedFixOf(t, source, "Add recipe")
	if !offered {
		t.Fatal("no fix adds the recipe")
	}
	if !strings.HasSuffix(got, "''\n") {
		t.Errorf("the recipe landed outside the string: %q", got)
	}
}

func TestFixAllReachesEmbeddedText(t *testing.T) {
	t.Parallel()

	source := "{ writeText }:\nwriteText \"x.nix\" ''\n  let\n    a = 1;\n    b = 2;\n  in\n  b\n''\n"
	tr := newTree(t, map[string]string{"host.nix": source})
	want := "{ writeText }:\nwriteText \"x.nix\" ''\n  let\n    b = 2;\n  in\n  b\n''\n"
	if got := textedit.Apply(source, lint.FindingsSafeEdits(tr.group("host.nix").Findings())); got != want {
		t.Errorf("fix-all\n got %q\nwant %q", got, want)
	}
}

func TestFixesCrossEveryHostString(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, source, title, want string
	}{
		{
			"a nix file in a nix file",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { writeText }:\n  writeText \"y.nix\" \"let alpha = 1; in alpho\"\n''\n",
			"Change to `alpha`",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { writeText }:\n  writeText \"y.nix\" \"let alpha = 1; in alpha\"\n''\n",
		},
		{
			"a closing delimiter keeps its own column",
			"{\n  home.file.\"justfile\".text = ''\n    deploy: missing\n      echo deploy\n  '';\n}\n",
			"Add recipe `missing`",
			"{\n  home.file.\"justfile\".text = ''\n    deploy: missing\n      echo deploy\n\n    missing:\n  '';\n}\n",
		},
		{
			"the last line of a string that ends without a newline",
			"{ writeText }:\nwriteText \"justfile\" ''\n  deploy: missing\n    echo deploy''\n",
			"Add recipe `missing`",
			"{ writeText }:\nwriteText \"justfile\" ''\n  deploy: missing\n    echo deploy\n  \n  missing:\n''\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, offered := embeddedFixOf(t, tt.source, tt.title)
			if !offered {
				t.Fatalf("no fix %q", tt.title)
			}
			if got != tt.want {
				t.Errorf("edited host\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestFixesReachTheSecondLevel(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, source, title, want string
	}{
		{
			"a recipe name in a justfile written by a nix file",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { writeText }:\n  writeText \"justfile\" '''\n    deploy: buil\n      echo deploy\n    build:\n      echo build\n  '''\n''\n",
			"Change to `build`",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { writeText }:\n  writeText \"justfile\" '''\n    deploy: build\n      echo deploy\n    build:\n      echo build\n  '''\n''\n",
		},
		{
			"a new recipe keeps both indents",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { writeText }:\n  writeText \"justfile\" \"deploy: missing\\n\"\n''\n",
			"Add recipe `missing`",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { writeText }:\n  writeText \"justfile\" \"deploy: missing\\n\n  missing:\n  \"\n''\n",
		},
		{
			"a makefile target through a quoted string",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { writeText }:\n  writeText \"Makefile\" \".PHONY: clea\\nclean:\\n\\ttrue\\n\"\n''\n",
			"Change to `clean`",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { writeText }:\n  writeText \"Makefile\" \".PHONY: clean\\nclean:\\n\\ttrue\\n\"\n''\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, offered := embeddedFixOf(t, tt.source, tt.title)
			if !offered {
				tr := newTree(t, map[string]string{"host.nix": tt.source})
				t.Fatalf("no fix %q; findings %+v", tt.title, tr.group("host.nix").Findings())
			}
			if got != tt.want {
				t.Errorf("edited host\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestHostedTextsThroughTwoLevelsKeepTheirText(t *testing.T) {
	t.Parallel()

	source := "{ writeText, v }:\nwriteText \"x.nix\" ''\n  { writeText }:\n  writeText \"justfile\" '''\n    x := \"''${v}\"\n    deploy:\n      echo ${v} \\$x\n  '''\n''\n"
	tr := newTree(t, map[string]string{"host.nix": source})
	hosted := tr.group("host.nix").Self.Native.Nix().Hosted()
	if len(hosted) != 2 {
		t.Fatalf("hosted = %+v, want the nix file and its justfile", hosted)
	}
	if want := "x := \"__nix__\"\ndeploy:\n  echo __nix__ \\$x\n"; hosted[1].Via.Text() != want {
		t.Errorf("text = %q, want %q", hosted[1].Via.Text(), want)
	}
	at := strings.Index(source, "deploy:")
	inner, inside := hosted[1].Via.Offset(projection.HostOffset(at))
	if !inside || !strings.HasPrefix(hosted[1].Via.Text()[inner:], "deploy:") {
		t.Errorf("host offset of deploy maps to %d (inside %v)", inner, inside)
	}
	if start, _ := hosted[1].Via.HostSpan(int(inner), int(inner)+6); start != at {
		t.Errorf("deploy maps back to %d, want %d", start, at)
	}
}

func TestRefactoringsReachNixInAString(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, source, selected, title, want string
	}{
		{
			"a string rewrite one level down",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { a = \"one\"; }\n''\n",
			"one",
			"Rewrite as an indented string in x.nix",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { a = '''one'''; }\n''\n",
		},
		{
			"a string rewrite two levels down",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { writeText }:\n  writeText \"y.nix\" '''\n    { a = \"one\"; }\n  '''\n''\n",
			"one",
			"Rewrite as an indented string in y.nix",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { writeText }:\n  writeText \"y.nix\" '''\n    { a = ''''one''''; }\n  '''\n''\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := newTree(t, map[string]string{"host.nix": tt.source})
			at := strings.Index(tt.source, tt.selected)
			var titles []string
			doc := tr.group("host.nix").Self.Native.Nix()
			for _, r := range doc.HostedRefactorings(parser.Span{Start: at, End: at}) {
				titles = append(titles, r.Title)
				if r.Title == tt.title {
					if got := textedit.Apply(tt.source, r.Edits); got != tt.want {
						t.Errorf("edited host\n got %q\nwant %q", got, tt.want)
					}
					return
				}
			}
			t.Errorf("no refactoring %q among %q", tt.title, titles)
		})
	}
}

func TestFixAllSetsThatEmptyTheShallowestLinesAreWithheld(t *testing.T) {
	t.Parallel()

	source := "{ writeText }:\nwriteText \"x.nix\" ''\n    let\n  a = 1;\n  b = 2;\n    in\n    2\n''\n"
	tr := newTree(t, map[string]string{"host.nix": source})
	findings := tr.group("host.nix").Findings()
	if got := textedit.Apply(source, lint.FindingsSafeEdits(findings)); got != source {
		t.Errorf("fix-all re-indented the string: %q", got)
	}
	for _, name := range []string{"a", "b"} {
		if _, offered := embeddedFixOf(t, source, "Remove `"+name+"`"); !offered {
			t.Errorf("removing %s alone keeps the string's indent, yet it was not offered", name)
		}
	}
}

func TestEmbeddedSymbolsAnswerAsTheirOwnLanguage(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, source, at, newName, want string
	}{
		{
			"a make variable",
			"{ writeText }:\nwriteText \"Makefile\" ''\n  CC := gcc\n  all:\n  \t$(CC) -o app main.c\n''\n",
			"CC :=", "COMPILER",
			"{ writeText }:\nwriteText \"Makefile\" ''\n  COMPILER := gcc\n  all:\n  \t$(COMPILER) -o app main.c\n''\n",
		},
		{
			"a just variable",
			"{ writeText }:\nwriteText \"justfile\" ''\n  version := \"1\"\n  show:\n    echo {{version}}\n''\n",
			"version :=", "release",
			"{ writeText }:\nwriteText \"justfile\" ''\n  release := \"1\"\n  show:\n    echo {{release}}\n''\n",
		},
		{
			"a docker stage",
			"{ writeText }:\nwriteText \"Dockerfile\" ''\n  FROM alpine:3 AS base\n  FROM debian:12\n  COPY --from=base /a /b\n''\n",
			"base\n", "first",
			"{ writeText }:\nwriteText \"Dockerfile\" ''\n  FROM alpine:3 AS first\n  FROM debian:12\n  COPY --from=first /a /b\n''\n",
		},
		{
			"a recipe two levels down",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { writeText }:\n  writeText \"justfile\" '''\n    build:\n      echo build\n    deploy: build\n      echo deploy\n  '''\n''\n",
			"build:", "compile",
			"{ writeText }:\nwriteText \"x.nix\" ''\n  { writeText }:\n  writeText \"justfile\" '''\n    compile:\n      echo build\n    deploy: compile\n      echo deploy\n  '''\n''\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := newTree(t, map[string]string{"host.nix": tt.source})
			g := tr.group("host.nix")
			sym, ok := g.At(strings.Index(tt.source, tt.at))
			if !ok {
				t.Fatalf("no symbol at %q", tt.at)
			}
			occs, err := g.Rename(sym, tt.newName)
			if err != nil {
				t.Fatalf("rename: %v", err)
			}
			var edits []textedit.Edit
			for _, o := range occs {
				edits = append(edits, o.Edits(tt.newName)...)
			}
			if got := textedit.Apply(tt.source, edits); got != tt.want {
				t.Errorf("renamed\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestEmbeddedHoverAnswersFromTheEmbeddedLanguage(t *testing.T) {
	t.Parallel()

	source := "{ writeText, v }:\nwriteText \"Dockerfile\" ''\n  FROM alpine:3\n  ARG VERSION=${v}\n  RUN echo $VERSION\n''\n"
	tr := newTree(t, map[string]string{"host.nix": source})
	g := tr.group("host.nix")
	keyword, ok := g.Hover(strings.Index(source, "FROM"))
	if !ok || !strings.Contains(keyword.Markdown, "FROM") {
		t.Errorf("hover on FROM = %+v, %v", keyword, ok)
	}
	if want := strings.Index(source, "FROM"); keyword.Span.Start != want {
		t.Errorf("hover span starts at %d, want %d", keyword.Span.Start, want)
	}
	variable, ok := g.Hover(strings.Index(source, "VERSION="))
	if !ok || strings.Contains(variable.Markdown, "__nix__") {
		t.Errorf("hover on VERSION shows the stand-in: %+v, %v", variable, ok)
	}
}

func TestARenameThroughAHoleIsRefused(t *testing.T) {
	t.Parallel()

	source := "{ writeText, v }:\nwriteText \"Makefile\" ''\n  ${v} := 1\n  CC := gcc\n  all:\n  \t$(CC) -o app main.c\n''\n"
	tr := newTree(t, map[string]string{"host.nix": source})
	g := tr.group("host.nix")
	sym, ok := g.At(strings.Index(source, "CC :="))
	if !ok {
		t.Fatal("no symbol at CC")
	}
	if _, err := g.Rename(sym, "__nix__"); err == nil {
		t.Error("a new name that spells the stand-in was accepted")
	}
}
