package justfile

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/projection"
)

const (
	tool   = "just"
	sigils = "@-"
)

var (
	keywords          = []string{"alias", "export", "import", "mod", "set", "unexport"}
	defaultShell      = []string{"sh", "-cu"}
	defaultScript     = []string{"sh", "-eu"}
	windowsShells     = []string{"windows-shell", "windows-powershell"}
	otherSystemsThan  = []string{"unix", "linux", "macos", "openbsd", "freebsd"}
	escapedOpen       = "{{{{"
	interpolationOpen = "{{"
)

type line struct {
	start, end int
}

type setting struct {
	values []string
	text   string
}

type recipe struct {
	attributes []string
	params     []string
	exported   []string
	indent     string
	body       []line
}

type script struct {
	recipe    recipe
	lines     []line
	whole     bool
	detection dialect.Detection
}

type parser struct {
	text      string
	lines     []line
	settings  map[string]setting
	exported  []string
	variables []string
	recipes   []recipe
}

func Project(text string) (*projection.Map, dialect.Detection) {
	p := &parser{text: text, lines: splitLines(text), settings: map[string]setting{}}
	p.parse()
	detection, kept := choose(p.scripts())
	b := projection.NewBuilder(text)
	for _, s := range kept {
		p.project(b, s)
	}
	return b.Map(), detection
}

func splitLines(text string) []line {
	var out []line
	for start := 0; start < len(text); {
		end := strings.IndexByte(text[start:], '\n')
		if end < 0 {
			out = append(out, line{start: start, end: len(text)})
			break
		}
		out = append(out, line{start: start, end: start + end})
		start += end + 1
	}
	return out
}

func (p *parser) content(l line) string { return p.text[l.start:l.end] }

func indented(s string) bool { return s != "" && (s[0] == ' ' || s[0] == '\t') }

func (p *parser) parse() {
	var attributes []string
	for i := 0; i < len(p.lines); i++ {
		content := p.content(p.lines[i])
		trimmed := strings.TrimSpace(content)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			continue
		case indented(content):
			attributes = nil
			continue
		case strings.HasPrefix(trimmed, "["):
			attributes = append(attributes, attributeItems(trimmed)...)
			continue
		}
		word := firstWord(trimmed)
		switch name, ok := assignment(trimmed); {
		case slices.Contains(keywords, word) && word != trimmed:
			p.statement(word, strings.TrimSpace(trimmed[len(word):]), trimmed)
		case ok:
			p.variables = append(p.variables, name)
		default:
			if r, ok := p.recipe(trimmed, i+1, attributes); ok {
				p.recipes = append(p.recipes, r)
				i += len(r.body)
			}
		}
		attributes = nil
	}
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

