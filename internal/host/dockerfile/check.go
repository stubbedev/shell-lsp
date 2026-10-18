package dockerfile

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

var errorCodes = map[ErrorKind]lint.Code{
	ErrSyntax:             lint.CodeDockerSyntax,
	ErrDirective:          lint.CodeDockerSyntax,
	ErrUnknownInstruction: lint.CodeDockerUnknownInstruction,
	ErrUnknownFlag:        lint.CodeDockerUnknownFlag,
}

var (
	exactlyOne     = []string{"WORKDIR", "USER"}
	atLeastOne     = []string{"ENV", "ARG", "LABEL", "MAINTAINER"}
	durationFlags  = []string{"interval", "timeout", "start-period", "start-interval"}
	singleton      = []string{"CMD", "ENTRYPOINT", "HEALTHCHECK"}
	notTriggers    = []string{"FROM", "MAINTAINER", "ONBUILD"}
	reservedStages = []string{"scratch", "context"}
	secretWords    = []string{"apikey", "auth", "credential", "credentials", "key", "password", "passwd", "pword", "secret", "token"}
	notSecret      = []string{"public", "file"}
	platformArgs   = []string{"TARGETPLATFORM", "TARGETOS", "TARGETARCH", "TARGETVARIANT", "BUILDPLATFORM", "BUILDOS", "BUILDARCH", "BUILDVARIANT"}
)

var RuleCodes = map[string]lint.Code{
	"StageNameCasing":                 lint.CodeDockerStageNameCasing,
	"FromAsCasing":                    lint.CodeDockerFromAsCasing,
	"NoEmptyContinuation":             lint.CodeDockerEmptyContinuation,
	"ConsistentInstructionCasing":     lint.CodeDockerInstructionCasing,
	"DuplicateStageName":              lint.CodeDockerDuplicateStage,
	"ReservedStageName":               lint.CodeDockerReservedStageName,
	"JSONArgsRecommended":             lint.CodeDockerJSONArgsRecommended,
	"MaintainerDeprecated":            lint.CodeDockerMaintainerDeprecated,
	"UndefinedArgInFrom":              lint.CodeDockerUndefinedVariable,
	"UndefinedVar":                    lint.CodeDockerUndefinedVariable,
	"WorkdirRelativePath":             lint.CodeDockerWorkdirRelative,
	"MultipleInstructionsDisallowed":  lint.CodeDockerMultipleInstructions,
	"LegacyKeyValueFormat":            lint.CodeDockerLegacyKeyValue,
	"SecretsUsedInArgOrEnv":           lint.CodeDockerSecretInArgOrEnv,
	"ExposeProtoCasing":               lint.CodeDockerExposeProtoCasing,
	"ExposeInvalidFormat":             lint.CodeDockerExposeInvalidFormat,
	"InvalidDefaultArgInFrom":         lint.CodeDockerInvalidDefaultArgInFrom,
	"FromPlatformFlagConstDisallowed": lint.CodeDockerConstantPlatform,
	"RedundantTargetPlatform":         lint.CodeDockerRedundantTargetPlatform,
}

type checkDirective struct {
	skip  []lint.Code
	all   bool
	error bool
}

func (f *File) checkDirective() checkDirective {
	var d checkDirective
	for _, dir := range f.Directives {
		if !strings.EqualFold(dir.Name.Text, "check") {
			continue
		}
		for option := range strings.SplitSeq(dir.Value.Text, ";") {
			key, value, _ := strings.Cut(strings.TrimSpace(option), "=")
			switch strings.TrimSpace(key) {
			case "skip":
				for rule := range strings.SplitSeq(value, ",") {
					rule = strings.TrimSpace(rule)
					if rule == "all" {
						d.all = true
					}
					if code, ok := RuleCodes[rule]; ok {
						d.skip = append(d.skip, code)
					}
				}
			case "error":
				d.error = strings.TrimSpace(value) == "true"
			}
		}
	}
	return d
}

