package justfile

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/projection"
	"github.com/stubbedev/shell-lsp/internal/scan"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

const (
	tool              = "just"
	escapedOpen       = "{{{{"
	interpolationOpen = "{{"
)

var (
	defaultShell     = []string{"sh", "-cu"}
	defaultScript    = []string{"sh", "-eu"}
	windowsShells    = []string{"windows-shell", "windows-powershell"}
	otherSystemsThan = []string{"unix", "linux", "macos", "openbsd", "freebsd", "netbsd", "dragonfly", "android"}
)

type line struct {
	start, end int
}

type script struct {
	recipe    *Recipe
	lines     []line
	whole     bool
	detection dialect.Detection
}

type projector struct {
	file *File
	text string
}

type Analysis struct {
	File      *File
	Map       *projection.Map
	Detection dialect.Detection
	Index     *model.Index
}

func Analyze(text string) Analysis {
	f := Parse(text)
	m, detection, shells := f.project()
	return Analysis{File: f, Map: m, Detection: detection, Index: f.Index(shells)}
}

func Project(text string) (*projection.Map, dialect.Detection) {
	m, detection, _ := Parse(text).project()
	return m, detection
}

func (f *File) project() (*projection.Map, dialect.Detection, []*Recipe) {
	p := projector{file: f, text: f.Text}
	detection, kept := dialect.Choose(p.scripts(), func(s script) dialect.Detection { return s.detection }, tool)
	b := projection.NewBuilder(f.Text)
	shells := make([]*Recipe, len(kept))
	for i, s := range kept {
		p.project(b, s)
		shells[i] = s.recipe
	}
	return b.Map(), detection, shells
}

func (r *Recipe) lines(text string) []line {
	if len(r.Body) == 0 {
		return nil
	}
	var out []line
	scan.Forward(r.Body[0].Span.Start, r.Span.End, func(start int) int {
		end := min(textline.End(text, start), r.Span.End)
		out = append(out, line{start: start, end: end})
		return end + 1
	})
	return out
}

func (f *File) setting(name string) (*Setting, bool) {
	for _, s := range f.Settings() {
		if s.Name.Text == name && !windowsOnly(s.Attributes) {
			return s, true
		}
	}
	return nil, false
}

func (f *File) Enabled(name string) bool {
	s, ok := f.setting(name)
	if !ok {
		return false
	}
	return s.Value == nil || s.Value.Kind == ExprVariable && s.Value.Name.Text == "true"
}

func (f *File) settingValues(name string, fallback []string) ([]string, string, bool) {
	s, ok := f.setting(name)
	if !ok {
		return fallback, "", false
	}
	values, _ := f.Constant(s.Value)
	return values, f.Text[s.Span.Start:s.Span.End], true
}

func attributeNamed(attrs []Attribute, name string) (Attribute, bool) {
	i := slices.IndexFunc(attrs, func(a Attribute) bool { return a.Name.Text == name })
	if i < 0 {
		return Attribute{}, false
	}
	return attrs[i], true
}

func windowsOnly(attrs []Attribute) bool {
	_, windows := attributeNamed(attrs, "windows")
	elsewhere := slices.ContainsFunc(attrs, func(a Attribute) bool { return slices.Contains(otherSystemsThan, a.Name.Text) })
	return windows && !elsewhere
}

func (p projector) scripts() []script {
	var out []script
	for _, r := range p.file.Recipes() {
		if s, ok := p.script(r); ok {
			out = append(out, s)
		}
	}
	return out
}

func (p projector) script(r *Recipe) (script, bool) {
	lines := r.lines(p.text)
	if len(lines) == 0 {
		return script{}, false
	}
	first := strings.TrimLeft(p.text[lines[0].start:lines[0].end], " \t")
	if r.Shebang {
		return detect(script{recipe: r, lines: lines[1:], whole: true}, first, first, dialect.SourceHostSetting)
	}
	if attr, ok := attributeNamed(r.Attributes, "script"); ok {
		if len(attr.Args) > 0 {
			var words []string
			for _, arg := range attr.Args {
				values, _ := p.file.Constant(arg.Value)
				words = append(words, values...)
			}
			return detect(script{recipe: r, lines: lines, whole: true}, "#!"+strings.Join(words, " "), "["+p.text[attr.Span.Start:attr.Span.End]+"]", dialect.SourceHostSetting)
		}
		interpreter, text, set := p.file.settingValues("script-interpreter", defaultScript)
		return detect(script{recipe: r, lines: lines, whole: true}, "#!"+strings.Join(interpreter, " "), text, source(set))
	}
	if p.windowsRecipe(r) {
		return script{}, false
	}
	shell, text, set := p.file.settingValues("shell", defaultShell)
	return detect(script{recipe: r, lines: lines}, "#!"+strings.Join(shell, " "), text, source(set))
}

func source(set bool) dialect.Source {
	if set {
		return dialect.SourceHostSetting
	}
	return dialect.SourceHostDefault
}

func (p projector) windowsRecipe(r *Recipe) bool {
	shell := slices.ContainsFunc(windowsShells, func(name string) bool { _, ok := p.file.setting(name); return ok })
	return windowsOnly(r.Attributes) && shell
}

