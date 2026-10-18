package hadolint

import (
	_ "embed"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/stubbedev/shell-lsp/internal/host/dockerfile"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/scan"
)

var (
	reservedLabelPrefixes = []string{"com.docker.", "io.docker.", "org.dockerproject."}
	formatCodes           = map[LabelFormat]lint.Code{
		LabelURL: lint.DL3052, LabelRFC3339: lint.DL3053, LabelSPDX: lint.DL3054, LabelHash: lint.DL3055, LabelSemver: lint.DL3056, LabelEmail: lint.DL3058,
	}
	formatChecks = map[LabelFormat]func(string) bool{
		LabelURL: validURL, LabelRFC3339: validRFC3339, LabelSPDX: validSPDX, LabelHash: validHash, LabelSemver: validSemver, LabelEmail: validEmail,
	}
	formatNames = map[LabelFormat]string{
		LabelURL: "an absolute URL", LabelRFC3339: "an RFC 3339 date and time", LabelSPDX: "an SPDX license expression",
		LabelHash: "a git commit hash", LabelSemver: "a semantic version", LabelEmail: "an email address",
	}
)

type label struct {
	key       string
	keySpan   dockerfile.Word
	value     string
	known     bool
	valueSpan dockerfile.Word
}

func (c *checker) labelsOf(in *dockerfile.Instruction) []label {
	var out []label
	for _, p := range c.File.Pairs(in) {
		key, keyKnown := c.File.Expand(in, p.Key.Text)
		if !keyKnown {
			continue
		}
		l := label{key: key, keySpan: p.Key, valueSpan: p.Key}
		if p.Value != nil {
			l.value, l.known = c.File.Expand(in, p.Value.Text)
			l.valueSpan = *p.Value
		}
		out = append(out, l)
	}
	return out
}

func (c *checker) labels() {
	schema := map[string]LabelFormat{}
	for _, l := range c.Config.LabelSchema {
		schema[l.Name] = l.Format
	}
	for _, s := range c.stages {
		for _, in := range s.Instructions {
			if in.Name != "LABEL" {
				continue
			}
			for _, l := range c.labelsOf(in) {
				c.label(l, schema)
			}
		}
	}
	c.required()
}

func (c *checker) label(l label, schema map[string]LabelFormat) {
	if !validLabelKey(l.key) {
		c.report(lint.DL3048, l.keySpan.Span, "label key `%s` breaks Docker's naming guidelines: lowercase letters, digits, `.`, `-`, `_` and `/`, starting with a letter, without `..` or `--`, outside the reserved com.docker., io.docker. and org.dockerproject. namespaces", l.key)
	}
	format, typed := schema[l.key]
	if !typed {
		if c.Config.StrictLabels {
			c.report(lint.DL3050, l.keySpan.Span, "label `%s` is not in the label schema %s defines", l.key, c.Config.Path)
		}
		return
	}
	if !l.known {
		return
	}
	if l.value == "" {
		c.report(lint.DL3051, l.valueSpan.Span, "label `%s` is empty", l.key)
	}
	if check, ok := formatChecks[format]; ok && !check(l.value) {
		c.report(formatCodes[format], l.valueSpan.Span, "label `%s` should be %s, but is `%s`", l.key, formatNames[format], l.value)
	}
}

func (c *checker) required() {
	if len(c.Config.LabelSchema) == 0 || len(c.stages) == 0 {
		return
	}
	final := c.stages[len(c.stages)-1]
	present := map[string]bool{}
	for s := final; ; s = c.stages[s.Parent] {
		for _, in := range s.Instructions {
			if in.Name == "LABEL" {
				for _, l := range c.labelsOf(in) {
					present[l.key] = true
				}
			}
		}
		if s.Parent < 0 {
			break
		}
	}
	for _, l := range c.Config.LabelSchema {
		if !present[l.Name] {
			c.report(lint.DL3049, final.From.Keyword.Span, "the image lacks the label `%s` that %s requires", l.Name, c.Config.Path)
		}
	}
}

func validLabelKey(key string) bool {
	if key == "" || !scan.Lower(key[0]) || !scan.LowerOrDigit(key[len(key)-1]) {
		return false
	}
	if strings.Contains(key, "..") || strings.Contains(key, "--") ||
		slices.ContainsFunc(reservedLabelPrefixes, func(p string) bool { return strings.HasPrefix(key, p) }) {
		return false
	}
	return scan.All(key, func(c byte) bool { return scan.LowerOrDigit(c) || strings.IndexByte(".-_/", c) >= 0 })
}

func validURL(v string) bool {
	u, err := url.Parse(v)
	if err != nil || u.Scheme == "" || strings.ContainsAny(v, " \t\"<>\\^`{|}") || strings.Count(v, "#") > 1 ||
		strings.ContainsFunc(v, func(r rune) bool { return r > 0x7f }) {
		return false
	}
	rest, hierarchical := strings.CutPrefix(v[len(u.Scheme)+1:], "//")
	authority := rest
	if hierarchical {
		if end := strings.IndexAny(rest, "/?#"); end >= 0 {
			authority, rest = rest[:end], rest[end:]
		} else {
			rest = ""
		}
		if strings.Count(authority, "@") > 1 {
			return false
		}
	}
	return !strings.ContainsAny(rest, "[]")
}

func digits(s string) bool { return s != "" && scan.All(s, scan.Digit) }

