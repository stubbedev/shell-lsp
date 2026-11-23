package gitrepo

import "strings"

type wildResult uint8

const (
	wildMatch wildResult = iota
	wildNoMatch
	wildAbortAll
	wildAbortToStarStar
)

func wildmatch(pattern, text string, fold bool) bool {
	return dowild(pattern, text, fold) == wildMatch
}

func byteAt(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return 0
}

func foldByte(c byte, fold bool) byte {
	if fold {
		return lower(c)
	}
	return c
}

func isGlobSpecial(c byte) bool { return c == '*' || c == '?' || c == '[' || c == '\\' }

func dowild(pat, text string, fold bool) wildResult {
	t := 0
	for p := 0; p < len(pat); p, t = p+1, t+1 {
		tc := byteAt(text, t)
		if tc == 0 && pat[p] != '*' {
			return wildAbortAll
		}
		tc, pc := foldByte(tc, fold), foldByte(pat[p], fold)
		switch pc {
		case '\\':
			p++
			if tc != byteAt(pat, p) {
				return wildNoMatch
			}
		case '?':
			if tc == '/' {
				return wildNoMatch
			}
		case '*':
			p++
			matchSlash := false
			if byteAt(pat, p) == '*' {
				prev := p - 2
				for p++; byteAt(pat, p) == '*'; p++ {
				}
				next := byteAt(pat, p)
				if (prev < 0 || pat[prev] == '/') && (next == 0 || next == '/' || next == '\\' && byteAt(pat, p+1) == '/') {
					if next == '/' && dowild(pat[p+1:], text[t:], fold) == wildMatch {
						return wildMatch
					}
					matchSlash = true
				}
			}
			if p == len(pat) {
				if !matchSlash && strings.IndexByte(text[t:], '/') >= 0 {
					return wildNoMatch
				}
				return wildMatch
			}
			if !matchSlash && pat[p] == '/' {
				slash := strings.IndexByte(text[t:], '/')
				if slash < 0 {
					return wildNoMatch
				}
				t += slash
				continue
			}
			return star(pat[p:], text[t:], fold, matchSlash)
		case '[':
			if r, ok := class(pat, &p, tc, fold); !ok {
				return r
			}
		default:
			if tc != pc {
				return wildNoMatch
			}
		}
	}
	if t < len(text) {
		return wildNoMatch
	}
	return wildMatch
}

func star(pat, text string, fold, matchSlash bool) wildResult {
	t := 0
	tc := byteAt(text, t)
	for tc != 0 {
		if !isGlobSpecial(pat[0]) {
			pc := foldByte(pat[0], fold)
			for {
				tc = byteAt(text, t)
				if tc == 0 || !matchSlash && tc == '/' {
					break
				}
				tc = foldByte(tc, fold)
				if tc == pc {
					break
				}
				t++
			}
			if tc != pc {
				return wildNoMatch
			}
		}
		matched := dowild(pat, text[t:], fold)
		if matched != wildNoMatch {
			if !matchSlash || matched != wildAbortToStarStar {
				return matched
			}
		} else if !matchSlash && tc == '/' {
			return wildAbortToStarStar
		}
		t++
		tc = byteAt(text, t)
	}
	return wildAbortAll
}

func class(pat string, at *int, tc byte, fold bool) (wildResult, bool) {
	p := *at + 1
	pc := byteAt(pat, p)
	if pc == '^' {
		pc = '!'
	}
	negated := pc == '!'
	if negated {
		p++
		pc = byteAt(pat, p)
	}
	var prev byte
	matched := false
	for {
		if pc == 0 {
			return wildAbortAll, false
		}
		switch {
		case pc == '\\':
			p++
			pc = byteAt(pat, p)
			if pc == 0 {
				return wildAbortAll, false
			}
			matched = matched || tc == pc
		case pc == '-' && prev != 0 && byteAt(pat, p+1) != 0 && byteAt(pat, p+1) != ']':
			p++
			pc = byteAt(pat, p)
			if pc == '\\' {
				p++
				pc = byteAt(pat, p)
				if pc == 0 {
					return wildAbortAll, false
				}
			}
			upper := tc - 'a' + 'A'
			matched = matched || prev <= tc && tc <= pc || fold && 'a' <= tc && tc <= 'z' && prev <= upper && upper <= pc
			pc = 0
		case pc == '[' && byteAt(pat, p+1) == ':':
			p += 2
			start := p
			for pc = byteAt(pat, p); pc != 0 && pc != ']'; pc = byteAt(pat, p) {
				p++
			}
			if pc == 0 {
				return wildAbortAll, false
			}
			if p-start-1 < 0 || pat[p-1] != ':' {
				p = start - 2
				pc = '['
				matched = matched || tc == pc
				break
			}
			in, known := inClass(pat[start:p-1], tc, fold)
			if !known {
				return wildAbortAll, false
			}
			matched = matched || in
			pc = 0
		default:
			matched = matched || tc == pc
		}
		prev = pc
		p++
		if pc = byteAt(pat, p); pc == ']' {
			break
		}
	}
	*at = p
	if matched == negated || tc == '/' {
		return wildNoMatch, false
	}
	return wildMatch, true
}

func inClass(name string, c byte, fold bool) (bool, bool) {
	isUpper, isLower, isDigit := 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9'
	isPrint := 0x20 <= c && c < 0x7f
	switch name {
	case "alnum":
		return isUpper || isLower || isDigit, true
	case "alpha":
		return isUpper || isLower, true
	case "blank":
		return c == ' ' || c == '\t', true
	case "cntrl":
		return c < 0x20 || c == 0x7f, true
	case "digit":
		return isDigit, true
	case "graph":
		return isPrint && c != ' ', true
	case "lower":
		return isLower, true
	case "print":
		return isPrint, true
	case "punct":
		return isPrint && c != ' ' && !isUpper && !isLower && !isDigit, true
	case "space":
		return isGitSpace(c), true
	case "upper":
		return isUpper || fold && isLower, true
	case "xdigit":
		return isDigit || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F', true
	}
	return false, false
}
