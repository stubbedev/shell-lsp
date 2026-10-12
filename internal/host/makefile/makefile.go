package makefile

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/projection"
)

const (
	defaultPrefix = '\t'
	recipeFlags   = " \t@-+"
	oneShell      = ".ONESHELL"
)

var (
	conditionals = []string{"ifeq", "ifneq", "ifdef", "ifndef", "else", "endif"}
	modifiers    = []string{"export", "override", "private"}
	directives   = []string{"include", "-include", "sinclude", "vpath", "undefine", "unexport", "export", "load", "-load"}
)

type recipeLine struct {
	start, end int
}

type parser struct {
	text     string
	prefix   byte
	inRule   bool
	defines  int
	oneShell bool
	shell    string
	shellSet bool
	setting  string
	recipes  [][]recipeLine
}

func Project(text string) (*projection.Map, dialect.Detection) {
	p := &parser{text: text, prefix: defaultPrefix}
	p.parse()
	b := projection.NewBuilder(text)
	detection, ok := p.dialect()
	if !ok {
		return b.Map(), detection
	}
	b.Region(encode)
	afterComment := false
	for _, lines := range p.recipes {
		if p.oneShell {
			b.Script()
			p.script(b, lines)
			continue
		}
		for _, l := range lines {
			afterComment = p.line(b, l, afterComment)
		}
	}
	return b.Map(), detection
}

func encode(_ string, _, _ int, text string) (string, bool) {
	if strings.ContainsRune(text, '\n') {
		return "", false
	}
	return strings.ReplaceAll(text, "$", "$$"), true
}

func (p *parser) dialect() (dialect.Detection, bool) {
	if !p.shellSet {
		return dialect.Detection{Dialect: dialect.Posix, Source: dialect.SourceHostDefault, Evidence: "make"}, true
	}
	found := dialect.Detection{Dialect: dialect.Posix, Source: dialect.SourceHostSetting, Evidence: p.setting}
	if strings.ContainsRune(p.shell, '$') {
		return found, false
	}
	name, ok := dialect.Interpreter("#!" + p.shell)
	if !ok {
		return found, false
	}
	d, ok := dialect.FromName(name)
	if ok {
		found.Dialect = d
	}
	return found, ok
}

func (p *parser) parse() {
	for pos := 0; pos < len(p.text); {
		end := p.logicalEnd(pos)
		line := p.text[pos:end]
		switch {
		case p.defines > 0:
			p.defineBody(line)
		case p.inRule && line != "" && line[0] == p.prefix:
			n := len(p.recipes) - 1
			p.recipes[n] = append(p.recipes[n], recipeLine{start: pos + 1, end: end})
		case strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimLeft(line, " \t"), "#"):
		default:
			p.directive(pos, end)
		}
		pos = end + 1
	}
}

func (p *parser) logicalEnd(pos int) int {
	for {
		end := strings.IndexByte(p.text[pos:], '\n')
		if end < 0 {
			return len(p.text)
		}
		end += pos
		if !continued(p.text[pos:end]) {
			return end
		}
		pos = end + 1
	}
}

func continued(line string) bool {
	return (len(line)-len(strings.TrimRight(line, "\\")))%2 == 1
}

func (p *parser) defineBody(line string) {
	switch firstWord(strings.TrimLeft(line, " \t")) {
	case "endef":
		p.defines--
	case "define":
		p.defines++
	}
}

func (p *parser) directive(start, end int) {
	body := p.text[start:end]
	body = body[:commentStart(body)]
	s := strings.TrimLeft(body, " \t")
	word := firstWord(s)
	if slices.Contains(conditionals, word) {
		return
	}
	p.inRule = false
	for slices.Contains(modifiers, word) && strings.TrimSpace(s[len(word):]) != "" {
		s = strings.TrimLeft(s[len(word):], " \t")
		word = firstWord(s)
	}
	switch {
	case word == "define":
		p.defines++
		return
	case slices.Contains(directives, word):
		return
	}
	i, rule := operator(s)
	switch {
	case i < 0:
	case rule:
		p.rule(s, i, start+len(body)-len(s), end)
	default:
		p.assign(s, i)
	}
}

func (p *parser) rule(s string, colon, offset, end int) {
	after := colon + 1
	if after < len(s) && s[after] == ':' {
		after++
	}
	for i := after; i < len(s); {
		switch s[i] {
		case '$':
			i = referenceEnd(s, i, len(s))
			continue
		case '=':
			return
		case ';':
			p.openRule(s[:colon])
			n := len(p.recipes) - 1
			p.recipes[n] = append(p.recipes[n], recipeLine{start: offset + i + 1, end: end})
			return
		}
		i++
	}
	p.openRule(s[:colon])
}

