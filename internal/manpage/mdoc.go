package manpage

import (
	"slices"
	"strings"
)

const defaultArgument = "file ..."

var mdocCallable = map[string]bool{
	"Ad": true, "An": true, "Ap": true, "Aq": true, "Ar": true, "At": true, "Bq": true, "Brq": true, "Bsx": true, "Bx": true,
	"Cm": true, "Dq": true, "Dv": true, "Em": true, "Er": true, "Ev": true, "Fa": true, "Fl": true, "Fn": true, "Ft": true,
	"Fx": true, "Ic": true, "Li": true, "Lk": true, "Ms": true, "Mt": true, "Nm": true, "No": true, "Ns": true, "Nx": true,
	"Op": true, "Ox": true, "Pa": true, "Pf": true, "Pq": true, "Ql": true, "Qq": true, "Sq": true, "St": true, "Sx": true,
	"Sy": true, "Tn": true, "Ux": true, "Va": true, "Xr": true, "Oo": true, "Oc": true, "Dl": true, "Eo": true, "Ec": true,
	"Do": true, "Dc": true, "Po": true, "Pc": true, "Qo": true, "Qc": true, "So": true, "Sc": true, "Bo": true, "Bc": true,
	"Xo": true, "Xc": true, "Ek": true, "Bk": true,
}

var mdocEnclosures = map[string][2]string{
	"Dq": {"“", "”"}, "Sq": {"‘", "’"}, "Pq": {"(", ")"}, "Qq": {"\"", "\""},
	"Bq": {"[", "]"}, "Brq": {"{", "}"}, "Aq": {"<", ">"}, "Op": {"[", "]"},
}

var mdocOpeners = map[string]string{"Oo": "[", "Do": "“", "Po": "(", "Qo": "\"", "So": "‘", "Bo": "[", "Eo": ""}

var mdocClosers = map[string]string{"Oc": "]", "Dc": "”", "Pc": ")", "Qc": "\"", "Sc": "’", "Bc": "]", "Ec": ""}

var mdocSystems = map[string]string{
	"Ux": "UNIX", "Bx": "BSD", "Ox": "OpenBSD", "Nx": "NetBSD", "Fx": "FreeBSD", "Bsx": "BSD/OS", "At": "AT&T UNIX", "St": "POSIX",
}

func isPunctuation(word string) bool {
	return len(word) == 1 && strings.Contains(".,:;)]?!", word)
}

func isOpening(word string) bool {
	return len(word) == 1 && strings.Contains("([", word)
}

type mdocLine struct {
	b       *builder
	words   []string
	out     strings.Builder
	noSpace bool
}

func (m *mdocLine) emit(word string) {
	switch {
	case m.out.Len() == 0, m.noSpace, isPunctuation(word):
	case strings.HasSuffix(m.out.String(), "(") || strings.HasSuffix(m.out.String(), "["):
	default:
		m.out.WriteByte(' ')
	}
	m.out.WriteString(word)
	m.noSpace = isOpening(word)
}

func (m *mdocLine) render() {
	for i := 0; i < len(m.words); {
		word := m.words[i]
		if !mdocCallable[word] {
			m.emit(m.b.st.inline(word))
			i++
			continue
		}
		i = m.macro(word, i+1)
	}
}

func (m *mdocLine) argsUntilMacro(from int) ([]string, int) {
	end := from
	for end < len(m.words) && !mdocCallable[m.words[end]] {
		end++
	}
	return m.words[from:end], end
}

