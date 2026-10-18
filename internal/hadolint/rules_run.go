package hadolint

import (
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/getopt"
	"github.com/stubbedev/shell-lsp/internal/host/dockerfile"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

var (
	pointless   = []string{"free", "kill", "mount", "ps", "service", "shutdown", "ssh", "top", "vim"}
	noPipes     = []string{"pwsh", "powershell", "cmd"}
	defaultExec = []string{"/bin/sh", "-c"}
)

var useradd = getopt.Grammar{Short: "b:c:d:De:f:g:G:hk:K:lmMNop:P:rR:s:u:UZ:", Long: merged(
	longs(getopt.Flag, "defaults", "help", "no-log-init", "create-home", "no-create-home", "no-user-group", "non-unique", "system", "user-group", "btrfs-subvolume-home"),
	longs(getopt.Valued, "base-dir", "comment", "home-dir", "expiredate", "inactive", "gid", "groups", "skel", "key", "password", "root", "prefix", "shell", "uid", "selinux-user", "selinux-range"),
)}

var wget = getopt.Grammar{Short: "46a:A:bB:cdD:e:EFhHi:I:kKl:LmNn:o:O:pP:qQ:rR:St:T:U:vVw:xX:", Long: merged(
	longs(getopt.Flag, "quiet", "no-verbose", "verbose", "show-progress", "no-show-progress", "continue", "timestamping", "no-clobber",
		"server-response", "spider", "no-check-certificate", "recursive", "mirror", "background", "debug", "help", "version", "inet4-only", "inet6-only"),
	longs(getopt.Valued, "output-file", "append-output", "output-document", "directory-prefix", "tries", "timeout", "wait", "user-agent",
		"header", "user", "password", "post-data", "post-file", "input-file", "level", "accept", "reject", "domains", "execute", "quota",
		"ca-certificate", "certificate", "private-key", "method", "body-data", "body-file", "referer", "load-cookies", "save-cookies"),
	longs(getopt.Optional, "progress", "backups"),
)}

var ln = getopt.Grammar{Short: "bdfFinLPrsS:t:Tv", Long: merged(
	longs(getopt.Flag, "directory", "force", "interactive", "logical", "no-dereference", "physical", "relative", "symbolic", "no-target-directory", "verbose", "help", "version"),
	longs(getopt.Valued, "suffix", "target-directory"),
	longs(getopt.Optional, "backup"),
)}

func parse(g getopt.Grammar, c Command) getopt.Parsed[Word] {
	return getopt.Parse(g, c.Args(), literal)
}

func (c *checker) runShell() {
	for _, run := range c.Runs {
		for _, cmd := range run.Commands {
			c.command(run, cmd)
		}
		c.pipefail(run)
	}
	c.downloaders()
	c.consecutive()
}

func (c *checker) command(run *Run, cmd Command) {
	for _, w := range cmd.Words[:cmd.Start+1] {
		if w.Literal && path.Base(w.Text) == "sudo" {
			c.report(lint.DL3004, w.Span, "builds run as root already, so sudo is not needed; set USER for the instructions that should not")
		}
	}
	name := cmd.Words[cmd.Start]
	switch cmd.Name() {
	case "cd":
		c.report(lint.DL3003, name.Span, "cd changes the directory for this RUN only; WORKDIR sets it for every later instruction")
	case "useradd":
		c.useradd(run, cmd)
	case "wget":
		c.wget(cmd)
	case "ln":
		c.ln(cmd)
	default:
		if slices.Contains(pointless, cmd.Name()) {
			c.report(lint.DL3001, name.Span, "`%s` is meant for a running system and does nothing useful while an image builds", cmd.Name())
		}
	}
}

const longUID = 100000

func (c *checker) useradd(run *Run, cmd Command) {
	opts := parse(useradd, cmd)
	if opts.Has("-l", "--no-log-init") {
		return
	}
	for _, o := range opts.Options {
		if o.Name != "-u" && o.Name != "--uid" || o.Argument == nil {
			continue
		}
		uid, ok := c.value(run, o.Argument.Word, o.Argument.Offset)
		if n, err := strconv.ParseUint(uid, 10, 64); ok && err == nil && n >= longUID {
			c.report(lint.DL3046, o.Argument.Word.Span, "useradd without -l writes a lastlog entry sized by the UID %s, a huge sparse file in the image; add -l", uid)
		}
	}
}

func (c *checker) value(run *Run, w Word, offset int) (string, bool) {
	if w.Literal {
		return w.Text[offset:], true
	}
	if !w.Expandable {
		return "", false
	}
	raw := c.File.Text[w.Span.Start:w.Span.End]
	value, known := c.File.ExpandDeclared(run.Instruction, raw)
	if !known || value == "" || offset > len(value) {
		return "", false
	}
	return value[offset:], true
}

func (c *checker) wget(cmd Command) {
	opts := parse(wget, cmd)
	quiet := opts.Has("-q", "--quiet", "--no-verbose", "--progress", "-o", "--output-file", "-a", "--append-output")
	for _, o := range opts.Options {
		text, _ := o.Argument.Text(literal)
		quiet = quiet || o.Name == "-n" && strings.Contains(text, "v")
	}
	if !quiet {
		c.report(lint.DL3047, cmd.Words[cmd.Start].Span, "wget draws its progress bar line by line into the build log; add `--progress=dot:giga`, `-nv` or `-q`")
	}
}

const binSh = "/bin/sh"

func (c *checker) ln(cmd Command) {
	opts := parse(ln, cmd)
	var dir *Word
	for _, o := range opts.Options {
		if (o.Name == "-t" || o.Name == "--target-directory") && o.Argument != nil {
			w := o.Argument.Word
			if text, ok := o.Argument.Text(literal); ok {
				w.Text = text
				dir = &w
			}
		}
	}
	targets := opts.Operands
	if dir == nil && len(targets) > 1 {
		last := targets[len(targets)-1]
		dir, targets = &last, targets[:len(targets)-1]
		if dir.Literal && path.Clean(dir.Text) == binSh && len(targets) == 1 {
			c.report(lint.DL4005, dir.Span, "replacing /bin/sh changes the shell of every later RUN behind Docker's back; set SHELL instead")
			return
		}
	}
	if dir == nil || !dir.Literal {
		return
	}
	for _, t := range targets {
		if t.Literal && path.Join(dir.Text, path.Base(t.Text)) == binSh {
			c.report(lint.DL4005, t.Span, "replacing /bin/sh changes the shell of every later RUN behind Docker's back; set SHELL instead")
		}
	}
}

func (c *checker) shellOf(run *Run) []string {
	stage := c.stages[run.Stage]
	shell := defaultExec
	chain := []dockerfile.Stage{stage}
	for p := stage.Parent; p >= 0; p = c.stages[p].Parent {
		chain = append(chain, c.stages[p])
	}
	for _, s := range slices.Backward(chain) {
		for _, in := range s.Instructions {
			if in == run.Reported() {
				return shell
			}
			if in.Name == "SHELL" && in.Form == dockerfile.FormJSON && len(in.JSON) > 0 {
				shell = make([]string, len(in.JSON))
				for i, w := range in.JSON {
					shell[i] = w.Text
				}
			}
		}
	}
	return shell
}

func setsPipefail(words []string) bool {
	for i := 0; i+1 < len(words); i++ {
		if strings.HasPrefix(words[i], "-") && !strings.HasPrefix(words[i], "--") && strings.HasSuffix(words[i], "o") && words[i+1] == "pipefail" {
			return true
		}
	}
	return false
}

func (c *checker) pipefail(run *Run) {
	if len(run.Pipes) == 0 || run.Trigger != nil {
		return
	}
	shell := c.shellOf(run)
	program := strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(shell[0], `\`, "/"))), ".exe")
	if slices.Contains(noPipes, program) || setsPipefail(shell[1:]) {
		return
	}
	pipe := run.Pipes[0]
	for _, cmd := range run.Commands {
		if cmd.Span.Start >= pipe.Start {
			break
		}
		if cmd.Name() == "set" && setsPipefail(texts(cmd.Args())) {
			return
		}
	}
	c.report(lint.DL4006, pipe, "a pipeline succeeds when its last command does, so a failure earlier in the pipe goes unnoticed; set `SHELL [\"/bin/bash\", \"-o\", \"pipefail\", \"-c\"]` or run `set -o pipefail` first")
}

func texts(words []Word) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = w.Text
	}
	return out
}

