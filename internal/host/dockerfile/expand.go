package dockerfile

import (
	"maps"
	"slices"
	"strings"
)

type binding struct {
	value string
	known bool
}

type environment map[string]binding

type expander struct {
	text   string
	escape byte
	env    environment
	at     int
	known  bool
}

func (f *File) evaluate(text string, env environment) (string, bool) {
	e := &expander{text: text, escape: f.Escape, env: env, known: true}
	out := e.word(func(byte) bool { return false })
	return out, e.known
}

func (e *expander) more() bool { return e.at < len(e.text) }

func (e *expander) word(stop func(byte) bool) string {
	var sb strings.Builder
	for e.more() && !stop(e.text[e.at]) {
		c := e.text[e.at]
		switch c {
		case e.escape:
			e.at++
			if !e.more() {
				sb.WriteByte(c)
				break
			}
			sb.WriteByte(e.text[e.at])
			e.at++
		case '\'':
			e.at++
			end := strings.IndexByte(e.text[e.at:], '\'')
			if end < 0 {
				sb.WriteString(e.text[e.at:])
				e.at = len(e.text)
				break
			}
			sb.WriteString(e.text[e.at : e.at+end])
			e.at += end + 1
		case '"':
			e.at++
			sb.WriteString(e.doubleQuoted())
		case '$':
			sb.WriteString(e.dollar())
		default:
			sb.WriteByte(c)
			e.at++
		}
	}
	return sb.String()
}

func (e *expander) doubleQuoted() string {
	var sb strings.Builder
	for e.more() && e.text[e.at] != '"' {
		c := e.text[e.at]
		switch {
		case c == e.escape && e.at+1 < len(e.text):
			next := e.text[e.at+1]
			if next != '"' && next != '$' && next != e.escape {
				sb.WriteByte(c)
			}
			sb.WriteByte(next)
			e.at += 2
		case c == '$':
			sb.WriteString(e.dollar())
		default:
			sb.WriteByte(c)
			e.at++
		}
	}
	if e.more() {
		e.at++
	}
	return sb.String()
}

func (e *expander) lookup(name string) (string, bool) {
	b, set := e.env[name]
	if set && !b.known {
		e.known = false
	}
	return b.value, set
}

func (e *expander) dollar() string {
	e.at++
	if !e.more() {
		return "$"
	}
	if e.text[e.at] != '{' {
		end := paramEnd(e.text, e.at, false)
		if end == e.at {
			return "$"
		}
		value, _ := e.lookup(e.text[e.at:end])
		e.at = end
		return value
	}
	e.at++
	end := paramEnd(e.text, e.at, true)
	if end == e.at {
		e.known = false
		e.skipBraced()
		return ""
	}
	name := e.text[e.at:end]
	e.at = end
	value, set := e.lookup(name)
	if !e.more() {
		e.known = false
		return value
	}
	op := e.text[e.at]
	if op == '}' {
		e.at++
		return value
	}
	colon := op == ':'
	if colon {
		e.at++
		if !e.more() {
			e.known = false
			return value
		}
		op = e.text[e.at]
	}
	if !slices.Contains([]byte("-+?#%/"), op) || colon && !slices.Contains([]byte("-+?"), op) {
		e.known = false
		e.skipBraced()
		return value
	}
	e.at++
	if op == '#' || op == '%' || op == '/' {
		e.known = false
		e.skipBraced()
		return value
	}
	present := set && (!colon || value != "")
	saved := e.known
	arg := e.word(func(c byte) bool { return c == '}' })
	if e.more() {
		e.at++
	} else {
		e.known = false
	}
	switch op {
	case '-':
		if present {
			e.known = saved
			return value
		}
		return arg
	case '+':
		if present {
			return arg
		}
		e.known = saved
		return ""
	}
	e.known = saved && present
	return value
}

func (e *expander) skipBraced() {
	for depth := 1; e.more() && depth > 0; e.at++ {
		switch e.text[e.at] {
		case '{':
			depth++
		case '}':
			depth--
		}
	}
}

func (f *File) bindings(env environment, in *Instruction) map[string]binding {
	out := map[string]binding{}
	for _, p := range f.Pairs(in) {
		if p.Value == nil {
			out[p.Key.Text] = binding{}
			continue
		}
		value, known := f.evaluate(p.Value.Text, env)
		out[p.Key.Text] = binding{value: value, known: known}
	}
	return out
}

func (f *File) declare(env environment, in *Instruction, fallback environment) {
	for _, p := range f.Pairs(in) {
		if p.Value == nil {
			if b, set := fallback[p.Key.Text]; set {
				env[p.Key.Text] = b
			}
			continue
		}
		value, known := f.evaluate(p.Value.Text, env)
		env[p.Key.Text] = binding{value: value, known: known}
	}
}

func (f *File) preambleEnvironment() environment {
	env := environment{}
	for _, name := range platformArgs {
		env[name] = binding{}
	}
	for _, in := range f.Preamble() {
		if in.Name == "ARG" {
			f.declare(env, in, environment{})
		}
	}
	return env
}

func (f *File) preambleDeclares(name string) bool {
	return slices.ContainsFunc(f.Preamble(), func(in *Instruction) bool {
		return in.Name == "ARG" && slices.Contains(f.declares(in), name)
	})
}

func (f *File) stageEnvironment(s Stage, stages []Stage, before *Instruction) environment {
	chain := []Stage{s}
	for i := s.Parent; i >= 0; i = stages[i].Parent {
		chain = append(chain, stages[i])
	}
	env := environment{"PATH": {value: defaultPath, known: true}}
	preamble := f.preambleEnvironment()
	for _, stage := range slices.Backward(chain) {
		for _, in := range stage.Instructions {
			if in == before {
				return env
			}
			switch in.Name {
			case "ENV":
				maps.Copy(env, f.bindings(env, in))
			case "ARG":
				f.declare(env, in, preamble)
			}
		}
	}
	return env
}
