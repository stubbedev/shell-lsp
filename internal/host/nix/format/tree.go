package format

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

type triviumKind uint8

const (
	blankLine triviumKind = iota
	lineNote
	blockNote
	docNote
	languageNote
)

type trivium struct {
	kind  triviumKind
	text  string
	lines []string
}

type token struct {
	pre   []trivium
	text  string
	trail string
	tight bool
	line  int
}

func (t *token) note() string {
	if t.tight {
		return t.trail
	}
	return " " + t.trail
}

func (t *token) lone() bool { return len(t.pre) == 0 && t.trail == "" }

type expr interface{ exprNode() }

type term interface {
	expr
	termNode()
}

type termBase struct{}

func (termBase) exprNode() {}
func (termBase) termNode() {}

type exprBase struct{}

func (exprBase) exprNode() {}

type plainTerm struct {
	termBase
	tok *token
}

type stringPiece struct {
	text   string
	interp *interpolation
}

type interpolation struct {
	open  *token
	value expr
	close *token
}

type simplicity uint8

const (
	simplicityUnknown simplicity = iota
	simplicityYes
	simplicityNo
)

type stringTerm struct {
	termBase
	indented   bool
	open       *token
	pieces     []stringPiece
	simplicity simplicity
	probing    *int
}

type uriTerm struct {
	termBase
	tok *token
}

type pathTerm struct {
	termBase
	tok    *token
	pieces []stringPiece
}

type listEntry struct {
	comments []trivium
	value    expr
}

type listTerm struct {
	termBase
	open    *token
	entries []listEntry
	close   *token
}

type setEntry struct {
	comments []trivium
	binding  binding
}

type setTerm struct {
	termBase
	keyword *token
	open    *token
	entries []setEntry
	close   *token
}

type selector struct {
	dot   *token
	name  *token
	text  *stringTerm
	inter *interpolation
}

type selectTerm struct {
	termBase
	target   term
	path     []selector
	or       *token
	fallback term
}

type parenTerm struct {
	termBase
	open   *token
	inner  expr
	close  *token
	simple simplicity
}

type withExpr struct {
	exprBase
	keyword *token
	scope   expr
	semi    *token
	body    expr
}

type letExpr struct {
	exprBase
	keyword *token
	entries []setEntry
	in      *token
	body    expr
}

type assertExpr struct {
	exprBase
	keyword *token
	cond    expr
	semi    *token
	body    expr
}

type ifExpr struct {
	exprBase
	ifTok     *token
	cond      expr
	thenTok   *token
	then      expr
	elseTok   *token
	otherwise expr
}

type lambdaExpr struct {
	exprBase
	param param
	colon *token
	body  expr
}

type applyExpr struct {
	exprBase
	fn     expr
	args   []expr
	simple simplicity
	call   simplicity
}

type binaryExpr struct {
	exprBase
	op    syntax.Op
	left  expr
	opTok *token
	right expr
}

type hasAttrExpr struct {
	exprBase
	target expr
	mark   *token
	path   []selector
}

type unaryExpr struct {
	exprBase
	opTok   *token
	operand expr
}

type binding interface{ bindingNode() }

type assignBinding struct {
	path   []selector
	equals *token
	value  expr
	semi   *token
}

func (*assignBinding) bindingNode() {}

type inheritBinding struct {
	keyword *token
	from    *parenTerm
	names   []selector
	semi    *token
}

func (*inheritBinding) bindingNode() {}

type param interface{ paramNode() }

type nameParam struct{ name *token }

func (*nameParam) paramNode() {}

type formalEntry struct {
	comments []trivium
	name     *token
	mark     *token
	fallback expr
	comma    *token
}

type setParam struct {
	open    *token
	formals []formalEntry
	close   *token
}

func (*setParam) paramNode() {}

type aliasParam struct {
	first  param
	at     *token
	second param
}

func (*aliasParam) paramNode() {}

type converter struct {
	file    *syntax.File
	src     string
	at      int
	line    int
	seen    int
	probing int
}

func newConverter(file *syntax.File) *converter {
	return &converter{file: file, src: file.Text, line: 1}
}

func isStringFragment(k syntax.TokenKind) bool {
	return k == syntax.TokStringText || k == syntax.TokEscape || k == syntax.TokPathText
}

func (c *converter) lineAt(offset int) int {
	c.line += strings.Count(c.src[c.seen:offset], "\n")
	c.seen = offset
	return c.line
}

