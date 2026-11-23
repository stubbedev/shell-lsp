package bake

import (
	"strings"
	"unicode/utf8"
)

type part struct {
	Text     string
	Variable bool
}

type template struct {
	Parts   []part
	Dynamic bool
}

type variables map[string]template

type environment map[string]Value

var unknown = template{Dynamic: true}

func plainTemplate(text string) template { return template{Parts: []part{{Text: text}}} }

func (t template) plain() (string, bool) {
	if t.Dynamic {
		return "", false
	}
	var sb strings.Builder
	for _, p := range t.Parts {
		if p.Variable {
			return "", false
		}
		sb.WriteString(p.Text)
	}
	return sb.String(), true
}

func (t template) resolve(env environment) Value {
	return t.substitute(func(name string) Value { return env[name] })
}

func (t template) substitute(lookup func(name string) Value) Value {
	if t.Dynamic {
		return Value{}
	}
	var sb strings.Builder
	for _, p := range t.Parts {
		if !p.Variable {
			sb.WriteString(p.Text)
			continue
		}
		v := lookup(p.Text)
		if !v.Known {
			return Value{}
		}
		sb.WriteString(v.Text)
	}
	return Value{Text: sb.String(), Known: true}
}

func (vars variables) environment() environment {
	env := make(environment, len(vars))
	pending := map[string]bool{}
	var value func(name string) Value
	value = func(name string) Value {
		if v, done := env[name]; done {
			return v
		}
		t, defined := vars[name]
		if !defined || pending[name] {
			return Value{}
		}
		pending[name] = true
		v := t.substitute(value)
		delete(pending, name)
		env[name] = v
		return v
	}
	for name := range vars {
		value(name)
	}
	return env
}

type templateBuilder struct {
	literal strings.Builder
	parts   []part
	dynamic bool
}

func (b *templateBuilder) text(s string) { b.literal.WriteString(s) }

func (b *templateBuilder) reference(name string) {
	if name == "" {
		b.dynamic = true
		return
	}
	b.flush()
	b.parts = append(b.parts, part{Text: name, Variable: true})
}

func (b *templateBuilder) flush() {
	if b.literal.Len() == 0 {
		return
	}
	b.parts = append(b.parts, part{Text: b.literal.String()})
	b.literal.Reset()
}

func (b *templateBuilder) build() template {
	if b.dynamic {
		return unknown
	}
	b.flush()
	return template{Parts: b.parts}
}

func identifier(text string) bool {
	r, size := utf8.DecodeRuneInString(text)
	if !identStart(r) {
		return false
	}
	for _, next := range text[size:] {
		if !identPart(next) {
			return false
		}
	}
	return true
}
