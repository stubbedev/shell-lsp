package makefile

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/projection"
	"github.com/stubbedev/shell-lsp/internal/scan"
)

const (
	defaultPrefix = '\t'
	recipeFlags   = " \t@-+"
	oneShell      = ".ONESHELL"
	posixTarget   = ".POSIX"
	defaultFlags  = "-c"
	posixFlags    = "-ec"
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
	flags    string
	posix    bool
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
	scan.Forward(0, len(p.text), func(pos int) int {
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
		return end + 1
	})
}

func (p *parser) logicalEnd(start int) int {
	end := len(p.text)
	scan.Forward(start, len(p.text), func(pos int) int {
		nl := strings.IndexByte(p.text[pos:], '\n')
		switch {
		case nl < 0:
			return len(p.text)
		case !continued(p.text[pos : pos+nl]):
			end = pos + nl
			return len(p.text)
		}
		return pos + nl + 1
	})
	return end
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
	i := topLevel(s, after, "=;")
	switch {
	case i == len(s):
		p.openRule(s[:colon])
	case s[i] == ';':
		p.openRule(s[:colon])
		n := len(p.recipes) - 1
		p.recipes[n] = append(p.recipes[n], recipeLine{start: offset + i + 1, end: end})
	}
}

func (p *parser) openRule(targets string) {
	p.inRule = true
	p.recipes = append(p.recipes, nil)
	fields := strings.Fields(targets)
	p.oneShell = p.oneShell || slices.Contains(fields, oneShell)
	p.posix = p.posix || slices.Contains(fields, posixTarget)
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
	case ".SHELLFLAGS":
		if op == '=' || op == ':' {
			p.flags = value
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
	i := topLevel(s, 0, "=:")
	switch {
	case i == len(s):
		return -1, false
	case s[i] == '=':
		return i, false
	}
	rest := strings.TrimLeft(s[i:], ":")
	return i, !strings.HasPrefix(rest, "=") || len(s)-i-len(rest) > 3
}

func commentStart(s string) int {
	return topLevel(s, 0, "#")
}

func topLevel(s string, from int, stops string) int {
	found := len(s)
	scan.Forward(from, len(s), func(i int) int {
		switch {
		case s[i] == '\\':
			return i + 2
		case s[i] == '$':
			return referenceEnd(s, i, len(s))
		case strings.IndexByte(stops, s[i]) >= 0:
			found = i
			return len(s)
		}
		return i + 1
	})
	return found
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

func (p *parser) prelude() string {
	flags := p.flags
	switch {
	case flags != "":
	case p.posix:
		flags = posixFlags
	default:
		flags = defaultFlags
	}
	if strings.ContainsRune(flags, '$') {
		return ""
	}
	words := strings.Fields(flags)
	var options []string
	for i := 0; i < len(words); i++ {
		word := words[i]
		switch {
		case word == "-o" && i+1 < len(words):
			options = append(options, word, words[i+1])
			i++
		case strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "--"):
			letters := strings.ReplaceAll(word[1:], "c", "")
			named := strings.HasSuffix(letters, "o") && i+1 < len(words)
			if named {
				letters = strings.TrimSuffix(letters, "o")
			}
			if letters != "" {
				options = append(options, "-"+letters)
			}
			if named {
				options = append(options, "-o", words[i+1])
				i++
			}
		}
	}
	if len(options) == 0 {
		return ""
	}
	return "set " + strings.Join(options, " ") + "\n"
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
	b.Opaque(start, start, "(\n"+p.prelude())
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
			b.Opaque(start, start, "(\n"+p.prelude())
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
	scan.Forward(start, end, func(i int) int {
		switch {
		case p.text[i] == '\\' && i+1 < end && p.text[i+1] == '\n':
			next := i + 2
			if next < end && p.text[next] == p.prefix {
				b.Verbatim(run, next)
				b.Opaque(next, next+1, "")
				run = next + 1
				return run
			}
			return next
		case p.text[i] == '$' && i+1 < end && p.text[i+1] == '$':
			b.Verbatim(run, i)
			b.Escape(i, i+2, "$")
			run = i + 2
			return run
		case p.text[i] == '$':
			b.Verbatim(run, i)
			run = referenceEnd(p.text, i, end)
			b.Placeholder(i, run)
			return run
		}
		return i + 1
	})
	b.Verbatim(run, end)
}
