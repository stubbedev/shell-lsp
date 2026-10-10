package manpage

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type blockKind uint8

const (
	paragraphBlock blockKind = iota
	taggedBlock
	literalBlock
)

type block struct {
	kind blockKind
	tag  string
	text string
}

type section struct {
	name   string
	blocks []block
}

type document struct {
	sections []section
}

func (d document) section(name string) (section, bool) {
	for _, s := range d.sections {
		if strings.EqualFold(s.name, name) {
			return s, true
		}
	}
	return section{}, false
}

var specialChars = map[string]string{
	"em": "—", "en": "–", "hy": "-", "mi": "-", "aq": "'", "dq": "\"", "lq": "“", "rq": "”",
	"oq": "‘", "cq": "’", "bu": "•", "co": "©", "rg": "®", "tm": "™", "de": "°",
	"ga": "`", "aa": "´", "ti": "~", "ha": "^", "rs": "\\", "sl": "/", "ba": "|", "br": "|", "or": "|",
	"at": "@", "sh": "#", "Do": "$", "Eu": "€", "Po": "£", "ct": "¢", "sc": "§", "ps": "¶",
	"dg": "†", "dd": "‡", ">=": "≥", "<=": "≤", "!=": "≠", "+-": "±", "mu": "×",
	"di": "÷", "->": "→", "<-": "←", "<>": "↔", "ua": "↑", "da": "↓", "ss": "ß",
	"Fo": "«", "Fc": "»", "fo": "‹", "fc": "›", "lB": "[", "rB": "]", "lC": "{", "rC": "}",
	"la": "⟨", "ra": "⟩", "pl": "+", "eq": "=", "sq": "□", "ul": "_", "rn": "‾", "bv": "|",
	"a:": "ä", "o:": "ö", "u:": "ü", "A:": "Ä", "O:": "Ö", "U:": "Ü", "e'": "é",
	"e`": "è", "a'": "á", "a`": "à", "o'": "ó", "i'": "í", "n~": "ñ", "c,": "ç",
}

var predefinedStrings = map[string]string{
	"lq": "“", "rq": "”", "R": "®", "Tm": "™", "S": "", "Lq": "“", "Rq": "”",
}

var escapesTakingDelimitedArgument = "XhvwoDlLbZSxR"

type roffState struct {
	strings map[string]string
}

func (st *roffState) inline(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 == len(s) {
			out.WriteByte(c)
			continue
		}
		i++
		switch e := s[i]; {
		case e == '"':
			return strings.TrimRight(out.String(), " \t")
		case e == '-':
			out.WriteByte('-')
		case e == 'e', e == '\\':
			out.WriteByte('\\')
		case e == ' ', e == '~', e == '0':
			out.WriteByte(' ')
		case e == 't':
			out.WriteByte(' ')
		case e == '`':
			out.WriteByte('`')
		case e == '\'':
			out.WriteString("´")
		case e == '.':
			out.WriteByte('.')
		case strings.IndexByte("&|^,/:%c{}a)!", e) >= 0:
		case e == 'f' || e == 'F' || e == 'n' || e == 'm' || e == 'M' || e == 'g' || e == 'k' || e == '$' || e == 's':
			i = skipEscapeName(s, i, e == 's')
		case e == '(':
			if i+2 < len(s) {
				out.WriteString(special(s[i+1 : i+3]))
				i += 2
			}
		case e == '[':
			end := strings.IndexByte(s[i:], ']')
			if end < 0 {
				return out.String()
			}
			out.WriteString(special(s[i+1 : i+end]))
			i += end
		case e == '*':
			name, next := escapeName(s, i)
			out.WriteString(st.lookupString(name))
			i = next
		case e == 'N':
			arg, next := delimited(s, i)
			if n, err := strconv.Atoi(arg); err == nil && n > 0 && n < utf8.MaxRune {
				out.WriteRune(rune(n))
			}
			i = next
		case strings.IndexByte(escapesTakingDelimitedArgument, e) >= 0:
			_, i = delimited(s, i)
		default:
			out.WriteByte(e)
		}
	}
	return out.String()
}

func special(name string) string {
	if c, ok := specialChars[name]; ok {
		return c
	}
	if hex, ok := strings.CutPrefix(name, "u"); ok {
		if n, err := strconv.ParseUint(strings.SplitN(hex, "_", 2)[0], 16, 32); err == nil && n <= unicode.MaxRune {
			return string(rune(n))
		}
	}
	if len(name) == 1 {
		return name
	}
	return ""
}

func (st *roffState) lookupString(name string) string {
	if v, ok := st.strings[name]; ok {
		return v
	}
	return predefinedStrings[name]
}

func escapeName(s string, at int) (string, int) {
	if at+1 >= len(s) {
		return "", at
	}
	switch s[at+1] {
	case '(':
		if at+3 < len(s) {
			return s[at+2 : at+4], at + 3
		}
		return "", len(s) - 1
	case '[':
		end := strings.IndexByte(s[at+1:], ']')
		if end < 0 {
			return "", len(s) - 1
		}
		return s[at+2 : at+1+end], at + 1 + end
	}
	return s[at+1 : at+2], at + 1
}

func skipEscapeName(s string, at int, signed bool) int {
	if signed && at+1 < len(s) && (s[at+1] == '+' || s[at+1] == '-') {
		at++
	}
	if signed && at+1 < len(s) && s[at+1] >= '0' && s[at+1] <= '9' {
		at++
		if at+1 < len(s) && s[at+1] >= '0' && s[at+1] <= '9' && s[at] < '4' {
			at++
		}
		return at
	}
	_, next := escapeName(s, at)
	return next
}

func delimited(s string, at int) (string, int) {
	if at+1 >= len(s) {
		return "", at
	}
	delim := s[at+1]
	end := strings.IndexByte(s[at+2:], delim)
	if end < 0 {
		return "", len(s) - 1
	}
	return s[at+2 : at+2+end], at + 2 + end
}

func requestArgs(line string) []string {
	var args []string
	for i := 0; i < len(line); {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i == len(line) {
			break
		}
		if line[i] == '"' {
			var arg strings.Builder
			i++
			for i < len(line) {
				if line[i] == '"' {
					if i+1 < len(line) && line[i+1] == '"' {
						arg.WriteByte('"')
						i += 2
						continue
					}
					i++
					break
				}
				arg.WriteByte(line[i])
				i++
			}
			args = append(args, arg.String())
			continue
		}
		start := i
		for i < len(line) && line[i] != ' ' && line[i] != '\t' {
			if line[i] == '\\' && i+1 < len(line) {
				i++
			}
			i++
		}
		args = append(args, line[start:i])
	}
	return args
}

func splitRequest(line string) (string, string) {
	body := strings.TrimLeft(line[1:], " \t")
	if strings.HasPrefix(body, `\"`) {
		return `\"`, ""
	}
	end := strings.IndexAny(body, " \t\\")
	if end < 0 {
		return body, ""
	}
	return body[:end], strings.TrimLeft(body[end:], " \t")
}