func (d checkDirective) apply(findings []lint.Finding) []lint.Finding {
	rule := func(code lint.Code) bool {
		for _, c := range RuleCodes {
			if c == code {
				return true
			}
		}
		return false
	}
	out := findings[:0]
	for _, f := range findings {
		if rule(f.Code) && (d.all || slices.Contains(d.skip, f.Code)) {
			continue
		}
		if rule(f.Code) && d.error {
			f.Severity = lint.SeverityError
		}
		out = append(out, f)
	}
	return out
}

type checker struct {
	f   *File
	out []lint.Finding
}

func (c *checker) report(code lint.Code, span Span, format string, args ...any) {
	c.out = append(c.out, lint.HostFinding(code, span, fmt.Sprintf(format, args...)))
}

func (f *File) check() []lint.Finding {
	c := &checker{f: f}
	for _, e := range f.Errors {
		c.report(errorCodes[e.Kind], e.Span, "%s", e.Message)
	}
	if len(f.Instructions) == 0 {
		c.report(lint.CodeDockerSyntax, Span{}, "the Dockerfile has no instructions")
		return c.out
	}
	for _, in := range f.Preamble() {
		if in.Name != "ARG" {
			c.report(lint.CodeDockerSyntax, in.Keyword.Span, "%s comes before the first FROM, where only ARG may appear", in.Keyword.Text)
		}
	}
	preamble := f.Preamble()
	for _, in := range f.Instructions {
		c.arguments(in, slices.Contains(preamble, in))
	}
	c.casing()
	stages := f.Stages()
	seen := map[string]bool{}
	for _, s := range stages {
		c.stage(s, stages, seen)
	}
	for _, e := range f.Empty {
		c.report(lint.CodeDockerEmptyContinuation, e, "an empty line continues the instruction; BuildKit will treat it as an error")
	}
	return f.checkDirective().apply(c.out)
}

func (c *checker) arguments(in *Instruction, global bool) {
	args := len(in.Args) + len(in.JSON)
	name := in.Name
	switch {
	case name == "FROM" && args != 1 && (args != 3 || !strings.EqualFold(in.Args[1].Text, "AS")):
		c.report(lint.CodeDockerSyntax, in.Keyword.Span, "FROM takes an image, optionally followed by `AS name`")
	case slices.Contains(exactlyOne, name) && args != 1:
		c.report(lint.CodeDockerSyntax, in.Keyword.Span, "%s takes exactly one argument", name)
	case slices.Contains(atLeastOne, name) && args == 0:
		c.report(lint.CodeDockerSyntax, in.Keyword.Span, "%s needs at least one argument", name)
	case (name == "COPY" || name == "ADD") && args+len(in.Heredocs) < 2 && (len(in.Heredocs) <= 0 || args < 2):
		c.report(lint.CodeDockerSyntax, in.Keyword.Span, "%s needs a source and a destination", name)
	case name == "SHELL" && in.Form != FormJSON:
		c.report(lint.CodeDockerSyntax, in.Keyword.Span, "SHELL takes its arguments in JSON form, as in `SHELL [\"/bin/bash\", \"-c\"]`")
	case name == "ONBUILD" && in.Inner != nil && slices.Contains(notTriggers, in.Inner.Name):
		c.report(lint.CodeDockerSyntax, in.Inner.Keyword.Span, "%s cannot be an ONBUILD trigger", in.Inner.Name)
	case name == "HEALTHCHECK" && args > 0 && !strings.EqualFold(c.first(in), "CMD") && !strings.EqualFold(c.first(in), "NONE"):
		c.report(lint.CodeDockerSyntax, in.Args[0].Span, "HEALTHCHECK takes `CMD command` or `NONE`")
	}
	if (name == "ENV" || name == "LABEL") && args > 0 {
		for _, p := range c.f.Pairs(in) {
			switch {
			case p.Legacy && p.Value == nil:
				c.report(lint.CodeDockerSyntax, p.Key.Span, "%s %s needs a value", name, p.Key.Text)
			case p.Legacy:
				c.report(lint.CodeDockerLegacyKeyValue, in.Keyword.Span, "write `%s %s=value` instead of the legacy `%s %s value`", name, p.Key.Text, name, p.Key.Text)
			}
		}
	}
	if (name == "ARG" || name == "ENV") && !global {
		for _, p := range c.f.Pairs(in) {
			if secret(p.Key.Text) {
				c.report(lint.CodeDockerSecretInArgOrEnv, p.Key.Span, "%s `%s` puts sensitive data into the image history; pass it with `RUN --mount=type=secret` instead", name, p.Key.Text)
			}
		}
	}
	if name == "EXPOSE" {
		for _, w := range in.Args {
			port, proto, _ := strings.Cut(w.Text, "/")
			if strings.Contains(port, ":") {
				c.report(lint.CodeDockerExposeInvalidFormat, w.Span, "EXPOSE `%s` maps a host port or address, which EXPOSE cannot; publish it with `docker run -p` instead", w.Text)
			}
			if proto != strings.ToLower(proto) {
				c.report(lint.CodeDockerExposeProtoCasing, w.Span, "the protocol of `%s` should be lowercase", w.Text)
			}
		}
	}
	if name == "MAINTAINER" {
		c.report(lint.CodeDockerMaintainerDeprecated, in.Keyword.Span, "MAINTAINER is deprecated; use `LABEL org.opencontainers.image.authors=...`")
	}
	if name == "HEALTHCHECK" {
		c.healthcheckFlags(in)
	}
}

