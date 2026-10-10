package directive

import "strings"

const word = "shellcheck"

const (
	Disable          = "disable"
	Enable           = "enable"
	Source           = "source"
	SourcePath       = "source-path"
	Shell            = "shell"
	ExternalSources  = "external-sources"
	ExtendedAnalysis = "extended-analysis"
)

var knownKeys = map[string]bool{
	Disable: true, Enable: true, Source: true, SourcePath: true,
	Shell: true, ExternalSources: true, ExtendedAnalysis: true,
}

type Pair struct {
	Key         string
	Value       string
	KeyOffset   int
	ValueOffset int
	ValueEnd    int
}

func (p Pair) Known() bool { return knownKeys[p.Key] }

type ProblemKind uint8

const (
	LeftoverText ProblemKind = iota + 1
	MissingEquals
	MalformedQuote
)

type Problem struct {
	Kind   ProblemKind
	Offset int
}

type Directive struct {
	Pairs   []Pair
	Problem *Problem
}

func Parse(comment string) (Directive, bool) {
	i := skipBlanks(comment, 0)
	if i == len(comment) || comment[i] != '#' {
		return Directive{}, false
	}
	i = skipBlanks(comment, i+1)
	if !strings.HasPrefix(comment[i:], word) {
		return Directive{}, false
	}
	i += len(word)
	if i < len(comment) && !isBlank(comment[i]) {
		return Directive{}, false
	}
	var d Directive
	for {
		i = skipBlanks(comment, i)
		if i == len(comment) || comment[i] == '#' {
			return d, len(d.Pairs) > 0
		}
		pair, end, problem := scanPair(comment, i)
		if problem != nil {
			if len(d.Pairs) == 0 && !startsWithKey(comment[i:]) {
				return Directive{}, false
			}
			d.Problem = problem
			return d, true
		}
		d.Pairs = append(d.Pairs, pair)
		i = end
	}
}

func startsWithKey(s string) bool {
	end := 0
	for end < len(s) && isKeyChar(s[end]) {
		end++
	}
	return knownKeys[s[:end]]
}

func scanPair(s string, start int) (Pair, int, *Problem) {
	keyEnd := start
	for keyEnd < len(s) && isKeyChar(s[keyEnd]) {
		keyEnd++
	}
	if keyEnd == start {
		return Pair{}, 0, &Problem{Kind: LeftoverText, Offset: start}
	}
	if keyEnd == len(s) || s[keyEnd] != '=' {
		return Pair{}, 0, &Problem{Kind: MissingEquals, Offset: start}
	}
	valueStart := keyEnd + 1
	value, end, ok := scanValue(s, valueStart)
	if !ok {
		return Pair{}, 0, &Problem{Kind: MalformedQuote, Offset: valueStart}
	}
	return Pair{Key: s[start:keyEnd], Value: value, KeyOffset: start, ValueOffset: valueStart, ValueEnd: end}, end, nil
}

func scanValue(s string, start int) (string, int, bool) {
	end := start
	for end < len(s) && !isBlank(s[end]) && s[end] != '\'' && s[end] != '"' {
		end++
	}
	if end == len(s) || isBlank(s[end]) {
		return s[start:end], end, true
	}
	var value strings.Builder
	value.WriteString(s[start:end])
	for end < len(s) && !isBlank(s[end]) {
		quote := s[end]
		if quote != '\'' && quote != '"' {
			value.WriteByte(quote)
			end++
			continue
		}
		closing := strings.IndexByte(s[end+1:], quote)
		if closing < 0 {
			return "", 0, false
		}
		value.WriteString(s[end+1 : end+1+closing])
		end += closing + 2
	}
	return value.String(), end, true
}

func skipBlanks(s string, i int) int {
	for i < len(s) && isBlank(s[i]) {
		i++
	}
	return i
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

func isKeyChar(c byte) bool { return c == '-' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') }

func (d Directive) Value(key string) (string, bool) {
	for _, pair := range d.Pairs {
		if pair.Key == key {
			return pair.Value, true
		}
	}
	return "", false
}

func (d Directive) Values(key string) []string {
	var out []string
	for _, pair := range d.Pairs {
		if pair.Key == key {
			out = append(out, pair.Value)
		}
	}
	return out
}
