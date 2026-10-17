package nix

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/projection"
)

var environment = directive.Render(directive.Disable, "SC2154") + "\n"

const (
	tool          = "nix"
	strictMode    = "set -o errexit -o nounset -o pipefail\n"
	phaseFunction = "nixPhase"
	shellApp      = "writeShellApplication"
	shellAppText  = "text"
	excludeChecks = "excludeShellChecks"
	noBrace       = -1
)

var (
	derivationBindings = phaseHooks(
		[]string{"Unpack", "Patch", "Configure", "Build", "Check", "Install", "Fixup", "InstallCheck", "Dist"},
		"buildPhase", "configurePhase", "checkPhase", "installPhase", "unpackPhase", "patchPhase", "fixupPhase",
		"installCheckPhase", "distPhase", "buildCommand",
	)
	serviceBindings = []string{"script", "preStart", "postStart", "preStop", "postStop", "reload"}
	bashWriters     = []string{"writeShellScript", "writeShellScriptBin", "writeBash", "writeBashBin"}
	dashWriters     = []string{"writeDash", "writeDashBin"}
	scriptWriters   = []string{"writeScript", "writeScriptBin"}
	commandRunner   = []string{"runCommand", "runCommandLocal", "runCommandCC", "runCommandNoCC", "runCommandNoCCLocal"}
	passThrough     = []string{"mkBefore", "mkAfter", "mkForce", "mkDefault"}
	conditional     = []string{"optionalString", "mkIf", "mkOrder"}
	markers         = map[string]dialect.Dialect{"shell": dialect.Bash, "bash": dialect.Bash, "sh": dialect.Posix, "dash": dialect.Dash, "zsh": dialect.Zsh, "ksh": dialect.Ksh}
)

func phaseHooks(stages []string, names ...string) []string {
	for _, stage := range stages {
		names = append(names, "pre"+stage, "post"+stage)
	}
	return names
}

type frameKind uint8

const (
	rootFrame frameKind = iota
	attrsFrame
	groupFrame
	listFrame
)

var frameOpeners = map[string]frameKind{"{": attrsFrame, "(": groupFrame, "[": listFrame}

func opensFrame(t token) (frameKind, bool) {
	if t.kind != punctToken {
		return rootFrame, false
	}
	kind, ok := frameOpeners[t.text]
	return kind, ok
}

type frame struct {
	open    int
	kind    frameKind
	binding string
	path    string
	owner   string
}

type context struct {
	binding string
	path    string
	brace   int
}

type shellString struct {
	tok        token
	detection  dialect.Detection
	prelude    string
	directive  string
	shebang    bool
	derivation bool
	joined     bool
	context    context
}

type file struct {
	src      string
	tokens   []token
	partner  map[int]int
	contexts map[int]context
	excluded map[int][]string
}

func Project(src string) (*projection.Map, dialect.Detection) {
	f := &file{src: src, tokens: tokenize(src), partner: map[int]int{}, contexts: map[int]context{}, excluded: map[int][]string{}}
	f.scan()
	found := f.shellStrings()
	detection, _ := dialect.Choose(found, func(s shellString) dialect.Detection { return s.detection }, tool)
	b := projection.NewBuilder(src)
	b.Shell(detection)
	for n, group := range groups(found) {
		b.Shell(group[0].detection)
		f.project(b, group, n)
	}
	return b.Map(), detection
}

func groups(found []shellString) [][]shellString {
	var out [][]shellString
	for _, s := range found {
		if n := len(out); n > 0 && s.joined && out[n-1][0].context == s.context {
			out[n-1] = append(out[n-1], s)
			continue
		}
		out = append(out, []shellString{s})
	}
	return out
}

func (f *file) scan() {
	frames := []frame{{open: noBrace, kind: rootFrame}}
	for i, t := range f.tokens {
		top := &frames[len(frames)-1]
		kind, opens := opensFrame(t)
		switch {
		case opens:
			frames = append(frames, frame{open: i, kind: kind, owner: joinPath(top.owner, top.path)})
		case t.is("}") || t.is(")") || t.is("]"):
			if len(frames) > 1 {
				f.partner[i], f.partner[top.open] = top.open, i
				frames = frames[:len(frames)-1]
			}
		case t.is(";"):
			top.binding, top.path = "", ""
		case t.kind == identToken && i+1 < len(f.tokens) && f.tokens[i+1].is("="):
			top.binding, top.path = t.name(), t.text
			if t.name() == excludeChecks && top.kind == attrsFrame {
				f.excluded[top.open] = f.listStrings(i + 2)
			}
		case t.kind == stringToken:
			f.contexts[i] = resolve(frames)
		}
	}
}

func joinPath(owner, path string) string {
	if owner == "" || path == "" {
		return owner + path
	}
	return owner + "." + path
}

func resolve(frames []frame) context {
	c := context{brace: noBrace}
	for _, fr := range slices.Backward(frames) {
		if c.binding == "" {
			c.binding, c.path = fr.binding, joinPath(fr.owner, fr.path)
		}
		switch fr.kind {
		case groupFrame:
			continue
		case attrsFrame:
			c.brace = fr.open
		case listFrame:
			c.binding = ""
		case rootFrame:
		}
		return c
	}
	return c
}

func (f *file) listStrings(i int) []string {
	if i >= len(f.tokens) || !f.tokens[i].is("[") {
		return nil
	}
	var out []string
	for j := i + 1; j < len(f.tokens) && !f.tokens[j].is("]"); j++ {
		if t := f.tokens[j]; t.kind == stringToken && !t.indented {
			out = append(out, strings.Trim(f.src[t.start:t.end], `"`))
		}
	}
	return out
}

