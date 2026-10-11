package cli

import (
	"fmt"
	"strings"
)

type argKind uint8

const (
	noArg argKind = iota
	requiredArg
	optionalArg
)

type option[T any] struct {
	short byte
	long  string
	kind  argKind
	apply func(into *T, value string) error
}

type argError struct {
	status int
	err    error
}

func (e *argError) Error() string { return e.err.Error() }

func (e *argError) Unwrap() error { return e.err }

func badSyntax(format string, args ...any) error {
	return &argError{status: checkStatusSyntax, err: fmt.Errorf(format, args...)}
}

func badValue(format string, args ...any) error {
	return &argError{status: checkStatusOptions, err: fmt.Errorf(format, args...)}
}

func parseGNU[T any](args []string, options []option[T], into *T) ([]string, error) {
	var operands []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			return append(operands, args[i+1:]...), nil
		case strings.HasPrefix(arg, "--"):
			consumed, err := parseLong(arg[2:], args[i+1:], options, into)
			if err != nil {
				return nil, err
			}
			i += consumed
		case len(arg) > 1 && arg[0] == '-':
			consumed, err := parseShorts(arg[1:], args[i+1:], options, into)
			if err != nil {
				return nil, err
			}
			i += consumed
		default:
			operands = append(operands, arg)
		}
	}
	return operands, nil
}

func parseLong[T any](arg string, rest []string, options []option[T], into *T) (int, error) {
	name, value, inline := strings.Cut(arg, "=")
	opt, err := longOption(name, options)
	if err != nil {
		return 0, err
	}
	switch opt.kind {
	case noArg:
		if inline {
			return 0, badSyntax("option `--%s' doesn't allow an argument", opt.long)
		}
		return 0, opt.apply(into, "")
	case optionalArg:
		return 0, opt.apply(into, value)
	}
	if inline {
		return 0, opt.apply(into, value)
	}
	if len(rest) == 0 {
		return 0, badSyntax("option `--%s' requires an argument", opt.long)
	}
	return 1, opt.apply(into, rest[0])
}

func longOption[T any](name string, options []option[T]) (option[T], error) {
	var matches []option[T]
	for _, opt := range options {
		if opt.long == name {
			return opt, nil
		}
		if opt.long != "" && strings.HasPrefix(opt.long, name) {
			matches = append(matches, opt)
		}
	}
	switch len(matches) {
	case 0:
		return option[T]{}, badSyntax("unrecognized option `--%s'", name)
	case 1:
		return matches[0], nil
	}
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = "--" + m.long
	}
	return option[T]{}, badSyntax("option `--%s' is ambiguous; could be %s", name, strings.Join(names, ", "))
}

func parseShorts[T any](cluster string, rest []string, options []option[T], into *T) (int, error) {
	for j := range len(cluster) {
		opt, ok := shortOption(cluster[j], options)
		if !ok {
			return 0, badSyntax("unrecognized option `-%c'", cluster[j])
		}
		attached := cluster[j+1:]
		switch opt.kind {
		case noArg:
			if err := opt.apply(into, ""); err != nil {
				return 0, err
			}
			continue
		case optionalArg:
			return 0, opt.apply(into, attached)
		}
		if attached != "" {
			return 0, opt.apply(into, attached)
		}
		if len(rest) == 0 {
			return 0, badSyntax("option `-%c' requires an argument", opt.short)
		}
		return 1, opt.apply(into, rest[0])
	}
	return 0, nil
}

func shortOption[T any](c byte, options []option[T]) (option[T], bool) {
	for _, opt := range options {
		if opt.short != 0 && opt.short == c {
			return opt, true
		}
	}
	return option[T]{}, false
}
