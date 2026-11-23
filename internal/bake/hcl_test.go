package bake

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func known(text string) *Value { return &Value{Text: text, Known: true} }

func TestHCLReadsTargets(t *testing.T) {
	t.Parallel()

	src := `# a bake file
variable "TAG" {
  default = "1.0"
}

function "tag" {
  params = [name]
  result = "${name}:${TAG}"
}

group "default" {
  targets = ["app", "base"]
}

/* the base
   image */
target "base" {
  context = "base"
  contexts = {
    alpine = "docker-image://alpine:3.20" // pinned
    "src"  = "../src",
    tools: "target:tools"
    dynamic = "docker-image://${REGISTRY}/x"
    "${KEY}" = "skipped"
    computed = tag("x")
  }
}

target "app" {
  inherits   = ["base", "missing"]
  dockerfile = "app.Dockerfile"
  args = {
    NOTE = <<-EOT
      text with } and ${TAG} and "quotes"
      EOT
  }
  tags = [for t in ["a", "b"] : "${t}:${TAG}"]
  labels = { "x" = "a\"b\u00e9" }
}
target "one-line" { context = "x" }
`
	got, err := parseHCL(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []target{
		{Name: "base", Context: known("base"), Contexts: map[string]Value{
			"alpine":   {Text: "docker-image://alpine:3.20", Known: true},
			"src":      {Text: "../src", Known: true},
			"tools":    {Text: "target:tools", Known: true},
			"dynamic":  {},
			"computed": {},
		}},
		{Name: "app", Dockerfile: known("app.Dockerfile"), Inherits: []string{"base", "missing"}},
		{Name: "one-line", Context: known("x")},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("targets (-want +got):\n%s", diff)
	}
}

func TestHCLDecodesStringLiterals(t *testing.T) {
	t.Parallel()

	got, err := parseHCL(`target "t" { context = "a\tb\\c\"d$${e}%%{f}\U0001F600" }`)
	if err != nil {
		t.Fatal(err)
	}
	if want := "a\tb\\c\"d${e}%{f}\U0001F600"; len(got) != 1 || got[0].Context == nil || *got[0].Context != (Value{Text: want, Known: true}) {
		t.Errorf("targets = %+v, want context %q", got, want)
	}
}

func TestHCLToleratesNestedTemplates(t *testing.T) {
	t.Parallel()

	got, err := parseHCL("target \"t\" {\n  context = \"${lookup({a = \"}\"}, \"a\", \"{\")}\"\n  dockerfile = \"D\"\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []target{{Name: "t", Context: &Value{}, Dockerfile: known("D")}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("targets (-want +got):\n%s", diff)
	}
}

func TestHCLRejectsBrokenFiles(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ src, want string }{
		{"target \"t\" {\n  context = \"x\n}\n", "line 2, column 13: the string is never closed"},
		{"target \"t\" {\n  context = \"x\"\n", "line 1, column 12: `{` is never closed"},
		{"target \"t\" {}\n}\n", "line 2, column 1: `}` closes nothing"},
		{"/* open\n", "line 1, column 1: the comment is never closed"},
		{"x = <<EOT\nbody\n", "line 1, column 5: the heredoc is never closed by a line with EOT"},
		{"x = \"${a\"\n", "line 1, column 9: the string is never closed"},
		{"target \"t\" {\n  context = \"\\q\"\n}\n", "line 2, column 14: \\q is no escape sequence"},
		{"\"x\" = 1\n", "line 1, column 1: expected an attribute name or a block type"},
		{"target \"t\"\n", "line 1, column 1: `target` is neither an attribute nor a block"},
	} {
		_, err := parseHCL(tt.src)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("parseHCL(%q) = %v, want %q", tt.src, err, tt.want)
		}
	}
}