func number(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

func twoDigits(s string, low, high int) bool {
	return len(s) == 2 && digits(s) && number(s) >= low && number(s) <= high
}

var monthDays = [...]int{31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}

func validRFC3339(v string) bool {
	if len(v) < len("2006-01-02T15:04:05Z") || v[4] != '-' || v[7] != '-' || v[10] != 'T' && v[10] != 't' || v[13] != ':' || v[16] != ':' {
		return false
	}
	year, month, day := v[0:4], v[5:7], v[8:10]
	if !digits(year) || !twoDigits(month, 1, 12) || !twoDigits(day, 1, monthDays[number(month)-1]) {
		return false
	}
	y := number(year)
	if number(month) == 2 && number(day) == 29 && (y%4 != 0 || y%100 == 0 && y%400 != 0) {
		return false
	}
	if !twoDigits(v[11:13], 0, 23) || !twoDigits(v[14:16], 0, 59) || !twoDigits(v[17:19], 0, 60) {
		return false
	}
	rest := v[19:]
	if fraction, ok := strings.CutPrefix(rest, "."); ok {
		end := scan.While(fraction, scan.Digit)
		if end == 0 {
			return false
		}
		rest = fraction[end:]
	}
	switch {
	case rest == "Z" || rest == "z":
		return true
	case len(rest) == 6 && (rest[0] == '+' || rest[0] == '-') && rest[3] == ':':
		return twoDigits(rest[1:3], 0, 23) && twoDigits(rest[4:6], 0, 59)
	}
	return false
}

func validHash(v string) bool {
	return (len(v) >= 7 && len(v) <= 40 || len(v) == 64) && scan.All(v, scan.LowerHexDigit)
}

func identifiers(s string, numeric bool) bool {
	for id := range strings.SplitSeq(s, ".") {
		if id == "" || !scan.All(id, func(c byte) bool { return scan.Alnum(c) || c == '-' }) {
			return false
		}
		if numeric && digits(id) && len(id) > 1 && id[0] == '0' {
			return false
		}
	}
	return true
}

func validSemver(v string) bool {
	v, build, hasBuild := strings.Cut(v, "+")
	v, pre, hasPre := strings.Cut(v, "-")
	parts := strings.Split(v, ".")
	if len(parts) != 3 || slices.ContainsFunc(parts, func(p string) bool { return !digits(p) || len(p) > 1 && p[0] == '0' }) {
		return false
	}
	return (!hasPre || identifiers(pre, true)) && (!hasBuild || identifiers(build, false))
}

const atext = "!#$%&'*+-/=?^_`{|}~"

func atom(s string) bool {
	return s != "" && scan.All(s, func(c byte) bool { return scan.Alnum(c) || strings.IndexByte(atext, c) >= 0 })
}

func hostname(s string) bool {
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return false
	}
	for l := range strings.SplitSeq(s, ".") {
		if l == "" || len(l) > maxLabel || l[0] == '-' || l[len(l)-1] == '-' ||
			!scan.All(l, func(c byte) bool { return scan.Alnum(c) || c == '-' }) {
			return false
		}
	}
	return true
}

const (
	maxLabel     = 63
	maxLocalPart = 64
	maxAddress   = 254
)

func validEmail(v string) bool {
	at := strings.LastIndexByte(v, '@')
	if at <= 0 || len(v) > maxAddress {
		return false
	}
	local, domain := v[:at], v[at+1:]
	if len(local) > maxLocalPart {
		return false
	}
	quoted := len(local) >= 2 && local[0] == '"' && local[len(local)-1] == '"' && !strings.ContainsAny(local[1:len(local)-1], "\"\\\r\n")
	if !quoted && slices.ContainsFunc(strings.Split(local, "."), func(a string) bool { return !atom(a) }) {
		return false
	}
	if inner, ok := strings.CutPrefix(domain, "["); ok {
		inner, closed := strings.CutSuffix(inner, "]")
		return closed && !strings.ContainsAny(inner, "[]\\")
	}
	return hostname(domain)
}

var (
	//go:embed spdx/licenses.txt
	licenseList string
	//go:embed spdx/exceptions.txt
	exceptionList string
)

func idSet(list string) map[string]bool {
	out := map[string]bool{}
	for line := range strings.Lines(list) {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			out[line] = true
		}
	}
	return out
}

var (
	licenses   = sync.OnceValue(func() map[string]bool { return idSet(licenseList) })
	exceptions = sync.OnceValue(func() map[string]bool { return idSet(exceptionList) })
)

func validSPDX(v string) bool {
	p := &spdxParser{tokens: spdxTokens(v)}
	return p.expression() && p.at == len(p.tokens)
}

func spdxTokens(v string) []string {
	var out []string
	for field := range strings.FieldsSeq(strings.NewReplacer("(", " ( ", ")", " ) ").Replace(v)) {
		out = append(out, field)
	}
	return out
}

type spdxParser struct {
	tokens []string
	at     int
}

func (p *spdxParser) next() string {
	if p.at >= len(p.tokens) {
		return ""
	}
	return p.tokens[p.at]
}

func (p *spdxParser) expression() bool {
	if !p.compound() {
		return false
	}
	for p.next() == "AND" || p.next() == "OR" {
		p.at++
		if !p.compound() {
			return false
		}
	}
	return true
}

func (p *spdxParser) compound() bool {
	if p.next() == "(" {
		p.at++
		if !p.expression() || p.next() != ")" {
			return false
		}
		p.at++
		return true
	}
	if !p.simple() {
		return false
	}
	if p.next() == "WITH" {
		p.at++
		if !exceptions()[p.next()] {
			return false
		}
		p.at++
	}
	return true
}

func (p *spdxParser) simple() bool {
	id := p.next()
	p.at++
	if ref, ok := strings.CutPrefix(id, "DocumentRef-"); ok {
		_, id, ok = strings.Cut(ref, ":")
		if !ok {
			return false
		}
	}
	if name, ok := strings.CutPrefix(id, "LicenseRef-"); ok {
		return atom(strings.ReplaceAll(name, ".", "-"))
	}
	return licenses()[strings.TrimSuffix(id, "+")]
}
