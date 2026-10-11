package getopt

import "strings"

type Arity uint8

const (
	Unknown Arity = iota
	Flag
	Valued
)

const EndOfOptions = "--"

func IsOption(word string) bool {
	return len(word) > 1 && word[0] == '-' && word != EndOfOptions
}

func Words(option string, arity func(name string) Arity) (int, bool) {
	if strings.HasPrefix(option, EndOfOptions) {
		if strings.Contains(option, "=") {
			return 1, true
		}
		return span(arity(option))
	}
	if a := arity(option); a != Unknown || len(option) == 2 {
		return span(a)
	}
	for i := 1; i < len(option); i++ {
		switch arity(option[:1] + option[i:i+1]) {
		case Unknown:
			return 1, false
		case Valued:
			if i == len(option)-1 {
				return 2, true
			}
			return 1, true
		}
	}
	return 1, true
}

func span(a Arity) (int, bool) {
	switch a {
	case Valued:
		return 2, true
	case Flag:
		return 1, true
	}
	return 1, false
}