func (p *parser) openRule(targets string) {
	p.inRule = true
	p.recipes = append(p.recipes, nil)
	if slices.Contains(strings.Fields(targets), oneShell) {
		p.oneShell = true
	}
}

func (p *parser) assign(s string, i int) {
	name, op := s[:i], s[i]
	if op == '=' && i > 0 && strings.ContainsRune("?+!", rune(s[i-1])) {
		name, op = s[:i-1], s[i-1]
	}
	value := strings.TrimSpace(s[strings.IndexByte(s[i:], '=')+i+1:])
	switch strings.TrimSpace(name) {
	case "SHELL":
		switch op {
		case '=', ':':
			p.shell, p.shellSet, p.setting = value, true, oneLine(s)
		case '!':
			p.shell, p.shellSet, p.setting = "$(shell)", true, oneLine(s)
		}
	case ".RECIPEPREFIX":
		switch {
		case value == "":
			p.prefix = defaultPrefix
		case !strings.ContainsRune(value, '$'):
			p.prefix = value[0]
		}
	}
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\\\n", " ")), " ")
}

func operator(s string) (int, bool) {
	for i := 0; i < len(s); {
		switch s[i] {
		case '$':
			i = referenceEnd(s, i, len(s))
			continue
		case '=':
			return i, false
		case ':':
			rest := strings.TrimLeft(s[i:], ":")
			return i, !strings.HasPrefix(rest, "=") || len(s)-i-len(rest) > 3
		}
		i++
	}
	return -1, false
}

func commentStart(s string) int {
	for i := 0; i < len(s); {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '$':
			i = referenceEnd(s, i, len(s))
			continue
		case '#':
			return i
		}
		i++
	}
	return len(s)
}

func referenceEnd(s string, i, end int) int {
	if i+1 >= end {
		return end
	}
	open := s[i+1]
	var closing byte
	switch open {
	case '(':
		closing = ')'
	case '{':
		closing = '}'
	default:
		return i + 2
	}
	depth := 0
	for j := i + 1; j < end; j++ {
		switch s[j] {
		case open:
			depth++
		case closing:
			depth--
			if depth == 0 {
				return j + 1
			}
		}
	}
	return end
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " \t\\"); i >= 0 {
		return s[:i]
	}
	return s
}

func (p *parser) commandStart(l recipeLine) int {
	return l.end - len(strings.TrimLeft(p.text[l.start:l.end], recipeFlags))
}

func (p *parser) commentOnly(start, end int) bool {
	return p.text[start] == '#' && !strings.Contains(p.text[start:end], "\n")
}

func (p *parser) line(b *projection.Builder, l recipeLine, afterComment bool) bool {
	start := p.commandStart(l)
	if start == l.end {
		return afterComment
	}
	if !afterComment {
		b.Script()
	}
	b.Opaque(l.start, start, "")
	if p.commentOnly(start, l.end) {
		p.content(b, start, l.end)
		b.Opaque(l.end, l.end, "\n")
		return true
	}
	b.Opaque(start, start, "(\n")
	p.content(b, start, l.end)
	b.Opaque(l.end, l.end, "\n)\n")
	return false
}

func (p *parser) script(b *projection.Builder, lines []recipeLine) {
	wrapped := slices.ContainsFunc(lines, func(l recipeLine) bool {
		start := p.commandStart(l)
		return start < l.end && !p.commentOnly(start, l.end)
	})
	opened := false
	last := 0
	for _, l := range lines {
		start := p.commandStart(l)
		if start == l.end {
			continue
		}
		b.Opaque(l.start, start, "")
		if wrapped && !opened {
			b.Opaque(start, start, "(\n")
			opened = true
		}
		p.content(b, start, l.end)
		b.Opaque(l.end, l.end, "\n")
		last = l.end
	}
	if opened {
		b.Opaque(last, last, ")\n")
	}
}

func (p *parser) content(b *projection.Builder, start, end int) {
	run := start
	for i := start; i < end; {
		switch {
		case p.text[i] == '\\' && i+1 < end && p.text[i+1] == '\n':
			i += 2
			if i < end && p.text[i] == p.prefix {
				b.Verbatim(run, i)
				b.Opaque(i, i+1, "")
				i++
				run = i
			}
		case p.text[i] == '$':
			b.Verbatim(run, i)
			if i+1 < end && p.text[i+1] == '$' {
				b.Escape(i, i+2, "$")
				i += 2
			} else {
				next := referenceEnd(p.text, i, end)
				b.Placeholder(i, next)
				i = next
			}
			run = i
		default:
			i++
		}
	}
	b.Verbatim(run, end)
}
