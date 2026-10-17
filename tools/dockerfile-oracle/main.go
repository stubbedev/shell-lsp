package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/sourceresolver"
	"github.com/moby/buildkit/frontend/dockerfile/dockerfile2llb"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/opencontainers/go-digest"
)

type heredoc struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Expand  bool   `json:"expand"`
	Chomp   bool   `json:"chomp"`
}

type instruction struct {
	Name     string       `json:"name"`
	Start    int          `json:"start"`
	End      int          `json:"end"`
	Flags    []string     `json:"flags"`
	Args     []string     `json:"args"`
	JSON     bool         `json:"json"`
	Heredocs []heredoc    `json:"heredocs"`
	Inner    *instruction `json:"inner,omitempty"`
}

func dump(n *parser.Node) instruction {
	in := instruction{Name: n.Value, Start: n.StartLine, End: n.EndLine, Flags: n.Flags, JSON: n.Attributes["json"], Args: []string{}, Heredocs: []heredoc{}}
	if in.Flags == nil {
		in.Flags = []string{}
	}
	for arg := n.Next; arg != nil; arg = arg.Next {
		if len(arg.Children) > 0 {
			inner := dump(arg.Children[0])
			in.Inner = &inner
			continue
		}
		in.Args = append(in.Args, arg.Value)
	}
	for _, h := range n.Heredocs {
		in.Heredocs = append(in.Heredocs, heredoc{Name: h.Name, Content: h.Content, Expand: h.Expand, Chomp: h.Chomp})
	}
	return in
}

type finding struct {
	Rule    string `json:"rule"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

type report struct {
	Error        string        `json:"error,omitempty"`
	Line         int           `json:"line,omitempty"`
	Escape       string        `json:"escape"`
	Instructions []instruction `json:"instructions"`
	Warnings     []finding     `json:"warnings"`
	Lint         []finding     `json:"lint"`
}

const version = "dockerfile-oracle 0.34.0"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(version)
		return
	}
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: dockerfile-oracle DOCKERFILE")
		os.Exit(2)
	}
	dt, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	out := run(dt)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func run(dt []byte) report {
	r := report{Instructions: []instruction{}, Warnings: []finding{}, Lint: []finding{}}
	parsed, err := parser.Parse(bytes.NewReader(dt))
	if err != nil {
		r.Error, r.Line = located(err)
		return r
	}
	r.Escape = string(parsed.EscapeToken)
	for _, w := range parsed.Warnings {
		line := 0
		if w.Location != nil {
			line = w.Location.Start.Line
		}
		r.Warnings = append(r.Warnings, finding{Rule: w.URL, Line: line, Message: w.Short})
	}
	for _, n := range parsed.AST.Children {
		r.Instructions = append(r.Instructions, dump(n))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	source := llb.Scratch()
	definition, err := source.Marshal(ctx)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	sourceMap := llb.NewSourceMap(&source, "Dockerfile", "Dockerfile", dt)
	sourceMap.Definition = definition
	results, err := dockerfile2llb.DockerfileLint(ctx, dt, dockerfile2llb.ConvertOpt{SourceMap: sourceMap, MetaResolver: offline{}})
	if err != nil {
		r.Error, r.Line = located(err)
		return r
	}
	for _, w := range results.Warnings {
		line := 0
		if w.Location != nil && len(w.Location.Ranges) > 0 {
			line = int(w.Location.Ranges[0].Start.Line)
		}
		r.Lint = append(r.Lint, finding{Rule: w.RuleName, Line: line, Message: w.Detail})
	}
	if results.Error != nil {
		r.Error = results.Error.Message
		if len(results.Error.Location.Ranges) > 0 {
			r.Line = int(results.Error.Location.Ranges[0].Start.Line)
		}
	}
	return r
}

type offline struct{}

func (offline) ResolveImageConfig(_ context.Context, ref string, _ sourceresolver.Opt) (string, digest.Digest, []byte, error) {
	return ref, digest.FromString(ref), []byte(`{"architecture":"amd64","os":"linux","config":{}}`), nil
}

func located(err error) (string, int) {
	var withLocation interface{ Location() []parser.Range }
	if errors.As(err, &withLocation) && len(withLocation.Location()) > 0 {
		return err.Error(), withLocation.Location()[0].Start.Line
	}
	return err.Error(), 0
}
