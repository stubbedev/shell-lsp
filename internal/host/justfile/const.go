package justfile

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type constants struct {
	f        *File
	visiting map[string]bool
}

func (f *File) Constant(e *Expr) ([]string, bool) {
	c := constants{f: f, visiting: map[string]bool{}}
	return c.eval(e)
}

func (c constants) scalar(e *Expr) (string, bool) {
	values, ok := c.eval(e)
	if !ok || len(values) != 1 {
		return "", false
	}
	return values[0], true
}

func (c constants) eval(e *Expr) ([]string, bool) {
	if e == nil {
		return nil, false
	}
	switch e.Kind {
	case ExprString:
		s, ok := c.str(e.Str)
		if !ok {
			return nil, false
		}
		return []string{s}, true
	case ExprGroup:
		return c.eval(e.Args[0])
	case ExprList:
		var out []string
		for _, item := range e.Args {
			values, ok := c.eval(item)
			if !ok {
				return nil, false
			}
			out = append(out, values...)
		}
		return out, true
	case ExprBinary:
		return c.binary(e)
	case ExprRoot:
		s, ok := c.scalar(e.Args[0])
		if !ok {
			return nil, false
		}
		return []string{"/" + s}, true
	case ExprIf:
		return c.conditional(e)
	case ExprVariable:
		return c.variable(e.Name.Text)
	case ExprBacktick, ExprCall, ExprNot, ExprBad:
	}
	return nil, false
}

func (c constants) binary(e *Expr) ([]string, bool) {
	left, okLeft := c.scalar(e.Args[0])
	right, okRight := c.scalar(e.Args[1])
	if !okLeft || !okRight {
		return nil, false
	}
	switch e.Op {
	case "+":
		return []string{left + right}, true
	case "/":
		return []string{left + "/" + right}, true
	}
	return nil, false
}

func (c constants) conditional(e *Expr) ([]string, bool) {
	holds, ok := c.condition(e.Args[0])
	switch {
	case !ok:
		return nil, false
	case holds:
		return c.eval(e.Args[1])
	case len(e.Args) > 2:
		return c.eval(e.Args[2])
	}
	return nil, true
}

func (c constants) condition(cond *Expr) (bool, bool) {
	if cond.Kind != ExprBinary {
		return false, false
	}
	left, okLeft := c.scalar(cond.Args[0])
	right, okRight := c.scalar(cond.Args[1])
	if !okLeft || !okRight {
		return false, false
	}
	switch cond.Op {
	case "==":
		return left == right, true
	case "!=":
		return left != right, true
	case "=~", "!~":
		re, err := regexp.Compile(right)
		if err != nil {
			return false, false
		}
		return re.MatchString(left) == (cond.Op == "=~"), true
	}
	return false, false
}

func (c constants) variable(name string) ([]string, bool) {
	if c.visiting[name] {
		return nil, false
	}
	for _, a := range slices.Backward(c.f.Assignments()) {
		if a.Name.Text == name {
			c.visiting[name] = true
			defer delete(c.visiting, name)
			return c.eval(a.Value)
		}
	}
	return nil, false
}

func (c constants) str(lit *StringLit) (string, bool) {
	switch lit.Prefix {
	case 'x':
		return "", false
	case 'f':
		return c.format(lit)
	}
	return lit.Value, true
}

func (c constants) format(lit *StringLit) (string, bool) {
	var template strings.Builder
	var values []string
	for _, seg := range lit.Segments {
		switch seg.Kind {
		case FragmentText:
			template.WriteString(c.f.Text[seg.Span.Start:seg.Span.End])
		case FragmentEscape:
			template.WriteString("{{")
		case FragmentInterpolation:
			value, ok := c.scalar(seg.Interp)
			if !ok {
				return "", false
			}
			template.WriteString(placeholder(len(values)))
			values = append(values, value)
		}
	}
	out, invalid := cook(template.String(), !lit.Raw(), lit.Indented())
	if invalid != "" {
		return "", false
	}
	for i, v := range values {
		out = strings.Replace(out, placeholder(i), v, 1)
	}
	return out, true
}

func placeholder(i int) string { return "\x00" + strconv.Itoa(i) + "\x00" }