func trimComment(s string) string { return strings.TrimRightFunc(s, unicode.IsSpace) }

func commentBody(c syntax.Token, src string) string {
	raw := src[c.Span.Start:c.Span.End]
	if c.Kind == syntax.TokLineComment {
		return strings.TrimSpace(raw[1:])
	}
	return strings.TrimSpace(raw[2 : len(raw)-2])
}

func (c *converter) trailing() string {
	var parts []string
	tokens := c.file.Tokens
	for c.at < len(tokens) {
		t := tokens[c.at]
		switch t.Kind {
		case syntax.TokWhitespace:
			if strings.ContainsRune(c.src[t.Span.Start:t.Span.End], '\n') {
				return strings.Join(parts, " ")
			}
		case syntax.TokLineComment:
			if body := commentBody(t, c.src); body != "" {
				parts = append(parts, body)
			}
			c.at++
			return strings.Join(parts, " ")
		case syntax.TokBlockComment:
			raw := c.src[t.Span.Start:t.Span.End]
			if strings.ContainsRune(raw, '\n') {
				return strings.Join(parts, " ")
			}
			if isDocComment(raw) {
				if strings.TrimSpace(raw[3:len(raw)-2]) != "" {
					return strings.Join(parts, " ")
				}
				c.at++
				continue
			}
			if len(parts) == 0 && isLanguageWord(commentBody(t, c.src)) && c.stringFollows(c.at+1) {
				return ""
			}
			if body := commentBody(t, c.src); body != "" {
				parts = append(parts, body)
			}
		default:
			return strings.Join(parts, " ")
		}
		c.at++
	}
	return strings.Join(parts, " ")
}

func (c *converter) column(offset int) int {
	start := strings.LastIndexByte(c.src[:offset], '\n') + 1
	return utf8.RuneCountInString(c.src[start:offset])
}

func (c *converter) blockComment(t syntax.Token, stringNext bool) (trivium, bool) {
	raw := c.src[t.Span.Start:t.Span.End]
	if !strings.ContainsRune(raw, '\n') {
		if isDocComment(raw) {
			line := strings.TrimSpace(raw[3 : len(raw)-2])
			return trivium{kind: docNote, lines: []string{line}}, line != ""
		}
		body := strings.TrimSpace(raw[2 : len(raw)-2])
		switch {
		case body == "":
			return trivium{}, false
		case stringNext && isLanguageWord(body):
			return trivium{kind: languageNote, text: body}, true
		}
		return trivium{kind: lineNote, text: " " + body}, true
	}
	doc := isDocComment(raw)
	body := raw[2 : len(raw)-2]
	if doc {
		body = raw[3 : len(raw)-2]
	}
	rows := strings.Split(body, "\n")
	for i, row := range rows {
		rows[i] = trimComment(row)
	}
	rows[0] = strings.TrimSpace(rows[0])
	unstarred(rows, c.column(t.Span.Start))
	if rows[0] == "" {
		rows = rows[1:]
	}
	if n := len(rows); n > 0 && strings.TrimSpace(rows[n-1]) == "" {
		rows = rows[:n-1]
	}
	if strings.TrimSpace(strings.Join(rows, "")) == "" {
		return trivium{}, false
	}
	first := 0
	margin := c.column(t.Span.Start) + len("/*")
	if strings.TrimSpace(body[:strings.IndexByte(body, '\n')]) != "" {
		first = 1
		margin++
	}
	for _, row := range rows[min(first, len(rows)):] {
		if strings.TrimSpace(row) == "" {
			continue
		}
		if lead := len(row) - len(strings.TrimLeft(row, " \t")); lead < margin {
			margin = lead
		}
	}
	for i := min(first, len(rows)); i < len(rows); i++ {
		if strings.TrimSpace(rows[i]) != "" {
			rows[i] = rows[i][margin:]
		}
	}
	kind := blockNote
	if doc {
		kind = docNote
	} else if len(rows) == 1 {
		return trivium{kind: lineNote, text: " " + strings.TrimSpace(rows[0])}, true
	}
	return trivium{kind: kind, lines: rows}, true
}

