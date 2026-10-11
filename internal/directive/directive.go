package directive

import (
	"slices"
	"strings"
)

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

const All = "all"

var keys = []string{Disable, Enable, Source, SourcePath, Shell, ExternalSources, ExtendedAnalysis}

func Keys() []string { return slices.Clone(keys) }

var knownKeys = func() map[string]bool {
	out := make(map[string]bool, len(keys))
	for _, key := range keys {
		out[key] = true
	}
	return out
}()

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
	i, ok := header(comment)
	if !ok {
		return Directive{}, false
	}
	return parsePairs(comment, i)
}

func header(comment string) (int, bool) {
	i := skipBlanks(comment, 0)
	if i == len(comment) || comment[i] != '#' {
		return 0, false
	}
	i = skipBlanks(comment, i+1)
	if !strings.HasPrefix(comment[i:], word) {
		return 0, false
	}
	i += len(word)
	if i < len(comment) && !isBlank(comment[i]) {
		return 0, false
	}
	return i, true
}

type Slot uint8

const (
	SlotKey Slot = iota + 1
	SlotValue
)

type Cursor struct {
	Slot   Slot
	Key    string
	Start  int
	End    int
	Others []string
}

func CursorAt(comment string, offset int) (Cursor, bool) {
	i, ok := header(comment)
	if !ok || offset <= i || offset > len(comment) {
		return Cursor{}, false
	}
	for {
		start := skipBlanks(comment, i)
		if offset < start || start == len(comment) || comment[start] == '#' {
			if offset > start {
				return Cursor{}, false
			}
			return Cursor{Slot: SlotKey, Start: offset, End: offset}, true
		}
		keyEnd := start
		for keyEnd < len(comment) && isKeyChar(comment[keyEnd]) {
			keyEnd++
		}
		if offset <= keyEnd {
			return Cursor{Slot: SlotKey, Start: start, End: keyEnd}, true
		}
		if keyEnd == len(comment) || comment[keyEnd] != '=' {
			return Cursor{}, false
		}
		valueStart := keyEnd + 1
		_, valueEnd, ok := scanValue(comment, valueStart)
		if !ok {
			return Cursor{}, false
		}
		if offset <= valueEnd {
			return valueCursor(comment[start:keyEnd], comment, valueStart, valueEnd, offset)
		}
		i = valueEnd
	}
}

func valueCursor(key, s string, start, end, offset int) (Cursor, bool) {
	raw := s[start:end]
	if strings.ContainsAny(raw, `'"`) {
		return Cursor{}, false
	}
	if !ListKey(key) {
		return Cursor{Slot: SlotValue, Key: key, Start: start, End: end}, true
	}
	itemStart := start + strings.LastIndexByte(s[start:offset], ',') + 1
	itemEnd := end
	if i := strings.IndexByte(s[offset:end], ','); i >= 0 {
		itemEnd = offset + i
	}
	var others []string
	for _, item := range Items(Pair{Key: key, Value: raw, ValueOffset: start, ValueEnd: end}) {
		if item.Offset != itemStart && item.Text != "" {
			others = append(others, item.Text)
		}
	}
	return Cursor{Slot: SlotValue, Key: key, Start: itemStart, End: itemEnd, Others: others}, true
}

func ListKey(key string) bool { return key == Disable || key == Enable }

type Item struct {
	Text   string
	Offset int
	End    int
}

func Items(p Pair) []Item {
	if p.ValueEnd-p.ValueOffset != len(p.Value) {
		return nil
	}
	var out []Item
	offset := p.ValueOffset
	for text := range strings.SplitSeq(p.Value, ",") {
		out = append(out, Item{Text: text, Offset: offset, End: offset + len(text)})
		offset += len(text) + 1
	}
	return out
}

func ParseSettings(line string) (Directive, bool) {
	return parsePairs(line, 0)
}

func parsePairs(s string, i int) (Directive, bool) {
	var d Directive
	for {
		i = skipBlanks(s, i)
		if i == len(s) || s[i] == '#' {
			return d, len(d.Pairs) > 0
		}
		pair, end, problem := scanPair(s, i)
		if problem != nil {
			if len(d.Pairs) == 0 && !startsWithKey(s[i:]) {
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
