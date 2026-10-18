package hadolint

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/host/dockerfile"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

type Input struct {
	File   *dockerfile.File
	Runs   []*Run
	Config Config
}

type checker struct {
	Input
	stages []dockerfile.Stage
	out    []lint.Finding
}

var rules = []func(c *checker){
	(*checker).apt,
	(*checker).apk,
	(*checker).rpm,
	(*checker).languages,
	(*checker).runShell,
	(*checker).instructions,
	(*checker).labels,
	(*checker).pragmas,
}

func Check(in Input) []lint.Finding {
	c := &checker{Input: in, stages: in.File.Stages()}
	for _, rule := range rules {
		rule(c)
	}
	return c.out
}

func (c *checker) enabled(code lint.Code) bool {
	info, _ := lint.Describe(code)
	return c.Config.Enables(code) || slices.Contains(c.Config.Optional, info.Optional) || slices.Contains(c.Config.Optional, directive.All)
}

func (c *checker) report(code lint.Code, span parser.Span, format string, args ...any) {
	c.out = append(c.out, lint.HostFinding(code, span, fmt.Sprintf(format, args...)))
}

func (c *checker) invocations(run *Run, tools ...string) []invocation {
	var out []invocation
	for _, cmd := range run.Commands {
		if inv, ok := invoke(cmd); ok && inv.is(tools...) {
			out = append(out, inv)
		}
	}
	return out
}

type mount struct {
	kind   string
	target string
}

const defaultMount = "bind"

func mounts(in *dockerfile.Instruction) []mount {
	var out []mount
	for _, f := range in.Flags {
		if f.Name.Text != "mount" || f.Value == nil {
			continue
		}
		m := mount{kind: defaultMount}
		for field := range strings.SplitSeq(f.Value.Text, ",") {
			key, value, _ := strings.Cut(field, "=")
			switch strings.ToLower(key) {
			case "type":
				m.kind = value
			case "target", "dst", "destination":
				m.target = value
			}
		}
		out = append(out, m)
	}
	return out
}

func covers(dir, path string) bool {
	dir = strings.TrimSuffix(dir, "/")
	return dir != "" && (path == dir || strings.HasPrefix(path, dir+"/"))
}

func (r *Run) cached(path string) bool {
	for _, m := range mounts(r.Instruction) {
		if (m.kind == "cache" || m.kind == "tmpfs") && covers(m.target, path) {
			return true
		}
	}
	return false
}

func (r *Run) cachedUnder(suffix string) bool {
	for _, m := range mounts(r.Instruction) {
		target := strings.TrimSuffix(m.target, "/")
		if (m.kind == "cache" || m.kind == "tmpfs") && (strings.HasSuffix(target, suffix) || strings.HasSuffix(target, path.Dir(suffix))) {
			return true
		}
	}
	return false
}

func (r *Run) removes(path string) bool {
	for _, cmd := range r.Commands {
		if cmd.Name() != "rm" {
			continue
		}
		for _, arg := range cmd.Args() {
			if !arg.Literal || strings.HasPrefix(arg.Text, "-") {
				continue
			}
			target := strings.TrimSuffix(strings.TrimSuffix(arg.Text, "*"), "/")
			if covers(target, path) {
				return true
			}
		}
	}
	return false
}
