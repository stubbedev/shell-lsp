package embedded

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

type Item struct {
	Label   string
	Detail  string
	Doc     string
	Span    parser.Span
	Binding bool
}

func CompletionsAt(programs []Program, source int) []Item {
	for i := range programs {
		p := &programs[i]
		at, ok := p.offset(max(source-1, 0))
		if !ok {
			continue
		}
		tok, ok := p.Result.TokenAt(at)
		if !ok || tok.Kind != lang.Function && tok.Kind != lang.Variable {
			return nil
		}
		return p.completions(tok, p.Text[tok.Start:min(at+1, tok.End)])
	}
	return nil
}

func (p *Program) completions(tok lang.Token, prefix string) []Item {
	span := p.Span(tok.Start, tok.End)
	var out []Item
	for _, b := range p.Bindings {
		if strings.HasPrefix(b.Name, prefix) {
			out = append(out, Item{Label: b.Name, Detail: b.Flag + " " + b.Name, Span: span, Binding: true})
		}
	}
	for name, doc := range languages[p.Language].names() {
		if strings.HasPrefix(name, prefix) {
			out = append(out, Item{Label: name, Detail: doc.Signature, Doc: doc.Summary, Span: span})
		}
	}
	slices.SortStableFunc(out, func(a, b Item) int { return strings.Compare(a.Label, b.Label) })
	return slices.CompactFunc(out, func(a, b Item) bool { return a.Label == b.Label })
}

type Signature struct {
	Label  string
	Doc    string
	Params []parser.Span
	Active int
}

func SignatureAt(programs []Program, source int) (Signature, bool) {
	for i := range programs {
		p := &programs[i]
		at, ok := p.offset(max(source-1, 0))
		if !ok {
			continue
		}
		return p.signature(at + 1)
	}
	return Signature{}, false
}

func (p *Program) signature(at int) (Signature, bool) {
	depth, active := 0, 0
	separator := languages[p.Language].argSeparator
	tokens := p.Result.Tokens
	next := len(tokens) - 1
	for i := min(at, len(p.Text)) - 1; i >= 0; i-- {
		for next >= 0 && tokens[next].Start > i {
			next--
		}
		if next >= 0 && i < tokens[next].End && opaqueToken(tokens[next].Kind) {
			i = tokens[next].Start
			continue
		}
		switch p.Text[i] {
		case ')':
			depth++
		case separator[0]:
			if depth == 0 {
				active++
			}
		case '(':
			if depth > 0 {
				depth--
				continue
			}
			return p.calleeSignature(i, next, separator, active)
		}
	}
	return Signature{}, false
}

func opaqueToken(kind lang.Kind) bool {
	return kind == lang.String || kind == lang.Regexp || kind == lang.Comment
}

func (p *Program) calleeSignature(open, before int, separator string, active int) (Signature, bool) {
	if before < 0 {
		return Signature{}, false
	}
	callee := p.Result.Tokens[before]
	if callee.Kind != lang.Function || strings.TrimSpace(p.Text[callee.End:open]) != "" {
		return Signature{}, false
	}
	doc, known := languages[p.Language].doc(p.Text[callee.Start:callee.End], callee)
	if !known {
		return Signature{}, false
	}
	return signatureOf(doc, separator, active)
}

func signatureOf(doc lang.Doc, separator string, active int) (Signature, bool) {
	label := doc.Signature
	if i := strings.LastIndex(label, ", "); i >= 0 && !strings.Contains(label[:i], "(") {
		label = label[i+2:]
	}
	open, end := strings.IndexByte(label, '('), strings.LastIndexByte(label, ')')
	if open < 0 || end <= open+1 {
		return Signature{}, false
	}
	var params []parser.Span
	start := open + 1
	nested := 0
	for i := open + 1; i <= end; i++ {
		switch {
		case i < end && label[i] == '[':
			nested++
		case i < end && label[i] == ']':
			nested--
		case i == end || nested == 0 && label[i] == separator[0]:
			params = append(params, trimmedParam(label, start, i))
			start = i + 1
		}
	}
	return Signature{Label: label, Doc: doc.Summary, Params: params, Active: min(active, len(params)-1)}, true
}

func trimmedParam(label string, start, end int) parser.Span {
	for start < end && label[start] == ' ' {
		start++
	}
	return parser.Span{Start: start, End: end}
}
