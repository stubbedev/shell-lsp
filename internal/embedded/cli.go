package embedded

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type option uint8

const (
	unknown option = iota
	flag
	valued
	paired
	glued
	source
	file
	unsupported
	extended
)

type cli struct {
	short   [128]option
	long    map[string]option
	permute bool
}

type script struct {
	word *syntax.Word
	skip int
}

type programSource interface{ programSource() }

type inline []script

type fromStdin struct{}

func (inline) programSource() {}

func (fromStdin) programSource() {}

type invocation struct {
	program  programSource
	extended bool
}

type scanner struct {
	cli      *cli
	args     []*syntax.Word
	at       int
	scripts  []script
	files    int
	stdin    bool
	extended bool
	operand  *syntax.Word
}

const expansion = "\x00"

func wordText(w *syntax.Word) (string, bool) {
	text, literal := parser.Text(w, expansion)
	if literal {
		return text, true
	}
	cut, _, _ := strings.Cut(text, expansion)
	return cut, false
}

func (c *cli) scan(args []*syntax.Word) (invocation, bool) {
	s := scanner{cli: c, args: args}
	for ; s.at < len(args); s.at++ {
		text, complete := wordText(args[s.at])
		switch {
		case text == "--" && complete:
			if s.operand == nil && s.at+1 < len(args) {
				s.operand = args[s.at+1]
			}
			return s.finish()
		case len(text) > 1 && text[0] == '-' && (s.operand == nil || c.permute):
			if !s.option(text, complete) {
				return invocation{}, false
			}
		default:
			if s.operand == nil {
				s.operand = args[s.at]
			}
			if !c.permute {
				return s.finish()
			}
		}
	}
	return s.finish()
}

func (s *scanner) option(text string, complete bool) bool {
	if long, ok := strings.CutPrefix(text, "--"); ok {
		name, value, hasValue := strings.Cut(long, "=")
		return s.apply(s.cli.long[name], len(text)-len(value), hasValue, value, complete)
	}
	for i := 1; i < len(text); i++ {
		if text[i] >= byte(len(s.cli.short)) {
			return false
		}
		opt := s.cli.short[text[i]]
		if opt == extended {
			s.extended = true
			continue
		}
		if opt != flag {
			return s.apply(opt, i+1, i+1 < len(text) || !complete, text[i+1:], complete)
		}
	}
	return complete
}

func (s *scanner) apply(opt option, at int, attached bool, value string, complete bool) bool {
	switch opt {
	case flag, glued:
		return true
	case extended:
		s.extended = true
		return true
	case valued:
		return attached || s.take()
	case paired:
		return !attached && s.take() && s.take()
	case source:
		if attached {
			s.scripts = append(s.scripts, script{word: s.args[s.at], skip: at})
			return complete
		}
		if !s.take() {
			return false
		}
		s.scripts = append(s.scripts, script{word: s.args[s.at]})
		return true
	case file:
		if !attached {
			if !s.take() {
				return false
			}
			value, complete = wordText(s.args[s.at])
		}
		s.files++
		s.stdin = value == "-" || value == "/dev/stdin"
		return complete
	}
	return false
}

func (s *scanner) take() bool {
	s.at++
	return s.at < len(s.args)
}

func (s *scanner) finish() (invocation, bool) {
	switch {
	case s.files > 0:
		return invocation{program: fromStdin{}, extended: s.extended}, s.files == 1 && s.stdin && len(s.scripts) == 0
	case len(s.scripts) > 0:
		return invocation{program: inline(s.scripts), extended: s.extended}, true
	case s.operand != nil:
		return invocation{program: inline{{word: s.operand}}, extended: s.extended}, true
	}
	return invocation{}, false
}
