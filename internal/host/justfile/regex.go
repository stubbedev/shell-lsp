package justfile

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	errRegexEnd    = errors.New("the pattern ends inside an escape, group or class")
	regexHexDigits = map[rune]int{'x': 2, 'u': 4, 'U': 8}
)

const (
	regexFlags          = "imsUuxR"
	regexClassEscapes   = "dswDSW"
	regexLiteralEscapes = "aftnrv"
	regexAssertions     = "AzbB<>"
	regexWordBoundaries = "start end start-half end-half"
)

type regexChecker struct {
	src      []rune
	pos      int
	names    map[string]bool
	extended []bool
}

func checkRegex(pattern string) error {
	for _, p := range []string{pattern, "^(?:" + pattern + ")$"} {
		r := &regexChecker{src: []rune(p), names: map[string]bool{}, extended: []bool{false}}
		if err := r.alternation(0); err != nil {
			return fmt.Errorf("at character %d of `%s`: %w", r.pos+1, p, err)
		}
	}
	return nil
}

func (r *regexChecker) more() bool { return r.pos < len(r.src) }

func (r *regexChecker) peek() rune { return r.src[r.pos] }

func (r *regexChecker) next() (rune, error) {
	r.space()
	if !r.more() {
		return 0, errRegexEnd
	}
	c := r.src[r.pos]
	r.pos++
	return c, nil
}

func (r *regexChecker) space() {
	if !r.extended[len(r.extended)-1] {
		return
	}
	for r.more() {
		switch c := r.peek(); {
		case unicode.IsSpace(c):
			r.pos++
		case c == '#':
			for r.more() && r.peek() != '\n' {
				r.pos++
			}
		default:
			return
		}
	}
}

func (r *regexChecker) alternation(depth int) error {
	repeatable := false
	for r.space(); r.more(); r.space() {
		c := r.peek()
		r.pos++
		var err error
		switch c {
		case '|':
			repeatable = false
		case ')':
			if depth == 0 {
				return errors.New("`)` closes no group")
			}
			return nil
		case '(':
			repeatable, err = r.group(depth)
		case '[':
			repeatable, err = true, r.class()
		case '*', '+', '?':
			err = r.repetition(repeatable)
		case '{':
			err = r.counted(repeatable)
		case '\\':
			repeatable, err = true, r.escape(false)
		default:
			repeatable = true
		}
		if err != nil {
			return err
		}
	}
	if depth > 0 {
		return errors.New("a group is not closed")
	}
	return nil
}

func (r *regexChecker) repetition(repeatable bool) error {
	if !repeatable {
		return errors.New("a repetition operator has no expression to repeat")
	}
	r.space()
	if r.more() && r.peek() == '?' {
		r.pos++
	}
	return nil
}

func (r *regexChecker) counted(repeatable bool) error {
	if !repeatable {
		return errors.New("a counted repetition has no expression to repeat")
	}
	lowest, err := r.decimal()
	if err != nil {
		return err
	}
	highest := lowest
	if r.blank(); r.more() && r.peek() == ',' {
		r.pos++
		if r.more() && r.peek() != '}' {
			if highest, err = r.decimal(); err != nil {
				return err
			}
		}
	}
	if r.blank(); !r.more() || r.peek() != '}' {
		return errors.New("a counted repetition is not closed with `}`")
	}
	r.pos++
	if lowest > highest {
		return errors.New("a counted repetition's minimum exceeds its maximum")
	}
	return r.repetition(true)
}

func (r *regexChecker) blank() {
	r.space()
	for r.more() && unicode.IsSpace(r.peek()) {
		r.pos++
	}
}

func (r *regexChecker) decimal() (uint64, error) {
	r.blank()
	start := r.pos
	for r.more() && r.peek() >= '0' && r.peek() <= '9' {
		r.pos++
	}
	n, err := strconv.ParseUint(string(r.src[start:r.pos]), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("a counted repetition needs a decimal count: %w", err)
	}
	return n, nil
}

func (r *regexChecker) group(depth int) (bool, error) {
	extended := r.extended[len(r.extended)-1]
	if !r.more() || r.peek() != '?' {
		return true, r.scoped(extended, depth)
	}
	r.pos++
	if r.more() && (r.peek() == 'P' || r.peek() == '<') {
		return true, r.capture(extended, depth)
	}
	scoped, flags, err := r.flags()
	if err != nil {
		return false, err
	}
	if strings.ContainsRune(flags.on, 'x') {
		extended = true
	}
	if strings.ContainsRune(flags.off, 'x') {
		extended = false
	}
	if scoped {
		return true, r.scoped(extended, depth)
	}
	r.extended[len(r.extended)-1] = extended
	return false, nil
}

func (r *regexChecker) scoped(extended bool, depth int) error {
	r.extended = append(r.extended, extended)
	defer func() { r.extended = r.extended[:len(r.extended)-1] }()
	return r.alternation(depth + 1)
}

