package printf

import "strings"

const (
	flags       = "#-+ 0"
	conversions = "diouxXfFeEgGaAcsbqQSC"
	star        = '*'
)

var lengths = []string{"hh", "h", "ll", "l", "L", "j", "z", "t"}

type outcome uint8

const (
	specValid outcome = iota
	specInvalid
	specCut
	specCutAfterLength
)

func Conversions(format string) []byte {
	var out []byte
	scan(format, func(c byte) { out = append(out, c) })
	return out
}

func Pending(format string) bool {
	return scan(format, func(byte) {})
}

func scan(format string, convert func(byte)) bool {
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		if i+1 == len(format) {
			return true
		}
		switch format[i+1] {
		case '%':
			i++
			continue
		case '(':
			closing := strings.IndexByte(format[i+2:], ')')
			at := i + 2 + closing + 1
			if closing < 0 || at >= len(format) {
				return false
			}
			convert(format[at])
			i = at
			continue
		}
		end, stars, result := specification(format, i+1)
		switch result {
		case specCut:
			return true
		case specCutAfterLength:
			return false
		case specInvalid:
			convert(format[i+1])
			i++
		case specValid:
			for range stars {
				convert(star)
			}
			convert(format[end])
			i = end
		}
	}
	return false
}

func specification(format string, j int) (int, int, outcome) {
	for j < len(format) && strings.IndexByte(flags, format[j]) >= 0 {
		j++
	}
	j, stars := width(format, j, 0)
	if j < len(format) && format[j] == '.' {
		j++
	}
	j, stars = width(format, j, stars)
	if j == len(format) {
		return j, stars, specCut
	}
	for _, length := range lengths {
		if strings.HasPrefix(format[j:], length) {
			j += len(length)
			break
		}
	}
	switch {
	case j == len(format):
		return j, stars, specCutAfterLength
	case strings.IndexByte(conversions, format[j]) < 0:
		return j, stars, specInvalid
	}
	return j, stars, specValid
}

func width(format string, j, stars int) (int, int) {
	if j < len(format) && format[j] == star {
		return j + 1, stars + 1
	}
	for j < len(format) && format[j] >= '0' && format[j] <= '9' {
		j++
	}
	return j, stars
}
