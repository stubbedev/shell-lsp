package scan

import (
	"fmt"
	"strings"
)

func Forward(start, end int, step func(at int) int) {
	for at := start; at < end; {
		next := step(at)
		if next <= at {
			panic(fmt.Sprintf("scan: the step at offset %d went to %d instead of forward", at, next))
		}
		at = next
	}
}

func Continued(line string) bool {
	return (len(line)-len(strings.TrimRight(line, `\`)))%2 == 1
}

func While(s string, in func(byte) bool) int {
	for i := range len(s) {
		if !in(s[i]) {
			return i
		}
	}
	return len(s)
}

func All(s string, in func(byte) bool) bool { return While(s, in) == len(s) }

func Digit(c byte) bool { return '0' <= c && c <= '9' }

func HexDigit(c byte) bool { return Digit(c) || 'a' <= c|0x20 && c|0x20 <= 'f' }

func Lower(c byte) bool { return 'a' <= c && c <= 'z' }

func Letter(c byte) bool { return Lower(c) || 'A' <= c && c <= 'Z' }

func Alnum(c byte) bool { return Letter(c) || Digit(c) }

func LowerOrDigit(c byte) bool { return Lower(c) || Digit(c) }

func Blank(c byte) bool { return c == ' ' || c == '\t' }

func TrimBlanks(s string) string { return s[While(s, Blank):] }