func isNameByte(c byte) bool {
	return c == '_' || c == '-' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func nameLength(s string) int {
	n := 0
	for n < len(s) && isNameByte(s[n]) {
		n++
	}
	return n
}

func assignment(s string) (string, bool) {
	n := nameLength(s)
	if n == 0 {
		return "", false
	}
	return s[:n], strings.HasPrefix(strings.TrimLeft(s[n:], " \t"), ":=")
}

func (p *parser) statement(keyword, rest, text string) {
	switch keyword {
	case "set":
		name := rest[:nameLength(rest)]
		value := strings.TrimSpace(rest[len(name):])
		values := []string{"true"}
		if after, ok := strings.CutPrefix(value, ":="); ok {
			values = stringValues(strings.TrimSpace(after))
		}
		p.settings[name] = setting{values: values, text: text}
	case "export":
		if name, ok := assignment(rest); ok {
			p.exported = append(p.exported, name)
		}
	}
}

func stringValues(s string) []string {
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	var out []string
	for item := range strings.SplitSeq(s, ",") {
		item = strings.TrimSpace(item)
		for _, quote := range []string{`"""`, "'''", `"`, "'"} {
			if len(item) >= 2*len(quote) && strings.HasPrefix(item, quote) && strings.HasSuffix(item, quote) {
				item = item[len(quote) : len(item)-len(quote)]
				break
			}
		}
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func attributeItems(s string) []string {
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	var out []string
	depth, start := 0, 0
	for i := range len(s) {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

func attributeName(item string) string {
	return item[:nameLength(item)]
}

func (p *parser) recipe(header string, next int, attributes []string) (recipe, bool) {
	header = strings.TrimPrefix(header, "@")
	n := nameLength(header)
	if n == 0 {
		return recipe{}, false
	}
	colon, ok := headerColon(header, n)
	if !ok {
		return recipe{}, false
	}
	r := recipe{attributes: attributes}
	for _, field := range paramFields(header[n:colon]) {
		field = strings.TrimLeft(field, "+*")
		exported := strings.HasPrefix(field, "$")
		field = strings.TrimPrefix(field, "$")
		name := field[:nameLength(field)]
		if name == "" {
			continue
		}
		r.params = append(r.params, name)
		if exported {
			r.exported = append(r.exported, name)
		}
	}
	last := next
	for i := next; i < len(p.lines); i++ {
		content := p.content(p.lines[i])
		if strings.TrimSpace(content) == "" {
			continue
		}
		if !indented(content) {
			break
		}
		if r.indent == "" {
			r.indent = content[:len(content)-len(strings.TrimLeft(content, " \t"))]
		}
		last = i + 1
	}
	r.body = p.lines[next:last]
	return r, len(r.body) > 0
}

func paramFields(s string) []string {
	var out []string
	depth, start := 0, -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			if end := strings.IndexByte(s[i+1:], c); end >= 0 {
				i += end + 1
			}
		case c == '(':
			depth++
		case c == ')':
			depth--
		case (c == ' ' || c == '\t') && depth == 0:
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

func headerColon(header string, from int) (int, bool) {
	depth := 0
	for i := from; i < len(header); i++ {
		switch c := header[i]; c {
		case '\'', '"', '`':
			end := strings.IndexByte(header[i+1:], c)
			if end < 0 {
				return 0, false
			}
			i += end + 1
		case '(':
			depth++
		case ')':
			depth--
		case ':':
			if depth == 0 {
				return i, !strings.HasPrefix(header[i+1:], "=")
			}
		}
	}
	return 0, false
}

func (p *parser) setting(name string, fallback []string) ([]string, string, bool) {
	s, ok := p.settings[name]
	if !ok {
		return fallback, "", false
	}
	return s.values, s.text, true
}

func (p *parser) scripts() []script {
	var out []script
	for _, r := range p.recipes {
		if s, ok := p.script(r); ok {
			out = append(out, s)
		}
	}
	return out
}

func (p *parser) script(r recipe) (script, bool) {
	first := strings.TrimLeft(p.content(r.body[0]), " \t")
	if strings.HasPrefix(first, "#!") {
		return detect(script{recipe: r, lines: r.body[1:], whole: true}, first, first, dialect.SourceHostSetting)
	}
	for _, item := range r.attributes {
		if attributeName(item) != "script" {
			continue
		}
		if args, ok := strings.CutPrefix(item, "script("); ok {
			return detect(script{recipe: r, lines: r.body, whole: true}, "#!"+strings.Join(stringValues(strings.TrimSuffix(args, ")")), " "), "["+item+"]", dialect.SourceHostSetting)
		}
		interpreter, text, set := p.setting("script-interpreter", defaultScript)
		return detect(script{recipe: r, lines: r.body, whole: true}, "#!"+strings.Join(interpreter, " "), text, source(set))
	}
	if p.windowsOnly(r) {
		return script{}, false
	}
	shell, text, set := p.setting("shell", defaultShell)
	return detect(script{recipe: r, lines: r.body}, "#!"+strings.Join(shell, " "), text, source(set))
}

func source(set bool) dialect.Source {
	if set {
		return dialect.SourceHostSetting
	}
	return dialect.SourceHostDefault
}

func (p *parser) windowsOnly(r recipe) bool {
	windows := slices.ContainsFunc(r.attributes, func(item string) bool { return attributeName(item) == "windows" })
	elsewhere := slices.ContainsFunc(r.attributes, func(item string) bool { return slices.Contains(otherSystemsThan, attributeName(item)) })
	shell := slices.ContainsFunc(windowsShells, func(name string) bool { _, ok := p.settings[name]; return ok })
	return windows && !elsewhere && shell
}

func detect(s script, shebang, evidence string, src dialect.Source) (script, bool) {
	name, ok := dialect.Interpreter(shebang)
	if !ok {
		return script{}, false
	}
	d, ok := dialect.FromName(name)
	if !ok {
		return script{}, false
	}
	if src == dialect.SourceHostDefault {
		evidence = tool
	}
	s.detection = dialect.Detection{Dialect: d, Source: src, Evidence: evidence}
	return s, true
}

func choose(scripts []script) (dialect.Detection, []script) {
	dialects := make([]dialect.Dialect, len(scripts))
	for i, s := range scripts {
		dialects[i] = s.detection.Dialect
	}
	shared, fits := dialect.Shared(dialects)
	kept := slices.DeleteFunc(slices.Clone(scripts), func(s script) bool { return !fits(s.detection.Dialect) })
	i := slices.IndexFunc(kept, func(s script) bool { return s.detection.Dialect == shared })
	if i < 0 {
		return dialect.Detection{Dialect: shared, Source: dialect.SourceHostDefault, Evidence: tool}, nil
	}
	return kept[i].detection, kept
}

func (p *parser) exportedBy(r recipe) []string {
	names := slices.Concat(p.exported, r.exported)
	if _, all := p.settings["export"]; all {
		names = slices.Concat(names, p.variables, r.params)
	}
	slices.Sort(names)
	return slices.DeleteFunc(slices.Compact(names), func(name string) bool { return strings.Contains(name, "-") })
}

func (p *parser) prelude(r recipe) string {
	var sb strings.Builder
	sb.WriteString("(\n")
	for _, name := range p.exportedBy(r) {
		sb.WriteString(name + "=" + projection.Dynamic + "\n")
	}
	return sb.String()
}

func (p *parser) project(b *projection.Builder, s script) {
	b.Region(encoder(s.whole, s.recipe.indent))
	if s.whole {
		p.wholeScript(b, s)
		return
	}
	afterComment := false
	for i := 0; i < len(s.lines); {
		n := p.logicalLength(s.lines[i:])
		afterComment = p.logicalLine(b, s, s.lines[i:i+n], afterComment)
		i += n
	}
}

func (p *parser) logicalLength(lines []line) int {
	n := 1
	for n < len(lines) && continued(p.content(lines[n-1])) {
		n++
	}
	return n
}

func continued(s string) bool {
	return (len(s)-len(strings.TrimRight(s, "\\")))%2 == 1
}

func (p *parser) start(l line, indent string) int {
	content := p.content(l)
	return l.start + min(len(indent), len(content)-len(strings.TrimLeft(content, " \t")))
}

func (p *parser) logicalLine(b *projection.Builder, s script, lines []line, afterComment bool) bool {
	first := lines[0]
	start := p.start(first, s.recipe.indent)
	command := start
	for command < first.end && command-start < len(sigils) && strings.IndexByte(sigils, p.text[command]) >= 0 {
		command++
	}
	if command == first.end && len(lines) == 1 {
		return afterComment
	}
	if !afterComment {
		b.Script()
	}
	b.Opaque(first.start, command, "")
	if p.text[command] == '#' && len(lines) == 1 {
		p.fragment(b, command, first.end)
		b.Opaque(first.end, first.end, "\n")
		return true
	}
	b.Opaque(command, command, p.prelude(s.recipe))
	from := command
	for i, l := range lines[:len(lines)-1] {
		p.fragment(b, from, l.end-1)
		next := lines[i+1]
		from = next.start + len(p.content(next)) - len(strings.TrimLeft(p.content(next), " \t"))
		b.Opaque(l.end-1, from, "")
	}
	last := lines[len(lines)-1]
	p.fragment(b, from, last.end)
	b.Opaque(last.end, last.end, "\n)\n")
	return false
}

func (p *parser) wholeScript(b *projection.Builder, s script) {
	b.Script()
	opened := false
	last := 0
	for _, l := range s.lines {
		start := p.start(l, s.recipe.indent)
		b.Opaque(l.start, start, "")
		if !opened {
			b.Opaque(start, start, p.prelude(s.recipe))
			opened = true
		}
		p.fragment(b, start, l.end)
		b.Opaque(l.end, l.end, "\n")
		last = l.end
	}
	if opened {
		b.Opaque(last, last, ")\n")
	}
}

func (p *parser) fragment(b *projection.Builder, start, end int) {
	run := start
	for i := start; i < end; {
		rest := p.text[i:end]
		switch {
		case strings.HasPrefix(rest, escapedOpen):
			b.Verbatim(run, i)
			b.Escape(i, i+len(escapedOpen), interpolationOpen)
			i += len(escapedOpen)
			run = i
		case strings.HasPrefix(rest, interpolationOpen):
			b.Verbatim(run, i)
			closing := strings.Index(rest, "}}")
			next := end
			if closing >= 0 {
				next = i + closing + len("}}")
			}
			b.Placeholder(i, next)
			i = next
			run = i
		default:
			i++
		}
	}
	b.Verbatim(run, end)
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