func unstarred(rows []string, column int) {
	star := strings.Repeat(" ", column+1) + "*"
	body := rows[1:]
	if n := len(body); n > 0 && strings.TrimSpace(body[n-1]) == "" {
		body = body[:n-1]
	}
	if len(body) == 0 {
		return
	}
	for _, row := range body {
		if !strings.HasPrefix(row, star) {
			return
		}
	}
	for i, row := range body {
		body[i] = row[len(star):]
	}
}

func isDocComment(raw string) bool {
	return strings.HasPrefix(raw, "/**") && len(raw) > len("/**/")
}

func isLanguageWord(s string) bool {
	for _, r := range s {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '+' || r == '.') {
			return false
		}
	}
	return s != ""
}

func (c *converter) leading() []trivium {
	var out []trivium
	tokens := c.file.Tokens
	for c.at < len(tokens) {
		t := tokens[c.at]
		switch t.Kind {
		case syntax.TokWhitespace:
			if c.at > 0 && strings.Count(c.src[t.Span.Start:t.Span.End], "\n") >= 2 {
				out = append(out, trivium{kind: blankLine})
			}
		case syntax.TokLineComment:
			out = append(out, trivium{kind: lineNote, text: trimComment(c.src[t.Span.Start+1 : t.Span.End])})
		case syntax.TokBlockComment:
			if note, ok := c.blockComment(t, c.stringFollows(c.at+1)); ok {
				out = append(out, note)
			}
		default:
			return out
		}
		c.at++
	}
	return out
}

func (c *converter) stringFollows(at int) bool {
	tokens := c.file.Tokens
	if at < len(tokens) && tokens[at].Kind == syntax.TokWhitespace {
		at++
	}
	return at < len(tokens) && (tokens[at].Kind == syntax.TokQuote || tokens[at].Kind == syntax.TokIndentQuote)
}

func (c *converter) next(kinds ...syntax.TokenKind) *token {
	pre := c.leading()
	t := c.file.Tokens[c.at]
	found := false
	for _, k := range kinds {
		found = found || t.Kind == k
	}
	if !found {
		panic("format: the token stream does not match the parsed tree")
	}
	c.at++
	return &token{pre: pre, text: c.src[t.Span.Start:t.Span.End], line: c.lineAt(t.Span.Start)}
}

func raiseTrail(from, to *token) {
	if from.trail == "" {
		return
	}
	note := trivium{kind: lineNote, text: " " + from.trail}
	from.trail = ""
	if from == to {
		to.pre = append(to.pre, note)
		return
	}
	to.pre = append([]trivium{note}, to.pre...)
}

func raiseTrailAfter(from, to *token) {
	to.pre = append(to.pre, trivium{kind: lineNote, text: " " + from.trail})
	from.trail = ""
}

func (c *converter) closeTok(t *token) *token {
	c.attachTrail(t)
	return t
}

func (c *converter) attachTrail(t *token) {
	first := c.at
	t.trail = c.trailing()
	for ; first < c.at; first++ {
		tok := c.file.Tokens[first]
		if tok.Kind == syntax.TokLineComment {
			body := c.src[tok.Span.Start+1 : tok.Span.End]
			t.tight = !strings.HasPrefix(body, " ") && !strings.HasPrefix(body, "\t")
			return
		}
		if tok.Kind == syntax.TokBlockComment {
			return
		}
	}
}

func (c *converter) take(kinds ...syntax.TokenKind) *token { return c.closeTok(c.next(kinds...)) }

func (c *converter) root(file *syntax.File) (expr, *token) {
	e := c.expr(file.Root)
	return e, &token{pre: c.leading()}
}