func (r *regexChecker) capture(extended bool, depth int) error {
	if r.peek() == 'P' {
		r.pos++
		if !r.more() || r.peek() != '<' {
			return errors.New("`(?P` starts no named group; backreferences are not supported")
		}
	}
	r.pos++
	start := r.pos
	for r.more() && r.peek() != '>' {
		if c := r.peek(); !unicode.IsLetter(c) && c != '_' && (r.pos == start || !unicode.IsDigit(c) && c != '.' && c != '[' && c != ']') {
			return fmt.Errorf("`%c` cannot appear in a group name there", c)
		}
		r.pos++
	}
	if !r.more() {
		return errRegexEnd
	}
	name := string(r.src[start:r.pos])
	r.pos++
	switch {
	case name == "":
		return errors.New("a group name is empty")
	case r.names[name]:
		return fmt.Errorf("the group name `%s` is used twice", name)
	}
	r.names[name] = true
	return r.scoped(extended, depth)
}

type regexFlagSet struct{ on, off string }

func (r *regexChecker) flags() (bool, regexFlagSet, error) {
	var set regexFlagSet
	negated, empty := false, true
	for {
		if !r.more() {
			return false, set, errRegexEnd
		}
		c := r.peek()
		r.pos++
		switch {
		case c == ':' || c == ')':
			if negated && empty {
				return false, set, errors.New("a flag group ends right after `-`")
			}
			if c == ')' && set.on == "" && set.off == "" {
				return false, set, errors.New("a flag group sets no flags")
			}
			return c == ':', set, nil
		case c == '-':
			if negated {
				return false, set, errors.New("a flag group negates twice")
			}
			negated, empty = true, true
		case strings.ContainsRune(regexFlags, c):
			if strings.ContainsRune(set.on+set.off, c) {
				return false, set, fmt.Errorf("the flag `%c` is set twice", c)
			}
			if negated {
				set.off += string(c)
			} else {
				set.on += string(c)
			}
			empty = false
		default:
			return false, set, fmt.Errorf("`%c` is no flag; look-around and other Perl groups are not supported", c)
		}
	}
}

type regexItem uint8

const (
	regexItemLiteral regexItem = iota
	regexItemClass
	regexItemAssertion
)

func (r *regexChecker) escape(inClass bool) error {
	_, _, err := r.escaped(inClass)
	return err
}

func (r *regexChecker) escaped(inClass bool) (regexItem, rune, error) {
	if !r.more() {
		return 0, 0, errRegexEnd
	}
	c := r.peek()
	r.pos++
	switch {
	case c >= '0' && c <= '9':
		return 0, 0, errors.New("backreferences and octal escapes are not supported")
	case regexHexDigits[c] > 0:
		value, err := r.hex(regexHexDigits[c])
		return regexItemLiteral, value, err
	case c == 'p' || c == 'P':
		return regexItemClass, 0, r.unicodeClass()
	case strings.ContainsRune(regexClassEscapes, c):
		return regexItemClass, 0, nil
	case strings.ContainsRune(regexLiteralEscapes, c):
		return regexItemLiteral, c, nil
	case strings.ContainsRune(regexAssertions, c):
		if inClass {
			return 0, 0, fmt.Errorf("the assertion `\\%c` cannot appear in a class", c)
		}
		if c == 'b' {
			return regexItemAssertion, 0, r.wordBoundary()
		}
		return regexItemAssertion, 0, nil
	case c < utf8.RuneSelf && !unicode.IsLetter(c):
		return regexItemLiteral, c, nil
	}
	return 0, 0, fmt.Errorf("`\\%c` is no escape sequence", c)
}

func (r *regexChecker) hex(digits int) (rune, error) {
	var text []rune
	switch {
	case r.more() && r.peek() == '{':
		end := slices.Index(r.src[r.pos:], '}')
		if end < 0 {
			return 0, errRegexEnd
		}
		text = r.src[r.pos+1 : r.pos+end]
		r.pos += end + 1
	case r.pos+digits <= len(r.src):
		text = r.src[r.pos : r.pos+digits]
		r.pos += digits
	default:
		return 0, errRegexEnd
	}
	value, err := strconv.ParseUint(string(text), 16, 32)
	if err != nil || value > unicode.MaxRune || value >= 0xD800 && value <= 0xDFFF {
		return 0, errors.New("a hexadecimal escape names no Unicode scalar value")
	}
	return rune(value), nil
}

func (r *regexChecker) unicodeClass() error {
	if !r.more() {
		return errRegexEnd
	}
	c := r.peek()
	r.pos++
	name := string(c)
	switch c {
	case '\\':
		return errors.New("a Unicode class cannot be named by an escape")
	case '{':
		end := slices.Index(r.src[r.pos:], '}')
		if end < 0 {
			return errRegexEnd
		}
		name = string(r.src[r.pos : r.pos+end])
		r.pos += end + 1
	}
	if !unicodeClassKnown(name) {
		return fmt.Errorf("`%s` names no Unicode class", name)
	}
	return nil
}

