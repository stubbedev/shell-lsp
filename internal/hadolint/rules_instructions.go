package hadolint

import (
	"math/big"
	"path"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/dockerfile"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

const (
	scratch      = "scratch"
	latest       = "latest"
	dockerHub    = "docker.io"
	maxPort      = 65535
	rootUser     = "root"
	rootUID      = "0"
	stageCopyKey = "from"
)

var (
	dockerHubAliases = []string{dockerHub, "index.docker.io", "registry-1.docker.io"}
	archiveSuffixes  = []string{".tar", ".tar.gz", ".tgz", ".gz", ".tar.bz2", ".tbz", ".tbz2", ".tb2", ".bz2", ".tar.xz", ".txz", ".xz", ".tar.Z", ".tZ", ".Z", ".lz", ".tlz", ".lzma", ".tar.lzma"}
	remoteSources    = []string{"http://", "https://", "git://", "git@", "ssh://"}
	unpackers        = []string{"gunzip", "gzcat", "zcat", "bunzip2", "bzcat", "unxz", "xzcat", "unlzma", "lzcat", "uncompress"}
)

func (c *checker) instructions() {
	for i, s := range c.stages {
		c.from(s)
		c.user(s, i == len(c.stages)-1)
		for _, in := range s.Instructions {
			c.instruction(s, in)
		}
		c.archives(s)
	}
	c.healthcheck()
}

func (c *checker) expand(in *dockerfile.Instruction, text string) (string, bool) {
	value, known := c.File.Expand(in, text)
	return value, known && value != ""
}

func (c *checker) stageNamed(name string, before int) bool {
	_, ok := c.File.StageNamed(name, before)
	return ok
}

func (c *checker) image(s dockerfile.Stage) (dockerfile.Reference, bool) {
	if s.Image == nil {
		return dockerfile.Reference{}, false
	}
	text, known := c.expand(s.From, s.Image.Text)
	if !known || text == scratch || c.stageNamed(text, s.Index) {
		return dockerfile.Reference{}, false
	}
	return dockerfile.ParseReference(text)
}

func (c *checker) from(s dockerfile.Stage) {
	ref, ok := c.image(s)
	if !ok {
		return
	}
	switch {
	case ref.Tag == "" && ref.Digest == "":
		c.report(lint.DL3006, s.Image.Span, "`%s` has no tag, so each build may start from a different image; name a version, as in `%s:<version>`", s.Image.Text, s.Image.Text)
	case ref.Tag == latest && ref.Digest == "":
		c.report(lint.DL3007, s.Image.Span, "the latest tag moves, so each build may start from a different image; name a version")
	}
	if len(c.Config.TrustedRegistries) == 0 {
		return
	}
	registry := cmpOr(ref.Domain, dockerHub)
	if !slices.ContainsFunc(c.Config.TrustedRegistries, func(trusted string) bool { return trusts(trusted, registry) }) {
		c.report(lint.DL3026, s.Image.Span, "%s is not one of the trusted registries %s lists", registry, c.Config.Path)
	}
}

func cmpOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func trusts(pattern, registry string) bool {
	if slices.Contains(dockerHubAliases, registry) && slices.Contains(dockerHubAliases, pattern) {
		return true
	}
	ok, err := path.Match(pattern, registry)
	return err == nil && ok
}

func (c *checker) user(s dockerfile.Stage, final bool) {
	if !final {
		return
	}
	chain := []dockerfile.Stage{s}
	for p := s.Parent; p >= 0; p = c.stages[p].Parent {
		chain = append(chain, c.stages[p])
	}
	for _, stage := range chain {
		for _, in := range slices.Backward(stage.Instructions) {
			if in.Name != "USER" || len(in.Args) == 0 {
				continue
			}
			text, known := c.expand(in, in.Args[0].Text)
			name, _, _ := strings.Cut(text, ":")
			if known && (name == rootUser || name == rootUID) {
				c.report(lint.DL3002, in.Args[0].Span, "the image runs as root; switch to an unprivileged USER at the end of the stage")
			}
			return
		}
	}
}

func (c *checker) instruction(s dockerfile.Stage, in *dockerfile.Instruction) {
	switch in.Name {
	case "EXPOSE":
		c.expose(in)
	case "ADD":
		c.add(in)
		c.sources(s, in)
	case "COPY":
		c.sources(s, in)
	case "ENV":
		c.env(in)
	}
}

func (c *checker) expose(in *dockerfile.Instruction) {
	limit := big.NewInt(maxPort)
	for _, w := range in.Args {
		text, known := c.expand(in, w.Text)
		if !known {
			continue
		}
		ports, _, _ := strings.Cut(text, "/")
		for p := range strings.SplitSeq(ports, "-") {
			n, ok := new(big.Int).SetString(p, 10)
			if ok && n.Cmp(limit) > 0 {
				c.report(lint.DL3011, w.Span, "port %s is above %d, the highest port TCP and UDP have", p, maxPort)
				break
			}
		}
	}
}

func archive(name string) bool {
	return slices.ContainsFunc(archiveSuffixes, func(suffix string) bool { return strings.HasSuffix(name, suffix) })
}

func remote(source string) bool {
	return slices.ContainsFunc(remoteSources, func(prefix string) bool { return strings.HasPrefix(source, prefix) })
}

type paths struct {
	sources []dockerfile.Word
	dest    dockerfile.Word
}

func (c *checker) paths(in *dockerfile.Instruction) (paths, bool) {
	words := in.Args
	if in.Form == dockerfile.FormJSON {
		words = in.JSON
	}
	if len(words) < 2 {
		return paths{}, false
	}
	return paths{sources: words[:len(words)-1], dest: words[len(words)-1]}, true
}

func heredocSource(w dockerfile.Word) bool { return strings.HasPrefix(w.Text, "<<") }

func (c *checker) add(in *dockerfile.Instruction) {
	p, ok := c.paths(in)
	if !ok {
		return
	}
	for _, src := range p.sources {
		if !remote(src.Text) && !archive(src.Text) && !heredocSource(src) {
			c.report(lint.DL3020, src.Span, "ADD copies `%s` as it is; use COPY, which does only that", src.Text)
			return
		}
	}
}

func (c *checker) sources(s dockerfile.Stage, in *dockerfile.Instruction) {
	p, ok := c.paths(in)
	if !ok {
		return
	}
	dest, known := c.expand(in, p.dest.Text)
	if !known {
		return
	}
	if len(p.sources) > 1 && !strings.HasSuffix(dest, "/") {
		c.report(lint.DL3021, p.dest.Span, "%s with several sources needs a directory destination ending in /, as in `%s/`", in.Name, p.dest.Text)
	}
	if !absolute(dest) && !c.workdirSet(s, in) {
		c.report(lint.DL3045, p.dest.Span, "the destination `%s` is relative, but no WORKDIR is set, so the base image decides where it lands", p.dest.Text)
	}
}

func absolute(p string) bool {
	return strings.HasPrefix(p, "/") || len(p) >= 2 && p[1] == ':' && (p[0]|0x20 >= 'a' && p[0]|0x20 <= 'z')
}

func (c *checker) workdirSet(s dockerfile.Stage, before *dockerfile.Instruction) bool {
	for _, in := range s.Instructions {
		if in == before {
			break
		}
		if in.Name == "WORKDIR" {
			return true
		}
	}
	for p := s.Parent; p >= 0; p = c.stages[p].Parent {
		if slices.ContainsFunc(c.stages[p].Instructions, func(in *dockerfile.Instruction) bool { return in.Name == "WORKDIR" }) {
			return true
		}
	}
	return false
}

func (c *checker) env(in *dockerfile.Instruction) {
	pairs := c.File.Pairs(in)
	for i, p := range pairs {
		if p.Legacy || p.Value == nil {
			continue
		}
		for _, ref := range c.File.References(*p.Value) {
			if slices.ContainsFunc(slices.Delete(slices.Clone(pairs), i, i+1), func(other dockerfile.Pair) bool { return other.Key.Text == ref.Name }) {
				c.report(lint.DL3044, ref.Span, "`$%s` expands to the value from before this ENV, not to the one it sets; move it to an ENV of its own", ref.Name)
			}
		}
	}
}

func (c *checker) archives(s dockerfile.Stage) {
	for _, in := range s.Instructions {
		if in.Name != "COPY" || slices.ContainsFunc(in.Flags, func(f dockerfile.Flag) bool { return f.Name.Text == stageCopyKey }) {
			continue
		}
		p, ok := c.paths(in)
		if !ok {
			continue
		}
		for _, src := range p.sources {
			name := path.Base(src.Text)
			if strings.HasSuffix(p.dest.Text, "/") || !archive(path.Base(p.dest.Text)) {
				if !archive(name) {
					continue
				}
			} else {
				name = path.Base(p.dest.Text)
			}
			if c.extracted(s, in, name) {
				c.report(lint.DL3010, src.Span, "`%s` is copied and then unpacked; ADD unpacks a local archive itself, in one layer", src.Text)
			}
		}
	}
}

func (c *checker) extracted(s dockerfile.Stage, after *dockerfile.Instruction, name string) bool {
	for _, run := range c.Runs {
		if run.Stage != s.Index || run.Trigger != nil || run.Instruction.Span.Start < after.Span.End {
			continue
		}
		for _, cmd := range run.Commands {
			if !unpacks(cmd) {
				continue
			}
			if slices.ContainsFunc(cmd.Args(), func(w Word) bool { return w.Literal && path.Base(w.Text) == name }) {
				return true
			}
		}
	}
	return false
}

func unpacks(cmd Command) bool {
	if slices.Contains(unpackers, cmd.Name()) || cmd.Name() == "unzip" {
		return true
	}
	if cmd.Name() != "tar" {
		return false
	}
	args := cmd.Args()
	if len(args) > 0 && args[0].Literal && !strings.HasPrefix(args[0].Text, "-") && strings.Contains(args[0].Text, "x") {
		return true
	}
	return slices.ContainsFunc(args, func(w Word) bool {
		return w.Literal && (strings.HasPrefix(w.Text, "--extract") || strings.HasPrefix(w.Text, "--get") ||
			strings.HasPrefix(w.Text, "-") && !strings.HasPrefix(w.Text, "--") && strings.Contains(w.Text, "x"))
	})
}

func (c *checker) healthcheck() {
	if len(c.stages) == 0 || !c.enabled(lint.DL3057) {
		return
	}
	final := c.stages[len(c.stages)-1]
	for s := final; ; s = c.stages[s.Parent] {
		if slices.ContainsFunc(s.Instructions, func(in *dockerfile.Instruction) bool { return in.Name == "HEALTHCHECK" }) {
			return
		}
		if s.Parent < 0 {
			break
		}
	}
	c.report(lint.DL3057, final.From.Keyword.Span, "the image has no HEALTHCHECK, so Docker cannot tell whether a container from it works")
}