func (c *converter) expr(e syntax.Expr) expr {
	switch e := e.(type) {
	case *syntax.Ident:
		return &plainTerm{tok: c.take(syntax.TokIdent, syntax.TokOrKeyword)}
	case *syntax.Int:
		return &plainTerm{tok: c.take(syntax.TokInt)}
	case *syntax.Float:
		return &plainTerm{tok: c.take(syntax.TokFloat)}
	case *syntax.URI:
		t := c.take(syntax.TokURI)
		t.text = `"` + t.text + `"`
		return &uriTerm{tok: t}
	case *syntax.String:
		return c.stringValue(e)
	case *syntax.Path:
		return c.path(e)
	case *syntax.Paren:
		return c.paren(e)
	case *syntax.List:
		return c.list(e)
	case *syntax.AttrSet:
		return c.attrSet(e, nil)
	case *syntax.LegacyLet:
		return c.attrSet(e.Set, c.take(syntax.TokLet))
	case *syntax.Let:
		return c.let(e)
	case *syntax.With:
		kw := c.take(syntax.TokWith)
		scope := c.expr(e.Scope)
		semi := c.take(syntax.TokSemicolon)
		return &withExpr{keyword: kw, scope: scope, semi: semi, body: c.expr(e.Body)}
	case *syntax.Assert:
		kw := c.take(syntax.TokAssert)
		raiseTrail(kw, kw)
		cond := c.expr(e.Cond)
		semi := c.take(syntax.TokSemicolon)
		return &assertExpr{keyword: kw, cond: cond, semi: semi, body: c.expr(e.Body)}
	case *syntax.If:
		return c.ifExpr(e, false)
	case *syntax.Lambda:
		p := c.param(e.Param)
		colon := c.take(syntax.TokColon)
		return &lambdaExpr{param: p, colon: colon, body: c.expr(e.Body)}
	case *syntax.Apply:
		return c.apply(e)
	case *syntax.Select:
		return c.selection(e)
	case *syntax.HasAttr:
		target := c.expr(e.Target)
		mark := c.take(syntax.TokQuestion)
		raiseTrail(mark, mark)
		return &hasAttrExpr{target: target, mark: mark, path: c.attrPath(e.Path)}
	case *syntax.Binary:
		left := c.expr(e.Left)
		op := c.take(binaryToken(e.Op))
		if !comparison(e.Op) {
			raiseTrail(op, op)
		}
		return &binaryExpr{op: e.Op, left: left, opTok: op, right: c.expr(e.Right)}
	case *syntax.Unary:
		op := c.take(syntax.TokNot, syntax.TokMinus)
		return &unaryExpr{opTok: op, operand: c.expr(e.Operand)}
	}
	panic("format: a parsed Nix file has an unknown expression")
}

func (c *converter) ifExpr(e *syntax.If, chained bool) *ifExpr {
	ifTok := c.take(syntax.TokIf)
	if !chained {
		raiseTrail(ifTok, ifTok)
	}
	cond := c.expr(e.Cond)
	thenTok := c.take(syntax.TokThen)
	then := c.expr(e.Then)
	elseTok := c.take(syntax.TokElse)
	raiseTrail(elseTok, elseTok)
	n := &ifExpr{ifTok: ifTok, cond: cond, thenTok: thenTok, then: then, elseTok: elseTok}
	if next, ok := e.Else.(*syntax.If); ok {
		n.otherwise = c.ifExpr(next, true)
		return n
	}
	n.otherwise = c.expr(e.Else)
	return n
}

func comparison(op syntax.Op) bool {
	switch op {
	case syntax.OpEq, syntax.OpNe, syntax.OpLt, syntax.OpLe, syntax.OpGt, syntax.OpGe:
		return true
	}
	return false
}

func binaryToken(op syntax.Op) syntax.TokenKind {
	switch op {
	case syntax.OpPipeInto:
		return syntax.TokPipeInto
	case syntax.OpPipeFrom:
		return syntax.TokPipeFrom
	case syntax.OpImplies:
		return syntax.TokImplies
	case syntax.OpOr:
		return syntax.TokOr
	case syntax.OpAnd:
		return syntax.TokAnd
	case syntax.OpEq:
		return syntax.TokEq
	case syntax.OpNe:
		return syntax.TokNe
	case syntax.OpLt:
		return syntax.TokLt
	case syntax.OpLe:
		return syntax.TokLe
	case syntax.OpGt:
		return syntax.TokGt
	case syntax.OpGe:
		return syntax.TokGe
	case syntax.OpUpdate:
		return syntax.TokUpdate
	case syntax.OpAdd:
		return syntax.TokPlus
	case syntax.OpSub:
		return syntax.TokMinus
	case syntax.OpMul:
		return syntax.TokStar
	case syntax.OpDiv:
		return syntax.TokSlash
	case syntax.OpConcat:
		return syntax.TokConcat
	}
	panic("format: a parsed Nix file has an unknown operator")
}

func (c *converter) apply(e *syntax.Apply) expr {
	fn := c.expr(e.Fn)
	var args []expr
	if spliced, ok := fn.(*applyExpr); ok {
		fn, args = spliced.fn, spliced.args
	}
	if head := calleeHead(fn); head != nil && head.trail != "" {
		raiseTrailAfter(head, head)
	}
	for _, a := range e.Args {
		arg := c.expr(a)
		if spliced, ok := arg.(*applyExpr); ok {
			args = append(append(args, spliced.fn), spliced.args...)
			continue
		}
		args = append(args, arg)
	}
	return &applyExpr{fn: fn, args: args}
}