const minimumInterval = time.Millisecond

func (c *checker) healthcheckFlags(in *Instruction) {
	for _, f := range in.Flags {
		if f.Value == nil || f.Value.Text == "" {
			continue
		}
		name, value := f.Name.Text, f.Value.Text
		switch {
		case slices.Contains(durationFlags, name):
			d, err := time.ParseDuration(value)
			switch {
			case err != nil:
				c.report(lint.CodeDockerSyntax, f.Value.Span, "--%s takes a duration such as 30s or 1m30s: %v", name, err)
			case d != 0 && d < minimumInterval:
				c.report(lint.CodeDockerSyntax, f.Value.Span, "--%s cannot be less than %s", name, minimumInterval)
			}
		case name == "retries":
			n, err := strconv.ParseInt(value, 10, 64)
			switch {
			case err != nil:
				c.report(lint.CodeDockerSyntax, f.Value.Span, "--retries takes a whole number: %v", err)
			case n < 0:
				c.report(lint.CodeDockerSyntax, f.Value.Span, "--retries cannot be negative")
			}
		}
	}
}

func (c *checker) first(in *Instruction) string {
	if len(in.Args) == 0 {
		return ""
	}
	return in.Args[0].Text
}

func secret(name string) bool {
	parts := strings.Split(strings.ToLower(name), "_")
	return slices.ContainsFunc(parts, func(p string) bool { return slices.Contains(secretWords, p) }) &&
		!slices.ContainsFunc(parts, func(p string) bool { return slices.Contains(notSecret, p) })
}

func (c *checker) casing() {
	upper, lower := 0, 0
	var all []*Instruction
	for _, s := range c.f.Stages() {
		all = append(append(all, s.From), s.Instructions...)
	}
	for _, in := range all {
		switch in.Keyword.Text {
		case strings.ToUpper(in.Keyword.Text):
			upper++
		case strings.ToLower(in.Keyword.Text):
			lower++
		}
	}
	majority, want := "uppercase", strings.ToUpper
	if lower > upper {
		majority, want = "lowercase", strings.ToLower
	}
	for _, in := range all {
		if in.Keyword.Text != want(in.Keyword.Text) {
			c.report(lint.CodeDockerInstructionCasing, in.Keyword.Span, "`%s` should be %s, like most instructions here", in.Keyword.Text, majority)
		}
	}
}