func (f *file) callee(open int) string {
	j := open - 1
	if j >= 0 && f.tokens[j].keyword("rec") {
		j--
	}
	if j >= 0 && f.tokens[j].kind == identToken {
		return f.tokens[j].name()
	}
	return ""
}

func (f *file) shellStrings() []shellString {
	var out []shellString
	for i, t := range f.tokens {
		if t.kind != stringToken || f.blank(t) {
			continue
		}
		if s, ok := f.classify(i); ok {
			out = append(out, s)
		}
	}
	return out
}

func (f *file) blank(t token) bool {
	start, end := contentBounds(f.src, t)
	return strings.TrimSpace(f.src[start:end]) == ""
}

func (f *file) classify(i int) (shellString, bool) {
	t := f.tokens[i]
	value, joined := f.valuePosition(i)
	c := f.contexts[i]
	s := shellString{tok: t, joined: joined, context: c}
	marker := strings.TrimSpace(t.comment)
	if d, ok := markers[marker]; ok {
		s.detection = dialect.Detection{Dialect: d, Source: dialect.SourceHostSetting, Evidence: "/* " + marker + " */"}
		return s, true
	}
	if callee, ok := f.applied(i); ok {
		return f.written(s, callee)
	}
	if !value {
		return shellString{}, false
	}
	switch {
	case slices.Contains(derivationBindings, c.binding):
		s.derivation = true
	case c.binding == "shellHook", slices.Contains(serviceBindings, c.binding) && slices.Contains(strings.Split(c.path, "."), "systemd"):
	case c.binding == shellAppText && c.brace != noBrace && f.callee(c.brace) == shellApp:
		s.prelude = strictMode
		if excluded := f.excluded[c.brace]; len(excluded) > 0 {
			s.directive = directive.Render(directive.Disable, strings.Join(excluded, ",")) + "\n"
		}
	default:
		return shellString{}, false
	}
	s.detection = dialect.Detection{Dialect: dialect.Bash, Source: dialect.SourceHostDefault, Evidence: tool}
	return s, true
}

func (f *file) applied(i int) (string, bool) {
	before := i - 1
	if before >= 0 && f.tokens[before].is("}") {
		open := f.partner[before]
		if open >= 2 && f.tokens[open-1].kind == stringToken && slices.Contains(commandRunner, f.tokens[open-2].name()) {
			return f.tokens[open-2].name(), true
		}
		return "", false
	}
	if before >= 1 && f.tokens[before].kind == stringToken && f.tokens[before-1].kind == identToken {
		name := f.tokens[before-1].name()
		if slices.Contains(bashWriters, name) || slices.Contains(dashWriters, name) || slices.Contains(scriptWriters, name) {
			return name, true
		}
	}
	return "", false
}

func (f *file) written(s shellString, callee string) (shellString, bool) {
	switch {
	case slices.Contains(dashWriters, callee):
		s.detection = dialect.Detection{Dialect: dialect.Dash, Source: dialect.SourceHostSetting, Evidence: callee}
	case slices.Contains(scriptWriters, callee):
		first := strings.TrimSpace(firstLine(f.src, s.tok))
		d, ok := dialect.FromShebang(first)
		if !ok {
			return shellString{}, false
		}
		s.detection = dialect.Detection{Dialect: d, Source: dialect.SourceHostSetting, Evidence: first}
		s.shebang = true
	case slices.Contains(commandRunner, callee):
		s.detection = dialect.Detection{Dialect: dialect.Bash, Source: dialect.SourceHostDefault, Evidence: tool}
		s.derivation = true
	default:
		s.detection = dialect.Detection{Dialect: dialect.Bash, Source: dialect.SourceHostDefault, Evidence: tool}
	}
	return s, true
}

func firstLine(src string, t token) string {
	start, end := contentBounds(src, t)
	text := src[start:end]
	if nl := strings.IndexByte(text, '\n'); nl >= 0 && t.indented && strings.TrimSpace(text[:nl]) == "" {
		text = text[nl+1:]
	}
	line, _, _ := strings.Cut(text, "\n")
	return line
}

func (f *file) valuePosition(i int) (bool, bool) {
	for j := i - 1; j >= 0; {
		t := f.tokens[j]
		switch {
		case t.is("+"):
			return true, true
		case t.is("=") || t.keyword("then") || t.keyword("else") || t.keyword("or"):
			return true, false
		case t.is("(") || t.kind == identToken && slices.Contains(passThrough, t.name()):
			j--
			continue
		}
		start := j
		if t.is(")") {
			start = f.partner[j]
		}
		if start < 1 || f.tokens[start-1].kind != identToken || !slices.Contains(conditional, f.tokens[start-1].name()) {
			return false, false
		}
		j = start - 2
	}
	return false, false
}

func contentBounds(src string, t token) (int, int) {
	open, closing := 1, `"`
	if t.indented {
		open, closing = 2, "''"
	}
	end := t.end
	if strings.HasSuffix(src[:end], closing) && end-len(closing) >= t.start+open {
		end -= len(closing)
	}
	return t.start + open, end
}

func (f *file) project(b *projection.Builder, group []shellString, n int) {
	first := group[0]
	opener, end := first.directive+"(\n"+first.prelude, closing
	if first.derivation {
		name := phaseFunction + strconv.Itoa(n)
		opener = environment + first.directive + name + "() {\n" + strictMode + first.prelude
		end = "\n:\n}\n" + name + "\n"
	}
	b.Script()
	last := 0
	for _, s := range group {
		last = f.body(b, s, opener)
		opener = "\n"
	}
	b.Opaque(last, last, end)
}