var downloaders = []string{"curl", "wget"}

func (c *checker) downloaders() {
	first := map[int]string{}
	for _, run := range c.Runs {
		if run.Trigger != nil {
			continue
		}
		for _, cmd := range run.Commands {
			name := cmd.Name()
			if !slices.Contains(downloaders, name) {
				continue
			}
			established, seen := first[run.Stage]
			if !seen {
				first[run.Stage] = name
				continue
			}
			if established != name {
				c.report(lint.DL4001, cmd.Words[cmd.Start].Span, "this stage already downloads with `%s`; use it here too instead of adding `%s`", established, name)
			}
		}
	}
}

func flagKey(in *dockerfile.Instruction, text string) string {
	keys := make([]string, len(in.Flags))
	for i, f := range in.Flags {
		keys[i] = text[f.Span.Start:f.Span.End]
	}
	slices.Sort(keys)
	return strings.Join(keys, "\x00")
}

func (c *checker) consecutive() {
	byInstruction := map[*dockerfile.Instruction]*Run{}
	for _, run := range c.Runs {
		if run.Trigger == nil {
			byInstruction[run.Instruction] = run
		}
	}
	small := func(r *Run) bool {
		named := 0
		for _, cmd := range r.Commands {
			if name := cmd.Name(); name != "" && name != "[" {
				named++
			}
		}
		return r.Read && named <= 1
	}
	for _, s := range c.stages {
		var previous *Run
		for _, in := range s.Instructions {
			run := byInstruction[in]
			if run != nil && previous != nil && small(run) && small(previous) && flagKey(in, c.File.Text) == flagKey(previous.Instruction, c.File.Text) {
				c.report(lint.DL3059, in.Keyword.Span, "this RUN follows another RUN and adds a layer of its own; join them with &&")
			}
			previous = run
		}
	}
}
