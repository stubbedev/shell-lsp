package directive

import (
	"fmt"
	"iter"
	"strings"
)

const (
	Word      = "shellcheck"
	All       = "all"
	ScriptDir = "SCRIPTDIR"
)

type Key uint8

const (
	Disable Key = iota + 1
	Enable
	Source
	SourcePath
	Shell
	ExternalSources
	ExtendedAnalysis
)

type ValueKind uint8

const (
	Codes ValueKind = iota + 1
	OptionalChecks
	File
	Directory
	ShellName
	Boolean
)

type keySpec struct {
	name  string
	value ValueKind
	doc   string
}

var keySpecs = [...]keySpec{
	Disable:          {name: "disable", value: Codes, doc: "Silence the listed checks for the next command, or for the whole file when the directive comes before the first command. Takes codes such as `SC2086`, ranges such as `SC2000-SC2099`, or `all`, separated by commas."},
	Enable:           {name: "enable", value: OptionalChecks, doc: "Turn on optional checks by name, or every optional check with `all`. Only honoured before the first command."},
	Source:           {name: "source", value: File, doc: "The file the following `source` or `.` command loads, for when its path is computed at run time. `/dev/null` skips it."},
	SourcePath:       {name: "source-path", value: Directory, doc: "A directory to look for sourced files in. `" + ScriptDir + "` stands for the directory of the script itself."},
	Shell:            {name: "shell", value: ShellName, doc: "Lint the file as this shell, whatever its shebang says."},
	ExternalSources:  {name: "external-sources", value: Boolean, doc: "Whether sourced files outside the ones being checked may be followed. Only honoured in a `.shellcheckrc` file."},
	ExtendedAnalysis: {name: "extended-analysis", value: Boolean, doc: "Turn the checks that follow values through the script on (`true`) or off (`false`) for this file."},
}

var keysByName = func() map[string]Key {
	out := make(map[string]Key, len(keySpecs))
	for key := range Keys() {
		out[key.String()] = key
	}
	return out
}()

func Keys() iter.Seq[Key] {
	return func(yield func(Key) bool) {
		for key := Disable; int(key) < len(keySpecs); key++ {
			if !yield(key) {
				return
			}
		}
	}
}

func KeyNames() []string {
	names := make([]string, 0, len(keySpecs))
	for key := range Keys() {
		names = append(names, key.String())
	}
	return names
}

func KeyNamed(name string) (Key, bool) {
	key, ok := keysByName[name]
	return key, ok
}

func (k Key) spec() keySpec {
	if k < Disable || int(k) >= len(keySpecs) {
		panic(fmt.Sprintf("directive: unknown key %d", k))
	}
	return keySpecs[k]
}

func (k Key) String() string { return k.spec().name }

func (k Key) Value() ValueKind { return k.spec().value }

func (k Key) Doc() string { return k.spec().doc }

func (k Key) List() bool {
	switch k.Value() {
	case Codes, OptionalChecks:
		return true
	case File, Directory, ShellName, Boolean:
		return false
	}
	panic(fmt.Sprintf("directive: key %s has no value kind", k))
}

type Pair struct {
	Name        string
	Value       string
	KeyOffset   int
	ValueOffset int
	ValueEnd    int
}

func (p Pair) Key() (Key, bool) { return KeyNamed(p.Name) }

func (p Pair) Known() bool {
	_, ok := p.Key()
	return ok
}

func (p Pair) Is(key Key) bool {
	k, ok := p.Key()
	return ok && k == key
}

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
	i, ok := Header(comment)
	if !ok {
		return Directive{}, false
	}
	return parsePairs(comment, i)
}

func Header(comment string) (int, bool) {
	i := skipBlanks(comment, 0)
	if i == len(comment) || comment[i] != '#' {
		return 0, false
	}
	i = skipBlanks(comment, i+1)
	if !strings.HasPrefix(comment[i:], Word) {
		return 0, false
	}
	i += len(Word)
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
	Key    Key
	Start  int
	End    int
	Others []string
}

func CursorAt(comment string, offset int) (Cursor, bool) {
	i, ok := Header(comment)
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

func valueCursor(name, s string, start, end, offset int) (Cursor, bool) {
	raw := s[start:end]
	key, known := KeyNamed(name)
	if !known || strings.ContainsAny(raw, `'"`) {
		return Cursor{}, false
	}
	if !key.List() {
		return Cursor{Slot: SlotValue, Key: key, Start: start, End: end}, true
	}
	itemStart := start + strings.LastIndexByte(s[start:offset], ',') + 1
	itemEnd := end
	if i := strings.IndexByte(s[offset:end], ','); i >= 0 {
		itemEnd = offset + i
	}
	var others []string
	for _, item := range Items(Pair{Name: name, Value: raw, ValueOffset: start, ValueEnd: end}) {
		if item.Offset != itemStart && item.Text != "" {
			others = append(others, item.Text)
		}
	}
	return Cursor{Slot: SlotValue, Key: key, Start: itemStart, End: itemEnd, Others: others}, true
}

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
	_, known := KeyNamed(s[:end])
	return known
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
	return Pair{Name: s[start:keyEnd], Value: value, KeyOffset: start, ValueOffset: valueStart, ValueEnd: end}, end, nil
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

func (d Directive) Pair(key Key) (Pair, bool) {
	for _, pair := range d.Pairs {
		if pair.Is(key) {
			return pair, true
		}
	}
	return Pair{}, false
}

func (d Directive) Value(key Key) (string, bool) {
	pair, ok := d.Pair(key)
	return pair.Value, ok
}

func (d Directive) Values(key Key) []string {
	var out []string
	for _, pair := range d.Pairs {
		if pair.Is(key) {
			out = append(out, pair.Value)
		}
	}
	return out
}
