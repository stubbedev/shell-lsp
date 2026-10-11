package embedded

import (
	"slices"
	"sort"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

type Program struct {
	Language Language
	Text     string
	Result   lang.Result
	pieces   []piece
}

type piece struct {
	at, source, length int
	joined             bool
}

func Find(tree *parser.Tree) []Program {
	bats := tree.Bats
	if !slices.ContainsFunc(tree.Commands, func(c parser.Command) bool { return mayEmbed(c.Name, bats) }) {
		return nil
	}
	var programs []Program
	parser.Walk(tree.File, func(node syntax.Node) bool {
		if stmt, ok := node.(*syntax.Stmt); ok {
			programs = appendPrograms(programs, tree, stmt)
		}
		return true
	})
	return programs
}

func In(tree *parser.Tree, stmt *syntax.Stmt) []Program {
	return appendPrograms(nil, tree, stmt)
}

func appendPrograms(programs []Program, tree *parser.Tree, stmt *syntax.Stmt) []Program {
	program, ok := embeddedIn(tree, stmt, tree.Bats)
	if !ok {
		return programs
	}
	program.Result = languages[program.Language].parse(program.Text)
	return append(programs, program)
}

func mayEmbed(name string, bats bool) bool {
	if (parser.Wrappers{Bats: bats}).Has(name) {
		return true
	}
	_, ok := languageOf(name)
	return ok
}

func embeddedIn(tree *parser.Tree, stmt *syntax.Stmt, bats bool) (Program, bool) {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok {
		return Program{}, false
	}
	at := parser.Wrappers{Bats: bats}.Start(call.Args)
	if at < 0 {
		return Program{}, false
	}
	name, ok := parser.Literal(call.Args[at])
	if !ok {
		return Program{}, false
	}
	language, ok := languageOf(name)
	if !ok {
		return Program{}, false
	}
	inv, ok := languages[language].cli.scan(call.Args[at+1:])
	if !ok {
		return Program{}, false
	}
	program := Program{Language: language}
	if inv.stdin {
		program.Text, program.pieces, ok = heredocProgram(tree, stmt)
	} else {
		program.Text, program.pieces, ok = decode(inv.scripts)
	}
	return program, ok && len(program.pieces) > 0
}

func heredocProgram(tree *parser.Tree, stmt *syntax.Stmt) (string, []piece, bool) {
	var input *syntax.Redirect
	for _, r := range stmt.Redirs {
		if r.N == nil || r.N.Value == "0" {
			input = r
		}
	}
	if input == nil || (input.Op != syntax.Hdoc && input.Op != syntax.DashHdoc) {
		return "", nil, false
	}
	if _, quoted := parser.HeredocDelimiter(input.Word); !quoted {
		return "", nil, false
	}
	at := parser.Offset(input.OpPos)
	i := slices.IndexFunc(tree.Heredocs, func(h parser.Heredoc) bool { return h.Span.Start == at })
	if i < 0 {
		return "", nil, false
	}
	body := tree.Heredocs[i].Body
	text := tree.Text()[body.Start:body.End]
	if !tree.Heredocs[i].StripTabs {
		return text, []piece{{source: body.Start, length: len(text)}}, true
	}
	var d decoder
	d.text.Grow(len(text))
	lineStart := true
	for i := range len(text) {
		if lineStart && text[i] == '\t' {
			continue
		}
		lineStart = text[i] == '\n'
		d.emit(text[i], body.Start+i)
	}
	return d.text.String(), d.pieces, true
}

func decode(scripts []script) (string, []piece, bool) {
	if len(scripts) == 1 && scripts[0].skip == 0 && len(scripts[0].word.Parts) == 1 {
		if q, ok := scripts[0].word.Parts[0].(*syntax.SglQuoted); ok && !q.Dollar && closed(q.Right) && q.Value != "" {
			return q.Value, []piece{{source: parser.Offset(q.Left) + 1, length: len(q.Value)}}, true
		}
	}
	var d decoder
	d.text.Grow(decodedSize(scripts))
	for i, s := range scripts {
		if i > 0 {
			d.text.WriteByte('\n')
		}
		d.drop = s.skip
		if !d.word(s.word) {
			return "", nil, false
		}
	}
	return d.text.String(), d.pieces, true
}

func decodedSize(scripts []script) int {
	size := len(scripts)
	for _, s := range scripts {
		for _, part := range s.word.Parts {
			switch p := part.(type) {
			case *syntax.SglQuoted:
				size += len(p.Value)
			case *syntax.Lit:
				size += len(p.Value)
			case *syntax.DblQuoted:
				for _, inner := range p.Parts {
					if lit, ok := inner.(*syntax.Lit); ok {
						size += len(lit.Value)
					}
				}
			}
		}
	}
	return size
}

func closed(right syntax.Pos) bool { return right.IsValid() && !right.IsRecovered() }

type decoder struct {
	text    strings.Builder
	pieces  []piece
	drop    int
	partial bool
}

func (d *decoder) emit(c byte, source int) {
	if d.drop > 0 {
		d.drop--
		return
	}
	at := d.text.Len()
	d.text.WriteByte(c)
	if n := len(d.pieces); n > 0 {
		last := &d.pieces[n-1]
		if last.at+last.length == at && last.source+last.length == source {
			last.length++
			return
		}
	}
	d.pieces = append(d.pieces, piece{at: at, source: source, length: 1, joined: d.partial})
	d.partial = true
}

func (d *decoder) word(w *syntax.Word) bool {
	for i, part := range w.Parts {
		d.partial = false
		switch p := part.(type) {
		case *syntax.SglQuoted:
			if p.Dollar || !closed(p.Right) {
				return false
			}
			start := parser.Offset(p.Left) + 1
			for j := range len(p.Value) {
				d.emit(p.Value[j], start+j)
			}
		case *syntax.DblQuoted:
			if p.Dollar || !closed(p.Right) {
				return false
			}
			for _, inner := range p.Parts {
				lit, ok := inner.(*syntax.Lit)
				if !ok {
					return false
				}
				d.unescape(lit.Value, parser.Offset(lit.ValuePos), parser.IsDoubleQuoteEscapable)
			}
		case *syntax.Lit:
			if expands(p.Value, i == 0) {
				return false
			}
			d.unescape(p.Value, parser.Offset(p.ValuePos), func(byte) bool { return true })
		default:
			return false
		}
	}
	return true
}

func (d *decoder) unescape(value string, source int, escapable func(byte) bool) {
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+1 < len(value) && escapable(value[i+1]) {
			i++
			if value[i] == '\n' {
				continue
			}
		}
		d.emit(value[i], source+i)
	}
}

