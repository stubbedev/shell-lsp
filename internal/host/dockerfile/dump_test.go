package dockerfile

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
	"github.com/stubbedev/shell-lsp/internal/host/oracle"
)

var update = flag.Bool("update", false, "rewrite the golden files from the BuildKit oracle")

type dumped struct {
	Name     string    `json:"name"`
	Start    int       `json:"start"`
	End      int       `json:"end"`
	Flags    []string  `json:"flags"`
	JSON     bool      `json:"json"`
	Command  []string  `json:"command"`
	Heredocs []heredoc `json:"heredocs"`
	Inner    string    `json:"inner,omitempty"`
}

type heredoc struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Expand  bool   `json:"expand"`
	Chomp   bool   `json:"chomp"`
}

var wordArguments = map[string]bool{"FROM": true, "ARG": true, "WORKDIR": true, "USER": true, "EXPOSE": true, "STOPSIGNAL": true}

func fromOracle(in oracle.DockerfileInstruction) dumped {
	d := dumped{Name: in.Name, Start: in.Start, End: in.End, Flags: in.Flags, JSON: in.JSON, Heredocs: []heredoc{}}
	name := strings.ToUpper(in.Name)
	if in.JSON || wordArguments[name] || name == "RUN" || name == "CMD" || name == "ENTRYPOINT" {
		d.Command = in.Args
	}
	for _, h := range in.Heredocs {
		d.Heredocs = append(d.Heredocs, heredoc(h))
	}
	if in.Inner != nil {
		d.Inner = in.Inner.Name
	}
	return d
}

func lineOf(text string, offset int) int { return strings.Count(text[:offset], "\n") + 1 }

func ours(f *File) []dumped {
	out := []dumped{}
	for _, in := range f.Instructions {
		d := dumped{Name: in.Keyword.Text, Start: lineOf(f.Text, in.Span.Start), End: lineOf(f.Text, max(in.Span.Start, in.Span.End-1)), Flags: []string{}, JSON: in.Form == FormJSON, Heredocs: []heredoc{}}
		for _, fl := range in.Flags {
			d.Flags = append(d.Flags, in.Logical(f.Text, fl.Span))
		}
		switch {
		case in.Form == FormJSON:
			d.Command = []string{}
			for _, w := range in.Args {
				d.Command = append(d.Command, w.Text)
			}
			for _, w := range in.JSON {
				d.Command = append(d.Command, w.Text)
			}
		case in.Form == FormShell && in.Name != "HEALTHCHECK":
			d.Command = []string{in.Logical(f.Text, in.Command)}
		case wordArguments[in.Name]:
			for _, w := range in.Args {
				d.Command = append(d.Command, in.Logical(f.Text, w.Span))
			}
		}
		for _, h := range in.Heredocs {
			d.Heredocs = append(d.Heredocs, heredoc{Name: h.Name.Text, Content: f.Text[h.Body.Start:h.Body.End], Expand: h.Expand, Chomp: h.Chomp})
		}
		if in.Inner != nil {
			d.Inner = in.Inner.Keyword.Text
		}
		out = append(out, d)
	}
	return out
}

func TestParseAgreesWithBuildKit(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("testdata", "parse", "*.Dockerfile"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no parse fixtures: %v", err)
	}
	if *update {
		hosttest.RequireTool(t, "dockerfile-oracle", hosttest.DockerfileVersion)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			golden := strings.TrimSuffix(path, ".Dockerfile") + ".json"
			if *update {
				r, err := oracle.Dockerfile(t.Context(), path, []string{"PATH=" + os.Getenv("PATH")})
				if err != nil {
					t.Fatal(err)
				}
				want := []dumped{}
				for _, in := range r.Instructions {
					want = append(want, fromOracle(in))
				}
				raw, err := json.MarshalIndent(want, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, append(raw, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v; record it with go test -run TestParseAgreesWithBuildKit -update", err)
			}
			var want []dumped
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			text, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(want, ours(Parse(string(text)))); diff != "" {
				t.Errorf("Parse(%s) differs from BuildKit (-buildkit +ours):\n%s", path, diff)
			}
		})
	}
}