func calleeHead(fn expr) *token {
	switch v := fn.(type) {
	case *plainTerm:
		return v.tok
	case *selectTerm:
		return calleeHead(v.target)
	case *listTerm:
		return v.open
	case *setTerm:
		return v.open
	case *parenTerm:
		return v.open
	}
	return nil
}

func (c *converter) mustTerm(e expr) term {
	t, ok := e.(term)
	if !ok {
		panic("format: expected a simple term in a parsed Nix file")
	}
	return t
}

func (c *converter) selection(e *syntax.Select) expr {
	target := c.mustTerm(c.expr(e.Target))
	n := &selectTerm{target: target}
	c.dotted(e.Path, &n.path)
	if e.Default != nil {
		n.or = c.take(syntax.TokOrKeyword)
		fallback := c.expr(e.Default)
		if a, ok := fallback.(*applyExpr); ok {
			n.fallback = c.mustTerm(a.fn)
			return &applyExpr{fn: n, args: a.args}
		}
		n.fallback = c.mustTerm(fallback)
	}
	return n
}

func (c *converter) dotted(path syntax.AttrPath, out *[]selector) {
	for _, a := range path {
		dot := c.take(syntax.TokDot)
		s := c.selectorOf(a)
		s.dot = dot
		*out = append(*out, s)
	}
}

func (c *converter) attrPath(path syntax.AttrPath) []selector {
	out := make([]selector, 0, len(path))
	for i, a := range path {
		var dot *token
		if i > 0 {
			dot = c.take(syntax.TokDot)
		}
		s := c.selectorOf(a)
		s.dot = dot
		out = append(out, s)
	}
	return out
}

func (c *converter) selectorOf(a syntax.Attr) selector {
	switch a := a.(type) {
	case *syntax.IdentAttr:
		return selector{name: c.take(syntax.TokIdent, syntax.TokOrKeyword)}
	case *syntax.StringAttr:
		st := c.stringValue(a.Value)
		return selector{text: st}
	case *syntax.DynamicAttr:
		return selector{inter: c.interpolationOf(a.Value, true)}
	}
	panic("format: a parsed Nix file has an unknown attribute")
}

func (c *converter) interpolationOf(value syntax.Expr, trailing bool) *interpolation {
	open := c.take(syntax.TokInterp)
	inner := c.expr(value)
	if open.trail != "" {
		first := firstToken(inner)
		first.pre = append([]trivium{{kind: lineNote, text: open.note()}}, first.pre...)
		open.trail = ""
	}
	closing := c.next(syntax.TokRBrace)
	if trailing {
		c.closeTok(closing)
	}
	return &interpolation{open: open, value: inner, close: closing}
}

func (c *converter) paren(e *syntax.Paren) *parenTerm {
	open := c.take(syntax.TokLParen)
	inner := c.expr(e.Inner)
	return &parenTerm{open: open, inner: inner, close: c.take(syntax.TokRParen)}
}

func (c *converter) carryComma(f *formalEntry) {
	end := f.name
	if f.fallback != nil {
		end = lastToken(f.fallback)
	}
	if end.trail == "" {
		return
	}
	if f.comma == nil {
		f.comma = &token{text: ","}
	}
	if f.comma.trail == "" {
		f.comma.trail, end.trail = end.trail, ""
	}
}

func (c *converter) list(e *syntax.List) expr {
	n := &listTerm{open: c.take(syntax.TokLBracket)}
	for _, item := range e.Items {
		value := c.expr(item)
		if spliced, ok := value.(*applyExpr); ok {
			n.entries = append(n.entries, listEntry{comments: liftLeading(spliced.fn), value: spliced.fn})
			for _, arg := range spliced.args {
				n.entries = append(n.entries, listEntry{comments: liftLeading(arg), value: arg})
			}
			continue
		}
		n.entries = append(n.entries, listEntry{comments: liftLeading(value), value: value})
	}
	n.close = c.take(syntax.TokRBracket)
	return n
}