func detect(s script, shebang, evidence string, src dialect.Source) (script, bool) {
	d, ok := dialect.FromShebang(shebang)
	if !ok {
		return script{}, false
	}
	if src == dialect.SourceHostDefault {
		evidence = tool
	}
	s.detection = dialect.Detection{Dialect: d, Source: src, Evidence: evidence}
	return s, true
}

func (f *File) ExportedTo(r *Recipe) []string {
	var names []string
	all := f.Enabled("export")
	for _, a := range f.Assignments() {
		if a.Export || all {
			names = append(names, a.Name.Text)
		}
	}
	for _, param := range r.Params {
		if param.Export || all {
			names = append(names, param.Name.Text)
		}
	}
	for _, attr := range r.Attributes {
		if attr.Name.Text == "env" && len(attr.Args) > 0 {
			values, _ := f.Constant(attr.Args[0].Value)
			names = append(names, values...)
		}
	}
	slices.Sort(names)
	return slices.DeleteFunc(slices.Compact(names), func(name string) bool { return !shellName(name) })
}

func shellName(name string) bool {
	if name == "" || !isNameStart(name[0]) {
		return false
	}
	return !strings.Contains(name, "-")
}

func (p projector) prelude(r *Recipe) string {
	var sb strings.Builder
	for _, name := range p.file.ExportedTo(r) {
		sb.WriteString(name + "=" + projection.Dynamic + "\n")
	}
	return sb.String()
}

func (p projector) project(b *projection.Builder, s script) {
	b.Region(encoder(s.whole, s.recipe.Indent))
	prelude := p.prelude(s.recipe)
	if s.whole {
		lines := make([]projection.RecipeLine, len(s.lines))
		for i, l := range s.lines {
			start := p.start(l, s.recipe.Indent)
			lines[i] = projection.RecipeLine{Start: l.start, Command: start, End: l.end, Comment: start < l.end && p.text[start] == '#'}
		}
		b.OneShell(prelude, lines, func(l projection.RecipeLine) { p.fragment(b, s.recipe, l.Command, l.End) })
		return
	}
	shells := b.LineShells(prelude)
	sigils := p.file.Sigils()
	for i := 0; i < len(s.lines); {
		n := p.logicalLength(s.lines[i:])
		p.logicalLine(b, shells, s, s.lines[i:i+n], sigils)
		i += n
	}
}

func (f *File) Sigils() string {
	if f.Enabled("guards") {
		return "@-?"
	}
	return "@-"
}

func (p projector) logicalLength(lines []line) int {
	n := 1
	for n < len(lines) && scan.Continued(p.text[lines[n-1].start:lines[n-1].end]) {
		n++
	}
	return n
}

func (p projector) start(l line, indent string) int {
	content := p.text[l.start:l.end]
	return l.start + min(len(indent), len(content)-len(strings.TrimLeft(content, " \t")))
}

func (p projector) logicalLine(b *projection.Builder, shells *projection.LineShells, s script, lines []line, sigils string) {
	first, last := lines[0], lines[len(lines)-1]
	start := p.start(first, s.recipe.Indent)
	command := start
	for command < first.end && strings.IndexByte(sigils, p.text[command]) >= 0 {
		command++
	}
	comment := len(lines) == 1 && command < first.end && p.text[command] == '#'
	shells.Add(projection.RecipeLine{Start: first.start, Command: command, End: last.end, Comment: comment}, func(projection.RecipeLine) {
		from := command
		for i, l := range lines[:len(lines)-1] {
			p.fragment(b, s.recipe, from, l.end-1)
			next := lines[i+1]
			content := p.text[next.start:next.end]
			from = next.start + len(content) - len(strings.TrimLeft(content, " \t"))
			b.Opaque(l.end-1, from, "")
		}
		p.fragment(b, s.recipe, from, last.end)
	})
}

func (p projector) fragment(b *projection.Builder, r *Recipe, start, end int) {
	at := start
	for _, l := range r.Body {
		for _, f := range l.Fragments {
			from, to := max(f.Span.Start, start), min(f.Span.End, end)
			if from >= to {
				continue
			}
			switch f.Kind {
			case FragmentText:
				b.Verbatim(max(from, at), to)
			case FragmentEscape:
				b.Escape(f.Span.Start, f.Span.End, interpolationOpen)
			case FragmentInterpolation:
				b.Placeholder(max(from, at), to)
			}
			at = to
		}
	}
	b.Verbatim(max(at, start), end)
}

func encoder(whole bool, indent string) projection.Encoder {
	return func(host string, start, end int, text string) (string, bool) {
		opensBefore := strings.HasPrefix(text, "{") && strings.HasSuffix(host[:start], "{")
		opensAfter := strings.HasSuffix(text, "{") && strings.HasPrefix(host[end:], "{")
		continues := strings.HasSuffix(text, "\\") && strings.HasPrefix(host[end:], "\n")
		if opensBefore || opensAfter || (!whole && (strings.Contains(text, "\n") || continues)) {
			return "", false
		}
		text = strings.ReplaceAll(text, interpolationOpen, escapedOpen)
		return strings.ReplaceAll(text, "\n", "\n"+indent), true
	}
}