func (c *checker) stage(s Stage, stages []Stage, seen map[string]bool) {
	if s.Name != nil {
		key := s.Key()
		switch {
		case s.Name.Text != key:
			c.report(lint.CodeDockerStageNameCasing, s.Name.Span, "stage name `%s` should be lowercase", s.Name.Text)
		}
		if (s.From.Keyword.Text == strings.ToUpper(s.From.Keyword.Text)) != (s.From.Args[1].Text == strings.ToUpper(s.From.Args[1].Text)) {
			c.report(lint.CodeDockerFromAsCasing, s.From.Args[1].Span, "`%s` and `%s` should have the same casing", s.From.Args[1].Text, s.From.Keyword.Text)
		}
		if seen[key] {
			c.report(lint.CodeDockerDuplicateStage, s.Name.Span, "a stage named `%s` comes earlier; stage names should be unique", key)
		}
		if slices.Contains(reservedStages, key) {
			c.report(lint.CodeDockerReservedStageName, s.Name.Span, "`%s` is a reserved name and should not name a stage", key)
		}
		seen[key] = true
	}
	for _, f := range s.From.Flags {
		if f.Name.Text != "platform" || f.Value == nil {
			continue
		}
		switch {
		case f.Value.Text == "$TARGETPLATFORM" || f.Value.Text == "${TARGETPLATFORM}":
			c.report(lint.CodeDockerRedundantTargetPlatform, f.Value.Span, "FROM builds for $TARGETPLATFORM by default, so --platform=%s is redundant", f.Value.Text)
		case !strings.ContainsRune(f.Value.Text, '$'):
			c.report(lint.CodeDockerConstantPlatform, f.Value.Span, "a constant --platform=%s pins the base image to one platform; use a build argument such as $BUILDPLATFORM", f.Value.Text)
		}
	}
	c.fromArgs(s)
	counts := map[string][]*Instruction{}
	workdir := inherits(s, stages, "WORKDIR")
	shell := inherits(s, stages, "SHELL")
	declared := c.f.inheritedNames(s)
	for _, in := range s.Instructions {
		if slices.Contains(singleton, in.Name) {
			counts[in.Name] = append(counts[in.Name], in)
		}
		if in.Name == "WORKDIR" && len(in.Args) == 1 {
			dir, known := c.f.evaluate(in.Args[0].Text, c.f.stageEnvironment(s, stages, in))
			if known && !workdir && !strings.HasPrefix(dir, "/") {
				c.report(lint.CodeDockerWorkdirRelative, in.Args[0].Span, "relative WORKDIR `%s` depends on the working directory of the base image; make it absolute", in.Args[0].Text)
			}
			workdir = true
		}
		if (in.Name == "CMD" || in.Name == "ENTRYPOINT") && in.Form == FormShell && !shell {
			c.report(lint.CodeDockerJSONArgsRecommended, in.Keyword.Span, "the shell form of %s runs the command under `/bin/sh -c`, which does not pass on signals; write it in JSON form", in.Name)
		}
		shell = shell || in.Name == "SHELL"
		c.copyFrom(in, s, stages)
		c.predefined(in, declared)
		declared = append(declared, c.f.declares(in)...)
	}
	for _, name := range singleton {
		list := counts[name]
		for _, in := range list[:max(len(list)-1, 0)] {
			c.report(lint.CodeDockerMultipleInstructions, in.Keyword.Span, "a later %s in this stage replaces this one", name)
		}
	}
}

func inherits(s Stage, stages []Stage, name string) bool {
	for parent := s.Parent; parent >= 0; parent = stages[parent].Parent {
		if slices.ContainsFunc(stages[parent].Instructions, func(in *Instruction) bool { return in.Name == name }) {
			return true
		}
	}
	return false
}

