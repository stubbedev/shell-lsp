package shlex

import "slices"

type frameKind uint8

const (
	frameOpen frameKind = iota
	frameSubst
	frameArray
)

type state uint8

const (
	stateCommand state = iota
	statePrefix
	stateArgs
	stateDeclArgs
	stateTarget
	stateLoopName
	stateLoopIn
	stateFunctionName
	stateSubject
	stateCaseIn
	stateCaseItem
	statePattern
)

type construct uint8

const (
	constructParen construct = iota
	constructCase
)

type frame struct {
	kind     frameKind
	tokens   []Token
	state    state
	resume   state
	operator Kind
	heredocs []heredoc
	cond     bool
	regex    bool
	open     []construct
}

func (f *frame) allowsArrays() bool {
	return f.kind != frameArray && !f.cond && (f.state == stateCommand || f.state == statePrefix || f.state == stateDeclArgs)
}

func (f *frame) opensArithmetic() bool {
	return f.kind != frameArray && !f.cond && (f.state == stateCommand || f.state == stateLoopName)
}

func (f *frame) inside(c construct) bool {
	return len(f.open) > 0 && f.open[len(f.open)-1] == c
}

func (f *frame) close(c construct) {
	if f.inside(c) {
		f.open = f.open[:len(f.open)-1]
	}
}

func (f *frame) nest(k Kind) bool {
	switch k {
	case LParen:
		f.open = append(f.open, constructParen)
	case RParen:
		f.close(constructParen)
	case AndIf, OrIf:
	default:
		return false
	}
	return true
}

func (f *frame) newline() {
	f.operator = 0
	if f.kind == frameArray || f.cond {
		return
	}
	switch f.state {
	case stateLoopName, stateLoopIn, stateFunctionName, stateSubject, stateCaseIn, stateCaseItem, statePattern:
	case stateCommand, statePrefix, stateArgs, stateDeclArgs, stateTarget:
		f.state = stateCommand
	}
}

func (f *frame) afterOperator(k Kind) {
	f.operator = 0
	if f.kind == frameArray {
		f.nest(k)
		return
	}
	if f.cond && f.nest(k) {
		return
	}
	f.cond = false
	inPattern := f.state == stateCaseItem || f.state == statePattern
	switch {
	case k == LParen && inPattern:
		f.state = statePattern
	case k == RParen && inPattern:
		f.state = stateCommand
	case k == Pipe && f.state == statePattern:
	case k == DSemi || k == SemiAnd || k == DSemiAnd || k == SemiPipe:
		f.state = stateCommand
		if f.inside(constructCase) {
			f.state = stateCaseItem
		}
	case k.isRedirect():
		if f.state != stateTarget {
			f.resume = f.state
		}
		if f.resume == stateCommand {
			f.resume = statePrefix
		}
		f.state = stateTarget
		if k == DLess || k == DLessDash {
			f.operator = k
		}
	default:
		f.nest(k)
		f.state = stateCommand
	}
}

func (f *frame) word(src string, t *Token) {
	if f.kind == frameArray {
		return
	}
	word, plain := t.Literal(src)
	if f.cond {
		f.condition(t, word, plain)
		return
	}
	switch f.state {
	case stateTarget:
		f.state, f.operator = f.resume, 0
	case stateCommand:
		t.Flags |= CommandStart
		if !plain || !f.reserved(t, word) {
			f.command(src, t, word, plain)
		}
	case statePrefix:
		f.command(src, t, word, plain)
	case stateLoopName:
		f.state = stateLoopIn
	case stateLoopIn:
		f.state = stateArgs
		switch {
		case plain && word == "in":
			t.Flags |= Keyword
		case plain && word == "do":
			t.Flags |= Keyword
			f.state = stateCommand
		}
	case stateFunctionName:
		f.state = stateCommand
	case stateSubject:
		f.state = stateCaseIn
	case stateCaseIn:
		f.state = stateArgs
		if plain && word == "in" {
			t.Flags |= Keyword
			f.open = append(f.open, constructCase)
			f.state = stateCaseItem
		}
	case stateCaseItem:
		f.state = statePattern
		if plain && word == "esac" {
			t.Flags |= Keyword
			f.close(constructCase)
			f.state = stateCommand
		}
	case statePattern, stateArgs, stateDeclArgs:
	}
}

func (f *frame) condition(t *Token, word string, plain bool) {
	switch {
	case !plain:
	case word == "]]":
		t.Flags |= Keyword
		f.cond = false
		f.state = stateCommand
	case word == "=~":
		f.regex = true
	}
}

func (f *frame) reserved(t *Token, word string) bool {
	switch word {
	case "{", "}":
		t.Kind, t.Flags, t.Parts = LBrace, 0, nil
		if word == "}" {
			t.Kind = RBrace
		}
		f.state = stateCommand
		return true
	case "if", "then", "else", "elif", "fi", "do", "done", "while", "until", "!", "time":
		f.state = stateCommand
	case "esac":
		f.close(constructCase)
		f.state = stateCommand
	case "for", "select":
		f.state = stateLoopName
	case "case":
		f.state = stateSubject
	case "function":
		f.state = stateFunctionName
	case "[[":
		f.cond = true
	default:
		return false
	}
	t.Flags |= Keyword
	return true
}

func (f *frame) command(src string, t *Token, word string, plain bool) {
	if _, ok := AssignmentPrefix(t.Text(src)); ok {
		f.state = statePrefix
		return
	}
	f.state = stateArgs
	if plain && slices.Contains(declarations, word) {
		f.state = stateDeclArgs
	}
}
