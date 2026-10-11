package editorconfig

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type numericRange struct {
	low, high int
}

type pattern struct {
	re     *regexp.Regexp
	ranges []numericRange
}

func compilePattern(glob, dir string) (pattern, error) {
	if !strings.Contains(glob, "/") {
		glob = "**/" + glob
	} else {
		glob = strings.TrimPrefix(glob, "/")
	}
	var ranges []numericRange
	body := translate(glob, &ranges)
	prefix := regexp.QuoteMeta(strings.TrimSuffix(dir, "/") + "/")
	re, err := regexp.Compile("^" + prefix + body + "$")
	if err != nil {
		return pattern{}, fmt.Errorf("section glob %q: %w", glob, err)
	}
	return pattern{re: re, ranges: ranges}, nil
}

func (p pattern) matches(path string) bool {
	groups := p.re.FindStringSubmatch(path)
	if groups == nil {
		return false
	}
	for i, r := range p.ranges {
		n, err := strconv.Atoi(groups[i+1])
		if err != nil || n < r.low || n > r.high {
			return false
		}
	}
	return true
}

func translate(glob string, ranges *[]numericRange) string {
	var out strings.Builder
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch c {
		case '\\':
			if i+1 < len(glob) {
				i++
				out.WriteString(regexp.QuoteMeta(glob[i : i+1]))
				continue
			}
			out.WriteString(`\\`)
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				i++
				if i+1 < len(glob) && glob[i+1] == '/' && (i == 1 || glob[i-2] == '/') {
					i++
					out.WriteString(`(?:.*/)?`)
					continue
				}
				out.WriteString(`.*`)
				continue
			}
			out.WriteString(`[^/]*`)
		case '?':
			out.WriteString(`[^/]`)
		case '[':
			class, next, ok := bracket(glob, i)
			if !ok {
				out.WriteString(`\[`)
				continue
			}
			out.WriteString(class)
			i = next
		case '{':
			alternation, next, ok := brace(glob, i, ranges)
			if !ok {
				out.WriteString(`\{`)
				continue
			}
			out.WriteString(alternation)
			i = next
		default:
			out.WriteString(regexp.QuoteMeta(glob[i : i+1]))
		}
	}
	return out.String()
}

func bracket(glob string, open int) (string, int, bool) {
	end := strings.IndexByte(glob[open+1:], ']')
	if end < 0 {
		return "", open, false
	}
	content := glob[open+1 : open+1+end]
	if strings.Contains(content, "/") {
		return "", open, false
	}
	negate := strings.HasPrefix(content, "!") || strings.HasPrefix(content, "^")
	if negate {
		content = content[1:]
	}
	var class strings.Builder
	class.WriteByte('[')
	if negate {
		class.WriteString("^/")
	}
	for i := range len(content) {
		switch content[i] {
		case '\\', '[', ']', '^':
			class.WriteByte('\\')
		}
		class.WriteByte(content[i])
	}
	class.WriteByte(']')
	return class.String(), open + 1 + end, true
}

func brace(glob string, open int, ranges *[]numericRange) (string, int, bool) {
	depth := 0
	for i := open; i < len(glob); i++ {
		switch glob[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return braceBody(glob[open+1:i], ranges), i, true
			}
		}
	}
	return "", open, false
}

var numericBrace = regexp.MustCompile(`^([+-]?\d+)\.\.([+-]?\d+)$`)

func braceBody(body string, ranges *[]numericRange) string {
	if m := numericBrace.FindStringSubmatch(body); m != nil {
		low, errLow := strconv.Atoi(m[1])
		high, errHigh := strconv.Atoi(m[2])
		if errLow == nil && errHigh == nil {
			*ranges = append(*ranges, numericRange{low: min(low, high), high: max(low, high)})
			return `([+-]?\d+)`
		}
	}
	alternatives := splitTopLevel(body)
	if len(alternatives) == 1 {
		return `\{` + translate(body, ranges) + `\}`
	}
	parts := make([]string, len(alternatives))
	for i, alt := range alternatives {
		parts[i] = translate(alt, ranges)
	}
	return "(?:" + strings.Join(parts, "|") + ")"
}

func splitTopLevel(body string) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, body[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, body[start:])
}
