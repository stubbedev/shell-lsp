package regex

import (
	"strconv"
	"strings"
)

type Flavor uint8

const (
	Basic Flavor = iota
	Extended
	Awk
	Oniguruma
)

type Error struct {
	Start, End int
	Message    string
}

func (e *Error) Error() string { return e.Message }

type Pattern struct {
	groups int
	known  bool
}

func (p Pattern) Groups() (int, bool) { return p.groups, p.known }

const maxTrackedGroups = 32

var classNames = map[string]bool{
	"alnum": true, "alpha": true, "blank": true, "cntrl": true, "digit": true, "graph": true,
	"lower": true, "print": true, "punct": true, "space": true, "upper": true, "xdigit": true,
}

type outcome uint8

const (
	proceed outcome = iota
	unknown
	invalid
)

type scanner struct {
	src        string
	flavor     Flavor
	pos        int
	groups     int
	depth      int
	opened     [maxTrackedGroups]int
	repeatable bool
	reference  int
	referredAt int
	err        *Error
}

func Check(pattern string, flavor Flavor) (Pattern, *Error) {
	s := scanner{src: pattern, flavor: flavor}
	for s.pos < len(s.src) {
		var next outcome
		switch c := s.src[s.pos]; c {
		case '\\':
			next = s.escape()
		case '[':
			next = s.bracket()
		default:
			next = s.operator(c)
		}
		switch next {
		case invalid:
			return Pattern{}, s.err
		case unknown:
			return Pattern{}, nil
		case proceed:
		}
	}
	if s.depth > 0 {
		at, width := s.opened[0], s.groupWidth()
		return Pattern{}, s.error(at, at+width, "`"+s.src[at:at+width]+"` is never closed in the regular expression")
	}
	if s.reference > s.groups {
		at := s.referredAt
		return Pattern{}, s.error(at, at+2, "`"+s.src[at:at+2]+"` refers to group "+strconv.Itoa(s.reference)+", but the regular expression has "+GroupCount(s.groups))
	}
	return Pattern{groups: s.groups, known: true}, nil
}

func GroupCount(n int) string {
	switch n {
	case 0:
		return "no groups"
	case 1:
		return "only 1 group"
	}
	return "only " + strconv.Itoa(n) + " groups"
}

func (s *scanner) error(start, end int, message string) *Error {
	return &Error{Start: start, End: end, Message: message}
}

func (s *scanner) fail(start, end int, message string) outcome {
	s.err = s.error(start, end, message)
	return invalid
}

func (s *scanner) groupWidth() int {
	if s.flavor == Basic {
		return 2
	}
	return 1
}

func (s *scanner) open(at int) outcome {
	if s.flavor != Oniguruma {
		s.groups++
	}
	if s.depth < maxTrackedGroups {
		s.opened[s.depth] = at
	}
	s.depth++
	s.repeatable = false
	return proceed
}

func (s *scanner) close(at, width int) outcome {
	s.repeatable = true
	switch {
	case s.depth > 0:
		s.depth--
	case s.flavor == Basic || s.flavor == Oniguruma:
		return s.fail(at, at+width, "`"+s.src[at:at+width]+"` has no matching opening parenthesis in the regular expression")
	}
	return proceed
}

func (s *scanner) escape() outcome {
	at := s.pos
	if at+1 == len(s.src) {
		return s.fail(at, at+1, "the regular expression ends with a lone `\\`")
	}
	c := s.src[at+1]
	s.pos += 2
	s.repeatable = true
	switch {
	case s.flavor == Basic && c == '(':
		return s.open(at)
	case s.flavor == Basic && c == ')':
		return s.close(at, 2)
	case s.flavor == Basic && c == '|':
		s.repeatable = false
	case s.flavor == Basic && c == '{':
		return s.basicInterval(at)
	case (s.flavor == Basic || s.flavor == Extended) && c >= '1' && c <= '9':
		if n := int(c - '0'); n > s.reference {
			s.reference, s.referredAt = n, at
		}
	case s.flavor == Oniguruma && c == 'Q':
		return unknown
	}
	return proceed
}

func (s *scanner) basicInterval(at int) outcome {
	end := strings.Index(s.src[s.pos:], `\}`)
	if end < 0 {
		return s.fail(at, at+2, "`\\{` is never closed in the regular expression")
	}
	body := s.src[s.pos : s.pos+end]
	s.pos += end + 2
	low, high, ranged := strings.Cut(body, ",")
	if !isCount(low) || (ranged && high != "" && !isCount(high)) {
		return s.fail(at, s.pos, "invalid interval `"+s.src[at:s.pos]+"`: it must hold a count such as 2, 1,3 or 2,")
	}
	return s.interval(at, low, high)
}

func (s *scanner) braceInterval(at int) outcome {
	end := strings.IndexByte(s.src[s.pos:], '}')
	if end < 0 {
		s.repeatable = true
		return proceed
	}
	low, high, ranged := strings.Cut(s.src[s.pos:s.pos+end], ",")
	if !isCount(low) || (ranged && high != "" && !isCount(high)) {
		s.repeatable = true
		return proceed
	}
	s.pos += end + 1
	return s.interval(at, low, high)
}

func (s *scanner) interval(at int, low, high string) outcome {
	if high != "" && count(low) > count(high) {
		return s.fail(at, s.pos, "the interval `"+s.src[at:s.pos]+"` has a minimum above its maximum")
	}
	return proceed
}