func (c *converter) attrSet(e *syntax.AttrSet, keyword *token) expr {
	if e.Rec {
		keyword = c.take(syntax.TokRec)
	}
	n := &setTerm{keyword: keyword, open: c.take(syntax.TokLBrace)}
	n.entries = c.bindings(e.Bindings)
	n.close = c.take(syntax.TokRBrace)
	return n
}

func (c *converter) bindings(list []syntax.Binding) []setEntry {
	out := make([]setEntry, 0, len(list))
	for _, b := range list {
		value := c.binding(b)
		out = append(out, setEntry{comments: liftBinding(value), binding: value})
	}
	return out
}

func (c *converter) binding(b syntax.Binding) binding {
	switch b := b.(type) {
	case *syntax.Assign:
		path := c.attrPath(b.Path)
		equals := c.take(syntax.TokAssign)
		value := c.expr(b.Value)
		return &assignBinding{path: path, equals: equals, value: value, semi: c.take(syntax.TokSemicolon)}
	case *syntax.Inherit:
		n := &inheritBinding{keyword: c.take(syntax.TokInherit)}
		if b.From != nil {
			n.from = c.paren(b.From)
		}
		for _, a := range b.Attrs {
			n.names = append(n.names, c.selectorOf(a))
		}
		n.semi = c.take(syntax.TokSemicolon)
		return n
	}
	panic("format: a parsed Nix file has an unknown binding")
}

func (c *converter) let(e *syntax.Let) expr {
	n := &letExpr{keyword: c.take(syntax.TokLet)}
	n.entries = c.bindings(e.Bindings)
	n.in = c.take(syntax.TokIn)
	n.body = c.expr(e.Body)
	raiseTrail(n.in, firstToken(n.body))
	return n
}

func (c *converter) param(p syntax.Param) param {
	switch p := p.(type) {
	case *syntax.IdentParam:
		return &nameParam{name: c.take(syntax.TokIdent, syntax.TokOrKeyword)}
	case *syntax.PatternParam:
		if p.Alias != nil && p.AliasFirst {
			name := &nameParam{name: c.take(syntax.TokIdent, syntax.TokOrKeyword)}
			at := c.take(syntax.TokAt)
			return &aliasParam{first: name, at: at, second: c.formals(p)}
		}
		set := c.formals(p)
		if p.Alias == nil {
			return set
		}
		at := c.take(syntax.TokAt)
		return &aliasParam{first: set, at: at, second: &nameParam{name: c.take(syntax.TokIdent, syntax.TokOrKeyword)}}
	}
	panic("format: a parsed Nix file has an unknown parameter")
}

func (c *converter) formals(p *syntax.PatternParam) *setParam {
	n := &setParam{open: c.take(syntax.TokLBrace)}
	raiseTrail(n.open, n.open)
	for i, f := range p.Formals {
		name := c.take(syntax.TokIdent, syntax.TokOrKeyword)
		entry := formalEntry{comments: lift(name), name: name}
		if f.Default != nil {
			entry.mark = c.take(syntax.TokQuestion)
			entry.fallback = c.expr(f.Default)
		}
		if i < len(p.Formals)-1 || p.Ellipsis || c.nextIs(syntax.TokComma) {
			entry.comma = c.take(syntax.TokComma)
		}
		c.carryComma(&entry)
		n.formals = append(n.formals, entry)
	}
	if p.Ellipsis {
		name := c.take(syntax.TokEllipsis)
		n.formals = append(n.formals, formalEntry{comments: lift(name), name: name})
	}
	n.close = c.take(syntax.TokRBrace)
	return n
}

func (c *converter) nextIs(kind syntax.TokenKind) bool {
	for at := c.at; at < len(c.file.Tokens); at++ {
		k := c.file.Tokens[at].Kind
		if k == syntax.TokWhitespace || k == syntax.TokLineComment || k == syntax.TokBlockComment {
			continue
		}
		return k == kind
	}
	return false
}

func (c *converter) path(e *syntax.Path) expr {
	if e.Kind == syntax.SearchPath {
		return &plainTerm{tok: c.take(syntax.TokSearchPath)}
	}
	first := c.next(syntax.TokPath, syntax.TokHomePath)
	n := &pathTerm{tok: first}
	n.pieces = c.pieces(e.Parts[1:], []stringPiece{{text: first.text}})
	c.closeTok(first)
	return n
}