func expands(lit string, leading bool) bool {
	if leading && strings.HasPrefix(lit, "~") {
		return true
	}
	for i := 0; i < len(lit); i++ {
		switch lit[i] {
		case '\\':
			i++
		case '*', '?', '[', '{':
			return true
		}
	}
	return false
}

func (p *Program) pieceAfter(offset int) int {
	return sort.Search(len(p.pieces), func(i int) bool { return p.pieces[i].at+p.pieces[i].length > offset })
}

func (p *Program) Spans(start, end int, yield func(parser.Span)) {
	var run parser.Span
	open := false
	for i := p.pieceAfter(start); i < len(p.pieces) && p.pieces[i].at < end; i++ {
		pc := p.pieces[i]
		from, to := max(start, pc.at), min(end, pc.at+pc.length)
		span := parser.Span{Start: pc.source + from - pc.at, End: pc.source + to - pc.at}
		if open && pc.joined && from == pc.at {
			run.End = span.End
			continue
		}
		if open {
			yield(run)
		}
		if pc.joined && from == pc.at && p.escaped(i) {
			span.Start--
		}
		run, open = span, true
	}
	if open {
		yield(run)
	}
}

func (p *Program) escaped(i int) bool {
	previous := p.pieces[i-1]
	return previous.source+previous.length == p.pieces[i].source-1
}

func (p *Program) Span(start, end int) parser.Span {
	found := parser.Span{Start: -1}
	p.Spans(start, max(end, start+1), func(s parser.Span) {
		if found.Start < 0 {
			found = s
		}
	})
	if found.Start >= 0 {
		return found
	}
	last := p.pieces[max(p.pieceAfter(start)-1, 0)]
	end = last.source + last.length
	return parser.Span{Start: end, End: end}
}

func (p *Program) Regions(yield func(parser.Span)) {
	var run parser.Span
	for i, pc := range p.pieces {
		if i > 0 && !pc.joined {
			yield(run)
		}
		if i == 0 || !pc.joined {
			run.Start = pc.source
		}
		run.End = pc.source + pc.length
	}
	if len(p.pieces) > 0 {
		yield(run)
	}
}

func (p *Program) offset(source int) (int, bool) {
	for _, pc := range p.pieces {
		if pc.source <= source && source < pc.source+pc.length {
			return pc.at + source - pc.source, true
		}
	}
	return 0, false
}

type Hover struct {
	Markdown string
	Span     parser.Span
}

func HoverAt(programs []Program, source int) (Hover, bool) {
	for i := range programs {
		p := &programs[i]
		at, ok := p.offset(source)
		if !ok {
			continue
		}
		tok, ok := p.Result.TokenAt(at)
		if !ok || tok.Modifiers&lang.Builtin == 0 {
			return Hover{}, false
		}
		doc, ok := languages[p.Language].doc(p.Text[tok.Start:tok.End], tok.Kind)
		if !ok {
			return Hover{}, false
		}
		return Hover{Markdown: doc.Markdown(p.Language.String()), Span: p.Span(tok.Start, tok.End)}, true
	}
	return Hover{}, false
}