func isCount(digits string) bool {
	return digits != "" && len(digits) <= 5 && strings.Trim(digits, "0123456789") == ""
}

func count(digits string) int {
	n, err := strconv.Atoi(digits)
	if err != nil {
		panic("regex: interval count " + strconv.Quote(digits) + " was checked to be digits: " + err.Error())
	}
	return n
}

func (s *scanner) operator(c byte) outcome {
	at := s.pos
	s.pos++
	extended := s.flavor != Basic
	switch {
	case extended && c == '(':
		if s.flavor != Oniguruma || !strings.HasPrefix(s.src[s.pos:], "?") {
			return s.open(at)
		}
		if strings.HasPrefix(s.src[s.pos:], "?#") {
			return unknown
		}
		s.pos++
		return s.open(at)
	case extended && c == ')':
		return s.close(at, 1)
	case extended && c == '|':
		s.repeatable = false
	case c == '*' || (extended && (c == '+' || c == '?')):
		if s.repeatable || s.flavor == Basic || s.flavor == Awk || (at > 0 && s.src[at-1] == '^') {
			s.repeatable = true
			return proceed
		}
		return s.fail(at, at+1, "`"+string(c)+"` has nothing to repeat")
	case c == '{' && (s.flavor == Extended || s.flavor == Oniguruma):
		return s.braceInterval(at)
	case c != '^':
		s.repeatable = true
	}
	return proceed
}

func (s *scanner) bracket() outcome {
	at := s.pos
	if s.flavor == Oniguruma {
		return s.onigurumaBracket(at)
	}
	end, closed := bracketEnd(s.src, at, false)
	if s.flavor == Awk {
		escapedEnd, escapedClosed := bracketEnd(s.src, at, true)
		switch {
		case !closed && !escapedClosed:
		case closed != escapedClosed || end != escapedEnd:
			return unknown
		case strings.IndexByte(s.src[at:end], '\\') >= 0:
			s.pos, s.repeatable = end, true
			return proceed
		}
	}
	if !closed {
		return s.fail(at, at+1, "`[` is never closed in the regular expression")
	}
	s.pos, s.repeatable = end, true
	return s.bracketContents(at, end, true)
}

func BracketEnd(pattern string, at int) (int, bool) { return bracketEnd(pattern, at, false) }

func bracketEnd(src string, at int, escapes bool) (int, bool) {
	i := skipBracketStart(src, at+1, len(src))
	for i < len(src) {
		switch c := src[i]; {
		case c == ']':
			return i + 1, true
		case escapes && c == '\\':
			i += 2
			continue
		case c == '[' && i+1 < len(src) && strings.IndexByte(":.=", src[i+1]) >= 0:
			if end := strings.Index(src[i+2:], src[i+1:i+2]+"]"); end >= 0 {
				i += end + 4
				continue
			}
		}
		i++
	}
	return 0, false
}

func skipBracketStart(src string, i, end int) int {
	if i < end && src[i] == '^' {
		i++
	}
	if i < end && src[i] == ']' {
		i++
	}
	return i
}

func (s *scanner) bracketContents(at, end int, classes bool) outcome {
	last := end - 1
	for i := skipBracketStart(s.src, at+1, last); i < last; {
		c := s.src[i]
		if c == '[' && i+1 < last && strings.IndexByte(":.=", s.src[i+1]) >= 0 {
			close := strings.Index(s.src[i+2:last], s.src[i+1:i+2]+"]")
			if close < 0 {
				return proceed
			}
			if name := s.src[i+2 : i+2+close]; classes && s.src[i+1] == ':' && !classNames[name] {
				return s.fail(i, i+close+4, "unknown character class `"+s.src[i:i+close+4]+"`")
			}
			i += close + 4
			continue
		}
		if i+2 < last && s.src[i+1] == '-' {
			if low, high := s.src[i], s.src[i+2]; sameClass(low, high) && low > high {
				return s.fail(i, i+3, "the range `"+s.src[i:i+3]+"` is empty because `"+string(low)+"` sorts after `"+string(high)+"`")
			}
			i += 3
			continue
		}
		i++
	}
	return proceed
}

func sameClass(a, b byte) bool {
	switch {
	case a >= 'a' && a <= 'z':
		return b >= 'a' && b <= 'z'
	case a >= 'A' && a <= 'Z':
		return b >= 'A' && b <= 'Z'
	case a >= '0' && a <= '9':
		return b >= '0' && b <= '9'
	}
	return false
}

func (s *scanner) onigurumaBracket(at int) outcome {
	depth := 0
	plain := true
	for i := at; i < len(s.src); i++ {
		switch c := s.src[i]; {
		case c == '\\':
			plain = false
			i++
		case c == '[' && strings.HasPrefix(s.src[i+1:], ":") && strings.Contains(s.src[i+2:], ":]"):
			i += strings.Index(s.src[i+2:], ":]") + 3
		case c == '[':
			if depth > 0 {
				plain = false
			}
			depth++
			i = skipBracketStart(s.src, i+1, len(s.src)) - 1
		case c == ']':
			if depth--; depth > 0 {
				continue
			}
			s.pos, s.repeatable = i+1, true
			if !plain {
				return proceed
			}
			return s.bracketContents(at, i+1, false)
		}
	}
	return s.fail(at, at+1, "`[` is never closed in the regular expression")
}