func (c *converter) pieces(parts []syntax.Part, out []stringPiece) []stringPiece {
	for _, p := range parts {
		if in, ok := p.(*syntax.Interpolation); ok {
			out = append(out, stringPiece{interp: c.interpolationOf(in.Value, false)})
			continue
		}
		t := c.file.Tokens[c.at]
		if !isStringFragment(t.Kind) {
			panic("format: the string fragments do not match the parsed tree")
		}
		c.at++
		s := c.src[p.Span().Start:p.Span().End]
		if last := len(out) - 1; last >= 0 && out[last].interp == nil {
			out[last].text += s
			continue
		}
		out = append(out, stringPiece{text: s})
	}
	return out
}

func (c *converter) stringValue(s *syntax.String) *stringTerm {
	kind := syntax.TokQuote
	if s.Kind == syntax.Indented {
		kind = syntax.TokIndentQuote
	}
	open := c.next(kind)
	pieces := c.pieces(s.Parts, nil)
	closing := c.file.Tokens[c.at]
	if closing.Kind != kind {
		panic("format: an unterminated string reached the formatter")
	}
	c.at++
	c.attachTrail(open)
	return &stringTerm{indented: s.Kind == syntax.Indented, open: open, pieces: pieces, probing: &c.probing}
}

func lift(t *token) []trivium {
	out := t.pre
	t.pre = nil
	return out
}

func firstToken(e expr) *token {
	switch e := e.(type) {
	case *plainTerm:
		return e.tok
	case *stringTerm:
		return e.open
	case *uriTerm:
		return e.tok
	case *pathTerm:
		return e.tok
	case *listTerm:
		return e.open
	case *setTerm:
		if e.keyword != nil {
			return e.keyword
		}
		return e.open
	case *selectTerm:
		return firstToken(e.target)
	case *parenTerm:
		return e.open
	case *withExpr:
		return e.keyword
	case *letExpr:
		return e.keyword
	case *assertExpr:
		return e.keyword
	case *ifExpr:
		return e.ifTok
	case *lambdaExpr:
		return firstParamToken(e.param)
	case *applyExpr:
		return firstToken(e.fn)
	case *binaryExpr:
		return firstToken(e.left)
	case *hasAttrExpr:
		return firstToken(e.target)
	case *unaryExpr:
		return e.opTok
	}
	panic("format: an expression has no first token")
}

func lastToken(e expr) *token {
	switch e := e.(type) {
	case *plainTerm:
		return e.tok
	case *stringTerm:
		return e.open
	case *uriTerm:
		return e.tok
	case *pathTerm:
		return e.tok
	case *listTerm:
		return e.close
	case *setTerm:
		return e.close
	case *selectTerm:
		if e.fallback != nil {
			return lastToken(e.fallback)
		}
		last := e.path[len(e.path)-1]
		switch {
		case last.name != nil:
			return last.name
		case last.text != nil:
			return last.text.open
		}
		return last.inter.close
	case *parenTerm:
		return e.close
	case *withExpr:
		return lastToken(e.body)
	case *letExpr:
		return lastToken(e.body)
	case *assertExpr:
		return lastToken(e.body)
	case *ifExpr:
		return lastToken(e.otherwise)
	case *lambdaExpr:
		return lastToken(e.body)
	case *applyExpr:
		return lastToken(e.args[len(e.args)-1])
	case *binaryExpr:
		return lastToken(e.right)
	case *hasAttrExpr:
		last := e.path[len(e.path)-1]
		switch {
		case last.name != nil:
			return last.name
		case last.text != nil:
			return last.text.open
		}
		return last.inter.close
	case *unaryExpr:
		return lastToken(e.operand)
	}
	panic("format: an expression has no last token")
}

func firstParamToken(p param) *token {
	switch p := p.(type) {
	case *nameParam:
		return p.name
	case *setParam:
		return p.open
	case *aliasParam:
		return firstParamToken(p.first)
	}
	panic("format: a parameter has no first token")
}

func liftLeading(e expr) []trivium { return lift(firstToken(e)) }

func liftBinding(b binding) []trivium {
	switch b := b.(type) {
	case *assignBinding:
		s := b.path[0]
		switch {
		case s.name != nil:
			return lift(s.name)
		case s.text != nil:
			return lift(s.text.open)
		}
		return lift(s.inter.open)
	case *inheritBinding:
		return lift(b.keyword)
	}
	panic("format: a binding has no first token")
}