func unicodeClassKnown(query string) bool {
	for _, op := range []string{"!=", ":", "="} {
		if property, value, ok := strings.Cut(query, op); ok {
			return unicodePropertyValueKnown(property, value)
		}
	}
	norm := normalizeUnicodeName(query)
	if canonical, ok := regexPropertyNames[norm]; ok && !slices.Contains(unicodeCategoryAbbreviations, norm) {
		_, boolean := slices.BinarySearch(regexBooleanProperties, canonical)
		return boolean || slices.Contains(unicodeBuiltinProperties, canonical)
	}
	return unicodeValueKnown("General_Category", norm) || unicodeValueKnown("Script", norm)
}

func unicodePropertyValueKnown(property, value string) bool {
	canonical, ok := regexPropertyNames[normalizeUnicodeName(property)]
	return ok && unicodeValueKnown(canonical, normalizeUnicodeName(value))
}

func unicodeValueKnown(property, norm string) bool {
	if property == "General_Category" && slices.Contains(unicodeGeneralCategoryExtras, norm) {
		return true
	}
	_, found := slices.BinarySearch(regexPropertyValues[property], norm)
	return found
}

func normalizeUnicodeName(name string) string {
	prefixed := len(name) >= 2 && strings.EqualFold(name[:2], "is")
	if prefixed {
		name = name[2:]
	}
	var sb strings.Builder
	for _, b := range []byte(name) {
		switch {
		case b == ' ' || b == '_' || b == '-' || b >= utf8.RuneSelf:
		case b >= 'A' && b <= 'Z':
			sb.WriteByte(b + 'a' - 'A')
		default:
			sb.WriteByte(b)
		}
	}
	if prefixed && sb.String() == "c" {
		return "isc"
	}
	return sb.String()
}

func (r *regexChecker) wordBoundary() error {
	if !r.more() || r.peek() != '{' {
		return nil
	}
	start := r.pos + 1
	end := start
	for end < len(r.src) && (unicode.IsLetter(r.src[end]) || r.src[end] == '-') {
		end++
	}
	if end == start {
		return nil
	}
	if end == len(r.src) || r.src[end] != '}' || !slices.Contains(strings.Fields(regexWordBoundaries), string(r.src[start:end])) {
		return fmt.Errorf("`\\b{%s` names no word boundary", string(r.src[start:end]))
	}
	r.pos = end + 1
	return nil
}

func (r *regexChecker) class() error {
	r.space()
	if r.more() && r.peek() == '^' {
		r.pos++
	}
	first := true
	for {
		r.space()
		if !r.more() {
			return errors.New("a class is not closed with `]`")
		}
		c := r.peek()
		switch {
		case c == ']' && !first:
			r.pos++
			return nil
		case c == '[':
			r.pos++
			if r.posix() {
				break
			}
			if err := r.class(); err != nil {
				return err
			}
		case r.operator():
			r.pos += 2
		default:
			if err := r.classRange(); err != nil {
				return err
			}
		}
		first = false
	}
}

func (r *regexChecker) operator() bool {
	if r.pos+1 >= len(r.src) {
		return false
	}
	c := r.src[r.pos]
	return (c == '&' || c == '-' || c == '~') && r.src[r.pos+1] == c
}

func (r *regexChecker) posix() bool {
	rest := string(r.src[r.pos:])
	if !strings.HasPrefix(rest, ":") {
		return false
	}
	body, _, closed := strings.Cut(rest[1:], ":]")
	if !closed || !slices.Contains(posixClasses, strings.TrimPrefix(body, "^")) {
		return false
	}
	r.pos += utf8.RuneCountInString(body) + len("::]")
	return true
}

var (
	unicodeCategoryAbbreviations = []string{"cf", "sc", "lc"}
	unicodeBuiltinProperties     = []string{"Decimal_Number", "White_Space"}
	unicodeGeneralCategoryExtras = []string{"any", "assigned", "ascii"}
)

var posixClasses = []string{"alnum", "alpha", "ascii", "blank", "cntrl", "digit", "graph", "lower", "print", "punct", "space", "upper", "word", "xdigit"}

func (r *regexChecker) classRange() error {
	start, low, err := r.classAtom()
	if err != nil {
		return err
	}
	r.space()
	if r.pos+1 >= len(r.src) || r.peek() != '-' || r.src[r.pos+1] == ']' || r.src[r.pos+1] == '-' {
		return nil
	}
	r.pos++
	end, high, err := r.classAtom()
	switch {
	case err != nil:
		return err
	case start != regexItemLiteral || end != regexItemLiteral:
		return errors.New("a class range must run between two characters")
	case low > high:
		return errors.New("a class range runs backwards")
	}
	return nil
}

func (r *regexChecker) classAtom() (regexItem, rune, error) {
	c, err := r.next()
	if err != nil {
		return 0, 0, err
	}
	if c == '\\' {
		return r.escaped(true)
	}
	return regexItemLiteral, c, nil
}