func (m *mdocLine) macro(name string, next int) int {
	if pair, ok := mdocEnclosures[name]; ok {
		inner := &mdocLine{b: m.b, words: m.words[next:]}
		inner.render()
		body := strings.TrimRight(inner.out.String(), ".,:;")
		trailing := inner.out.String()[len(body):]
		m.emit(pair[0] + body + pair[1] + trailing)
		return len(m.words)
	}
	if open, ok := mdocOpeners[name]; ok {
		m.emit(open)
		m.noSpace = true
		return next
	}
	if closer, ok := mdocClosers[name]; ok {
		m.noSpace = true
		m.emit(closer)
		m.noSpace = false
		return next
	}
	if system, ok := mdocSystems[name]; ok {
		_, end := m.argsUntilMacro(next)
		m.emit(system)
		return end
	}
	switch name {
	case "Ns":
		m.noSpace = true
		return next
	case "Ap":
		m.noSpace = true
		m.emit("'")
		m.noSpace = true
		return next
	}
	args, end := m.argsUntilMacro(next)
	switch name {
	case "Fl":
		if len(args) == 0 || isPunctuation(args[0]) {
			m.emit("-")
		}
		for _, a := range args {
			if isPunctuation(a) {
				m.emit(a)
				continue
			}
			m.emit("-" + m.b.st.inline(a))
		}
	case "Ar":
		if len(args) == 0 || isPunctuation(args[0]) {
			m.emit(defaultArgument)
		}
		m.emitAll(args)
	case "Nm":
		if len(args) > 0 && !isPunctuation(args[0]) && m.b.name == "" {
			m.b.name = m.b.st.inline(args[0])
		}
		if len(args) == 0 || isPunctuation(args[0]) {
			m.emit(m.b.name)
		}
		m.emitAll(args)
	case "Xr":
		if len(args) >= 2 {
			m.emit(m.b.st.inline(args[0]) + "(" + args[1] + ")")
			m.emitAll(args[2:])
			break
		}
		m.emitAll(args)
	case "Pf":
		if len(args) > 0 {
			m.emit(m.b.st.inline(args[0]))
			m.noSpace = true
			m.emitAll(args[1:])
		}
	case "Ek", "Bk", "Xo", "Xc":
	default:
		m.emitAll(args)
	}
	return end
}

func (m *mdocLine) emitAll(words []string) {
	for _, w := range words {
		m.emit(m.b.st.inline(w))
	}
}

func (b *builder) mdocText(rest string) string {
	m := &mdocLine{b: b, words: requestArgs(rest)}
	m.render()
	return m.out.String()
}

func (b *builder) mdocRequest(name, rest string) {
	switch name {
	case "Dd", "Dt", "Os":
	case "Sh":
		b.startSection(b.mdocText(rest))
	case "Ss", "Pp", "Lp":
		b.flush()
	case "Nm":
		if b.currentSection() == "SYNOPSIS" {
			b.flush()
		}
		b.appendRendered(b.mdocText("Nm " + rest))
	case "Nd":
		b.appendRendered("- " + b.mdocText(rest))
	case "Bl":
		b.flush()
		kind := listPlain
		if slices.ContainsFunc(requestArgs(rest), func(a string) bool {
			return a == "-tag" || a == "-ohang" || a == "-hang" || a == "-inset" || a == "-diag"
		}) {
			kind = listTagged
		}
		b.lists = append(b.lists, kind)
	case "El":
		b.flush()
		if len(b.lists) > 0 {
			b.lists = b.lists[:len(b.lists)-1]
		}
	case "It":
		b.flush()
		if len(b.lists) > 0 && b.lists[len(b.lists)-1] == listTagged {
			b.kind, b.tag = taggedBlock, b.mdocText(rest)
			return
		}
		b.appendRendered(b.mdocText(rest))
	case "Bd":
		b.flush()
		if strings.Contains(rest, "-literal") || strings.Contains(rest, "-unfilled") {
			b.nofill, b.kind = true, literalBlock
		}
	case "Ed":
		b.flush()
		b.nofill, b.kind = false, paragraphBlock
	case "D1", "Dl":
		b.flush()
		b.kind = literalBlock
		b.text.WriteString(b.mdocText(rest))
		b.flush()
	case "Ex":
		utility := b.name
		if args := requestArgs(rest); len(args) > 1 {
			utility = args[1]
		}
		b.appendRendered("The " + utility + " utility exits 0 on success, and >0 if an error occurs.")
	default:
		if mdocCallable[name] {
			b.appendRendered(b.mdocText(name + " " + rest))
		}
	}
}