func (c *checker) predefined(in *Instruction, declared []string) {
	if in.Name == "RUN" || in.Name == "CMD" || in.Name == "ENTRYPOINT" || in.Name == "SHELL" || in.Name == "ARG" {
		return
	}
	var words []Word
	words = append(words, in.Args...)
	words = append(words, in.JSON...)
	for _, f := range in.Flags {
		if f.Value != nil {
			words = append(words, *f.Value)
		}
	}
	for _, w := range words {
		for _, r := range c.f.References(w) {
			if _, ok := predefinedArgs[r.Name]; ok && !slices.Contains(declared, r.Name) {
				c.report(lint.CodeDockerUndefinedVariable, r.Span, "`%s` is set for FROM lines only; declare it with `ARG %s` in the stage to use it here", r.Name, r.Name)
			}
		}
	}
}

func (c *checker) fromArgs(s Stage) {
	if s.Image == nil {
		return
	}
	env := c.f.preambleEnvironment()
	image, known := c.f.evaluate(s.Image.Text, env)
	if known && c.f.constantImage(s.Image.Text) {
		switch {
		case strings.TrimSpace(image) == "":
			c.report(lint.CodeDockerSyntax, s.Image.Span, "the image name `%s` expands to nothing", s.Image.Text)
		case !validReference(image):
			c.report(lint.CodeDockerSyntax, s.Image.Span, "`%s` is no valid image reference", image)
		}
	}
	for _, r := range c.f.References(*s.Image) {
		if slices.Contains(platformArgs, r.Name) {
			continue
		}
		switch {
		case !c.f.preambleDeclares(r.Name):
			c.report(lint.CodeDockerUndefinedVariable, r.Span, "FROM uses `%s`, which no ARG before the first FROM declares", r.Name)
		case known && strings.TrimSpace(image) == "" && env[r.Name].value == "":
			c.report(lint.CodeDockerInvalidDefaultArgInFrom, r.Span, "the default of ARG %s leaves the image name empty", r.Name)
		}
	}
}

func (f *File) constantImage(image string) bool {
	for _, r := range substitutions(image, 0, f.Escape) {
		if slices.Contains(platformArgs, r.Name) {
			return false
		}
	}
	return true
}

func lastStage(stages []Stage, key string) int {
	for i, stage := range slices.Backward(stages) {
		if stage.Key() == key {
			return i
		}
	}
	return -1
}

func (c *checker) copyFrom(in *Instruction, s Stage, stages []Stage) {
	if in.Name != "COPY" {
		return
	}
	for _, f := range in.Flags {
		if f.Name.Text != "from" || f.Value == nil || strings.ContainsRune(f.Value.Text, '$') {
			continue
		}
		if n, err := strconv.Atoi(f.Value.Text); err == nil {
			if n >= s.Index {
				c.report(lint.CodeDockerStageUsedBeforeDefinition, f.Value.Span, "--from=%d names a stage that is not defined before this one", n)
			}
			continue
		}
		key := strings.ToLower(f.Value.Text)
		i := lastStage(stages, key)
		switch {
		case i == s.Index:
			c.report(lint.CodeDockerStageUsedBeforeDefinition, f.Value.Span, "stage `%s` copies from itself", f.Value.Text)
		case i > s.Index:
			c.report(lint.CodeDockerStageUsedBeforeDefinition, f.Value.Span, "stage `%s` is defined after the stage that copies from it", f.Value.Text)
		case i < 0 && !validReference(f.Value.Text):
			c.report(lint.CodeDockerSyntax, f.Value.Span, "--from=%s names no stage and is no valid image reference", f.Value.Text)
		case i < 0 && validStageName(f.Value.Text) && !strings.ContainsAny(f.Value.Text, ":/@."):
			c.report(lint.CodeDockerUnknownStage, f.Value.Span, "no stage is named `%s`, so Docker copies from the image of that name", f.Value.Text)
		}
	}
}
