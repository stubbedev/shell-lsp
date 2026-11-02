package format

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

const stringBlanks = " \t\r"

type comment struct {
	token  syntax.Token
	before int
	after  int
}

type trivium struct {
	comments []comment
	newlines int
}

func (t *trivium) trail(src string) *comment {
	if len(t.comments) == 0 || t.comments[0].before != 0 {
		return nil
	}
	if s := t.comments[0].token.Span; strings.Contains(src[s.Start:s.End], "\n") {
		return nil
	}
	return &t.comments[0]
}

func (t *trivium) pre(src string) []comment {
	if t.trail(src) != nil {
		return t.comments[1:]
	}
	return t.comments
}

func (t *trivium) broken() bool { return t.newlines > 0 || len(t.comments) > 0 }

func (t *trivium) gapAfter() int {
	if len(t.comments) > 0 {
		return t.comments[len(t.comments)-1].after
	}
	return t.newlines
}

type builder struct {
	src  string
	used map[int]bool
}

func (b *builder) tx(n *node) string { return b.src[n.span.Start:n.span.End] }

func (b *builder) multi(n *node) bool { return strings.Contains(b.tx(n), "\n") }

func (b *builder) tokenText(t syntax.Token) string { return b.src[t.Span.Start:t.Span.End] }

func (b *builder) significant(n *node) []element {
	out := make([]element, 0, len(n.elements))
	for _, e := range n.elements {
		if e.child != nil || !isTrivia(e.token.Kind) {
			out = append(out, e)
		}
	}
	return out
}

func (b *builder) trivia(n *node) []trivium {
	out := make([]trivium, 0, len(n.elements)+1)
	cur := trivium{}
	pending := 0
	flush := func() {
		if count := len(cur.comments); count > 0 {
			cur.comments[count-1].after = pending
		}
		out = append(out, cur)
		cur = trivium{}
		pending = 0
	}
	for _, e := range n.elements {
		switch {
		case e.child != nil || !isTrivia(e.token.Kind):
			flush()
		case e.token.Kind == syntax.TokWhitespace:
			count := strings.Count(b.tokenText(e.token), "\n")
			pending += count
			cur.newlines += count
		default:
			if count := len(cur.comments); count > 0 {
				cur.comments[count-1].after = pending
			}
			cur.comments = append(cur.comments, comment{token: e.token, before: pending})
			pending = 0
		}
	}
	flush()
	return out
}

func (b *builder) column(t syntax.Token) int {
	before := b.src[:t.Span.Start]
	return utf8.RuneCountInString(before[strings.LastIndexByte(before, '\n')+1:])
}

func (b *builder) commentDoc(t syntax.Token, nest int, kind textKind) []item {
	b.used[t.Span.Start] = true
	raw := b.tokenText(t)
	if t.Kind == syntax.TokLineComment {
		return textOf(nest, strings.TrimRightFunc(raw, unicode.IsSpace), kind)
	}
	body := raw[2 : len(raw)-2]
	if !strings.Contains(body, "\n") {
		if strings.HasPrefix(raw, "/**") {
			return textOf(nest, raw, kind)
		}
		return textOf(nest, strings.TrimRightFunc("# "+strings.TrimSpace(body), unicode.IsSpace), kind)
	}
	rows := strings.Split(body, "\n")
	for i, row := range rows {
		rows[i] = strings.TrimRightFunc(row, unicode.IsSpace)
	}
	if strings.TrimSpace(rows[0]) == "" {
		rows = rows[1:]
	} else {
		rows[0] = strings.Repeat(" ", b.column(t)+len("/*")) + rows[0]
	}
	if len(rows) > 0 && strings.TrimSpace(rows[len(rows)-1]) == "" {
		rows = rows[:len(rows)-1]
	}
	margin := -1
	for _, row := range rows {
		if strings.TrimSpace(row) == "" {
			continue
		}
		if lead := len(row) - len(strings.TrimLeft(row, " \t")); margin < 0 || lead < margin {
			margin = lead
		}
	}
	out := textOf(nest, "/*", kind)
	for _, row := range rows {
		out = append(out, hardLine...)
		if strings.TrimSpace(row) != "" {
			line := textOf(nest, row[margin:], kind)
			line[0].offset = indentWidth
			out = append(out, line...)
		}
	}
	return seq(out, hardLine, textOf(nest, "*/", kind))
}

func (b *builder) comment(c comment, nest int) []item { return b.commentDoc(c.token, nest, textComment) }

func (b *builder) trailing(c *comment, nest int) []item {
	return seq(hardSpace, b.commentDoc(c.token, nest, textTrailingComment))
}

func lineGap(count int) []item {
	if count >= 2 {
		return emptyLine
	}
	return hardLine
}

func (b *builder) root(n *node) []item {
	tvs := b.trivia(n)
	var out []item
	if len(tvs[0].comments) > 0 {
		out = seq(out, hardLine)
		for _, c := range tvs[0].comments {
			out = seq(out, b.comment(c, 0), lineGap(c.after))
		}
	}
	var extra []item
	if tr := tvs[1].trail(b.src); tr != nil {
		extra = seq(extra, b.trailing(tr, 0))
	}
	for _, c := range tvs[1].pre(b.src) {
		extra = seq(extra, lineGap(c.before), b.comment(c, 0))
	}
	body := appended(group(b.expr(n.kids[0], 0)), extra)
	out = seq(out, body)
	if len(extra) > 0 {
		out = seq(out, hardLine)
	}
	return out
}

func (b *builder) hasComments(n *node) bool {
	for _, e := range n.elements {
		if e.child == nil && (e.token.Kind == syntax.TokLineComment || e.token.Kind == syntax.TokBlockComment) {
			return true
		}
	}
	return false
}

func (b *builder) simple(n *node) bool {
	switch n.kind {
	case kindIdent, kindNumber, kindURI:
		return true
	case kindPath:
		return len(n.parts) == 1 && n.parts[0].interp == nil
	case kindString:
		return len(n.kids) == 0 && !b.multi(n)
	case kindSelect:
		if n.fallback != nil || !b.simple(n.kids[0]) {
			return false
		}
		for _, a := range n.attrs {
			if a.kind != kindAttrName && !b.simple(a) {
				return false
			}
		}
		return true
	case kindParen:
		return b.simple(n.kids[0])
	case kindApply:
		if len(n.kids) > 3 || b.hasComments(n) {
			return false
		}
		for _, k := range n.kids {
			if !b.simple(k) {
				return false
			}
		}
		return true
	}
	return false
}

func (b *builder) indentedMulti(n *node) bool { return n.kind == kindString && n.indented && b.multi(n) }

func (b *builder) simpleArgs(n *node) bool {
	if len(n.kids) > 3 || b.hasComments(n) || !b.simple(n.kids[0]) {
		return false
	}
	args := n.kids[1:]
	for _, a := range args[:len(args)-1] {
		if !b.simple(a) {
			return false
		}
	}
	last := args[len(args)-1]
	return b.simple(last) || (last.kind == kindParen && b.simple(last.kids[0])) || b.indentedMulti(last)
}

func (b *builder) expr(n *node, nest int) []item {
	switch n.kind {
	case kindIdent, kindNumber, kindAttrName:
		return text(nest, b.tx(n))
	case kindURI:
		return text(nest, `"`+b.tx(n)+`"`)
	case kindPath:
		return b.pathParts(n, nest)
	case kindString:
		if n.indented {
			return b.indented(n, nest)
		}
		return b.quotedParts(n, nest)
	case kindInterpolation:
		return b.interp(n, nest)
	case kindParen:
		return b.paren(n, nest)
	case kindList:
		return b.block(n, nest, "[", "]", n.kids, false)
	case kindAttrSet:
		return b.attrSet(n, nest, false)
	case kindLegacyLet:
		return b.legacyLet(n, nest, false)
	case kindLet:
		return b.let(n, nest)
	case kindWith:
		return b.with(n, nest)
	case kindAssert:
		return b.assertion(n, nest)
	case kindIf:
		return b.ifExpr(n, nest, false)
	case kindLambda:
		if n.kids[0].kind == kindIdent {
			return seq(plainBrk, b.lambda(n, nest, 1))
		}
		return b.lambda(n, nest, 1)
	case kindApply:
		return b.apply(n, nest)
	case kindSelect:
		return b.selection(n, nest)
	case kindHasAttr:
		return b.hasAttr(n, nest)
	case kindBinary:
		return b.binary(n, nest)
	case kindUnary:
		return b.unary(n, nest)
	case kindAssign:
		return b.assign(n, nest)
	case kindInherit:
		return b.inherit(n, nest)
	}
	panic("format: no layout for a parsed Nix node")
}

func (b *builder) pathParts(n *node, nest int) []item {
	var out []item
	for _, p := range n.parts {
		if p.interp != nil {
			out = seq(out, b.interp(p.interp, nest))
			continue
		}
		out = seq(out, text(nest, p.text))
	}
	return out
}

func (b *builder) quotedParts(n *node, nest int) []item {
	out := text(nest, `"`)
	for _, p := range n.parts {
		if p.interp != nil {
			out = seq(out, b.interp(p.interp, nest))
			continue
		}
		out = seq(out, text(nest, p.text))
	}
	return seq(out, text(nest, `"`))
}

func (b *builder) plainInterp(in *node) bool {
	tvs := b.trivia(in)
	if tvs[1].broken() || tvs[2].broken() {
		return false
	}
	v := in.kids[0]
	return v.kind == kindIdent || v.kind == kindNumber || v.kind == kindURI || v.kind == kindPath || (v.kind == kindSelect && b.simple(v))
}

func (b *builder) interp(in *node, nest int) []item {
	tvs := b.trivia(in)
	v := in.kids[0]
	broken := tvs[1].broken() || tvs[2].broken()
	if b.plainInterp(in) || (!broken && ((v.kind == kindString && b.simple(v)) || (v.kind == kindApply && b.simple(v)))) {
		return seq(text(nest, "${"), b.expr(v, nest), text(nest, "}"))
	}
	if !broken && (v.kind == kindAttrSet || v.kind == kindList) {
		return group(text(nest, "${"), b.expr(v, nest), text(nest, "}"))
	}
	return group(b.enclosed(in, nest, "${", "}"))
}

func (b *builder) enclosedComments(n *node, nest int, open, closing string, tvs []trivium) []item {
	inner := n.kids[0]
	out := seq(text(nest, open), hardLine)
	for _, c := range tvs[1].comments {
		out = seq(out, b.comment(c, nest+1), lineGap(c.after))
	}
	body := group(b.expr(inner, nest+1))
	if tr := tvs[2].trail(b.src); tr != nil {
		body = appended(body, b.trailing(tr, nest+1))
	}
	out = seq(out, body)
	for _, c := range tvs[2].pre(b.src) {
		out = seq(out, hardLine, b.comment(c, nest+1))
	}
	sep := plainBrk
	if len(tvs[2].comments) > 0 {
		sep = hardLine
	}
	return seq(out, sep, text(nest, closing))
}

func (b *builder) enclosed(n *node, nest int, open, closing string) []item {
	tvs := b.trivia(n)
	if len(tvs[1].comments) > 0 || len(tvs[2].comments) > 0 {
		if len(tvs[1].comments) == 0 {
			return seq(text(nest, open), plainBrk, b.enclosedTail(n, nest, closing, tvs))
		}
		return b.enclosedComments(n, nest, open, closing, tvs)
	}
	if tvs[1].newlines == 0 && tvs[2].newlines == 0 {
		if hug := b.hugged(n.kids[0], nest, open, closing); hug != nil {
			return hug
		}
	}
	return seq(text(nest, open), plainBrk, group(b.expr(n.kids[0], nest+1)), plainBrk, text(nest, closing))
}

func (b *builder) enclosedTail(n *node, nest int, closing string, tvs []trivium) []item {
	body := group(b.expr(n.kids[0], nest+1))
	if tr := tvs[2].trail(b.src); tr != nil {
		body = appended(body, b.trailing(tr, nest+1))
	}
	for _, c := range tvs[2].pre(b.src) {
		body = seq(body, hardLine, b.comment(c, nest+1))
	}
	return seq(body, hardLine, text(nest, closing))
}

func (b *builder) hugged(i *node, nest int, open, closing string) []item {
	if !b.multi(i) {
		return nil
	}
	switch {
	case i.kind == kindApply && b.simpleArgs(i):
		return seq(text(nest, open), group(b.expr(i, nest+1)), text(nest, closing))
	case i.kind == kindApply:
		doc := transparent(group(plainBrk, b.expr(i.kids[0], nest+1)))
		args := i.kids[1:]
		for _, a := range args[:len(args)-1] {
			doc = transparent(doc, space, priority(b.arg(a, nest+2)))
		}
		return seq(text(nest, open), group(doc, space, b.lastArg(args[len(args)-1], nest+2, false), plainBrk), text(nest, closing))
	case i.kind == kindLambda && b.lambdaEndsInBlock(i):
		return seq(text(nest, open), group(plainBrk, b.lambda(i, nest+1, 1)), text(nest, closing))
	case i.kind == kindWith && (i.kids[1].kind == kindAttrSet || i.kids[1].kind == kindList):
		return seq(text(nest, open), group(plainBrk, text(nest+1, "with"), hardSpace, group(b.expr(i.kids[0], nest+2)), text(nest+1, ";"), hardSpace, priority(b.forced(i.kids[1], nest+1))), text(nest, closing))
	}
	return nil
}

func (b *builder) indented(n *node, nest int) []item {
	type piece struct {
		text   string
		interp *node
	}
	rows := [][]piece{nil}
	for _, p := range n.parts {
		if p.interp != nil {
			rows[len(rows)-1] = append(rows[len(rows)-1], piece{interp: p.interp})
			continue
		}
		for i, line := range strings.Split(p.text, "\n") {
			if i > 0 {
				rows = append(rows, nil)
			}
			rows[len(rows)-1] = append(rows[len(rows)-1], piece{text: line})
		}
	}
	blank := func(row []piece) bool {
		for _, p := range row {
			if p.interp != nil || strings.Trim(p.text, stringBlanks) != "" {
				return false
			}
		}
		return true
	}
	lead := func(row []piece) int {
		if len(row) == 0 || row[0].interp != nil {
			return 0
		}
		return len(row[0].text) - len(strings.TrimLeft(row[0].text, " \t"))
	}
	if len(rows) == 1 {
		row := rows[0]
		if blank(row) {
			return text(nest, `""`)
		}
		if row[0].interp == nil {
			row[0].text = strings.TrimLeft(row[0].text, " \t")
		}
		plain := true
		for _, p := range row {
			if p.interp == nil && strings.ContainsAny(p.text, `"\`) {
				plain = false
			}
		}
		delim := `"`
		if !plain {
			delim = "''"
		}
		out := text(nest, delim)
		for _, p := range row {
			switch {
			case p.interp != nil:
				out = seq(out, b.interp(p.interp, nest))
			case plain:
				out = seq(out, text(nest, quotedText(p.text)))
			default:
				out = seq(out, text(nest, p.text))
			}
		}
		return seq(out, text(nest, delim))
	}
	margin := -1
	for _, row := range rows {
		if blank(row) {
			continue
		}
		if l := lead(row); margin < 0 || l < margin {
			margin = l
		}
	}
	margin = max(margin, 0)
	if blank(rows[0]) {
		rows = rows[1:]
	}
	out := seq(text(nest, "''"), plainBrk)
	pending := 0
	for i, row := range rows {
		last := i == len(rows)-1
		if i > 0 {
			pending++
		}
		if blank(row) {
			continue
		}
		if pending > 0 {
			out = seq(out, lines(pending))
			pending = 0
		}
		shift := 0
		for j, p := range row {
			if p.interp != nil {
				doc := b.stringInterp(p.interp, nest+1)
				if shift > 0 && j > 0 {
					doc = shifted(doc, shift)
				}
				out = seq(out, doc)
				continue
			}
			s := p.text
			if j == 0 {
				s = s[min(margin, len(s)):]
				shift = len(s) - len(strings.TrimLeft(s, " \t"))
			}
			if !last && j == len(row)-1 {
				s = strings.TrimRight(s, stringBlanks)
			}
			doc := text(nest+1, s)
			if shift > 0 && j > 0 {
				doc = shifted(doc, shift)
			}
			out = seq(out, doc)
		}
	}
	if blank(rows[len(rows)-1]) {
		return seq(out, lines(max(pending, 1)), text(nest, "''"))
	}
	return seq(out, text(nest+1, "''"))
}

func (b *builder) stringInterp(in *node, nest int) []item {
	if b.plainInterp(in) {
		return seq(text(nest, "${"), b.expr(in.kids[0], nest), text(nest, "}"))
	}
	return group(b.enclosed(in, nest, "${", "}"))
}

func quotedText(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(strings.NewReplacer("''$", `\$`, "'''", "''").Replace(s))
}

func (b *builder) paren(n *node, nest int) []item {
	tvs := b.trivia(n)
	i := n.kids[0]
	if len(tvs[1].comments) > 0 || len(tvs[2].comments) > 0 {
		return b.enclosed(n, nest, "(", ")")
	}
	if hug := b.hugged(i, nest, "(", ")"); hug != nil {
		return hug
	}
	if hug := b.huggedOnce(i, nest); hug != nil {
		return hug
	}
	switch {
	case i.kind == kindAttrSet || i.kind == kindList || (i.kind == kindApply && b.simple(i)):
		return seq(text(nest, "("), group(b.expr(i, nest+1)), text(nest, ")"))
	case i.kind == kindSelect && b.simple(i):
		return seq(text(nest, "("), group(b.expr(i, nest+1)), plainBrk, text(nest, ")"))
	}
	return b.parenOpen(n, nest)
}

func (b *builder) parenOpen(n *node, nest int) []item {
	return seq(text(nest, "("), plainBrk, group(b.expr(n.kids[0], nest+1)), plainBrk, text(nest, ")"))
}

func (b *builder) huggedOnce(i *node, nest int) []item {
	switch {
	case i.kind == kindLambda && i.kids[0].kind == kindIdent && isBlock(i.kids[1]):
		return seq(text(nest, "("), text(nest, b.tx(i.kids[0])+":"), hardSpace, b.forced(i.kids[1], nest), text(nest, ")"))
	case i.kind == kindLambda && b.lambdaEndsInBlock(i):
		return seq(text(nest, "("), b.lambda(i, nest, 1), text(nest, ")"))
	case i.kind == kindApply && len(i.kids) == 2 && b.simple(i.kids[0]) && isBlock(i.kids[1]) && !b.hasComments(i):
		return seq(text(nest, "("), b.simpleText(i.kids[0], nest), hardSpace, b.forced(i.kids[1], nest), text(nest, ")"))
	}
	return nil
}

func isBlock(n *node) bool { return n.kind == kindAttrSet || n.kind == kindList }

func (b *builder) block(n *node, nest int, open, closing string, items []*node, force bool) []item {
	els := b.significant(n)
	at := 0
	for i, e := range els {
		if e.child == nil && b.tokenText(e.token) == open {
			at = i
			break
		}
	}
	tvs := b.trivia(n)[at+1 : at+2+len(items)]
	if len(items) == 0 && len(tvs[0].comments) == 0 {
		sep := hardSpace
		if tvs[0].newlines > 0 {
			sep = hardLine
		}
		return seq(text(nest, open), sep, text(nest, closing))
	}
	hardEdge := force || b.multi(n)
	edge := func(count int) []item {
		switch {
		case count >= 2:
			return emptyLine
		case count == 1 || hardEdge:
			return hardLine
		}
		return space
	}
	inner := func(count int) []item { return lineGap(count) }
	out := text(nest, open)
	for i := range tvs {
		tv := &tvs[i]
		first, closer := i == 0, i == len(items)
		trail := tv.trail(b.src)
		pre := tv.pre(b.src)
		if trail != nil {
			if first {
				sep := edge(0)
				if trail.token.Kind == syntax.TokLineComment || tv.newlines > 0 {
					sep = hardLine
				}
				out = seq(out, sep, b.commentDoc(trail.token, nest+1, textTrailingComment))
			} else {
				out = appended(out, b.trailing(trail, nest+1))
			}
		}
		var next []item
		switch {
		case len(pre) > 0:
			out = seq(out, inner(pre[0].before))
			for j, c := range pre {
				out = seq(out, b.comment(c, nest+1))
				if j < len(pre)-1 {
					out = seq(out, lineGap(c.after))
				}
			}
			next = lineGap(pre[len(pre)-1].after)
		case trail != nil:
			next = lineGap(trail.after)
		case first || closer:
			next = edge(tv.newlines)
		default:
			next = inner(tv.newlines)
		}
		out = seq(out, next)
		if !closer {
			out = seq(out, group(b.expr(items[i], nest+1)))
		}
	}
	return seq(out, text(nest, closing))
}

func (b *builder) attrSet(n *node, nest int, force bool) []item {
	var out []item
	if n.rec {
		out = text(nest, "rec")
		tv := b.trivia(n)[1]
		if tr := tv.trail(b.src); tr != nil {
			out = seq(out, b.trailing(tr, nest), hardLine)
		} else {
			out = seq(out, hardSpace)
		}
		for _, c := range tv.pre(b.src) {
			out = seq(out, b.comment(c, nest), hardLine)
		}
	}
	return seq(out, b.block(n, nest, "{", "}", n.kids, force))
}

func (b *builder) legacyLet(n *node, nest int, force bool) []item {
	return seq(text(nest, "let"), hardSpace, b.attrSet(n.kids[0], nest, force))
}

func (b *builder) forced(n *node, nest int) []item {
	if n.kind == kindAttrSet {
		return b.attrSet(n, nest, true)
	}
	return b.expr(n, nest)
}

func (b *builder) attrPath(p *node, nest int) []item {
	var out []item
	for _, e := range b.significant(p) {
		switch {
		case e.child == nil:
			out = seq(out, text(nest, b.tokenText(e.token)))
		case e.child.kind == kindAttrName:
			out = seq(out, text(nest, b.tx(e.child)))
		case e.child.kind == kindString && b.simple(e.child):
			out = seq(out, group(text(nest, b.tx(e.child))))
		default:
			out = seq(out, b.expr(e.child, nest))
		}
	}
	return out
}

type valueShape uint8

const (
	shapeBreak valueShape = iota
	shapeHug
	shapeHugBinary
	shapeHugPriority
	shapeSpace
)

func (b *builder) value(v *node, m int) (valueShape, []item) {
	switch v.kind {
	case kindIdent, kindNumber:
		return shapeBreak, b.expr(v, m)
	case kindPath:
		if strings.HasPrefix(b.tx(v), "<") {
			return shapeBreak, b.expr(v, m)
		}
		return shapeHug, b.expr(v, m)
	case kindURI, kindString:
		return shapeHug, b.expr(v, m)
	case kindAttrSet:
		return b.setValue(v, v, m, func(force bool) []item { return b.attrSet(v, m, force) })
	case kindLegacyLet:
		return b.setValue(v, v.kids[0], m, func(force bool) []item { return b.legacyLet(v, m, force) })
	case kindList:
		if len(v.kids) == 0 && !b.trivia(v)[1].broken() {
			return shapeBreak, group(b.expr(v, m))
		}
		return shapeHug, b.expr(v, m)
	case kindParen:
		if isBlock(v.kids[0]) {
			return shapeHug, b.expr(v, m)
		}
		return shapeHugPriority, priority(b.paren(v, m+1))
	case kindApply:
		if b.simpleArgs(v) {
			return shapeHug, b.expr(v, m)
		}
		return shapeBreak, b.expr(v, m)
	case kindLambda:
		return b.lambdaValue(v, m)
	case kindWith:
		if isBlock(v.kids[1]) {
			return shapeBreak, seq(text(m, "with"), hardSpace, group(b.expr(v.kids[0], m+1)), text(m, ";"), hardSpace, priority(b.forced(v.kids[1], m)))
		}
		return shapeBreak, b.with(v, m)
	case kindBinary:
		return b.binaryValue(v, m)
	case kindSelect:
		return shapeBreak, b.expr(v, m)
	}
	return shapeSpace, b.expr(v, m)
}

func (b *builder) setValue(v, set *node, m int, render func(force bool) []item) (valueShape, []item) {
	if v.kind == kindAttrSet && len(set.kids) == 0 && !set.rec && !b.trivia(set)[1].broken() {
		return shapeBreak, b.expr(v, m)
	}
	force := false
	for _, k := range set.kids {
		force = force || k.kind == kindAssign
	}
	return shapeHug, render(force)
}

func (b *builder) lambdaValue(v *node, m int) (valueShape, []item) {
	param, body := v.kids[0], v.kids[1]
	tv := b.trivia(v)[2]
	if param.kind == kindIdent && (isBlock(body) || (body.kind == kindLambda && b.lambdaEndsInBlock(v))) {
		return shapeBreak, b.lambda(v, m, 1)
	}
	if param.kind == kindPattern && isBlock(body) && tv.newlines == 0 && len(tv.comments) == 0 && !b.multi(param) && !hasDefault(param) {
		return shapeHug, b.lambda(v, m, 1)
	}
	return shapeSpace, b.expr(v, m)
}

func hasDefault(pattern *node) bool {
	for _, f := range pattern.kids {
		if f.fallback != nil {
			return true
		}
	}
	return false
}

func (b *builder) binaryValue(v *node, m int) (valueShape, []item) {
	if v.op == "//" || v.op == "++" || v.op == "+" {
		ops, _ := flattened(v)
		first := ops[0]
		if isBlock(first) || b.indentedMulti(first) {
			return shapeHugBinary, b.binaryHugged(v, m-1)
		}
		if v.kids[0].kind != kindBinary && (isBlock(v.kids[1]) || b.indentedMulti(v.kids[1])) {
			right := priority(group(b.expr(v.kids[1], m)))
			if isBlock(v.kids[1]) {
				right = priority(b.forced(v.kids[1], m))
			}
			return shapeBreak, seq(b.operand(v.kids[0], m), space, transparent(text(m, v.op), hardSpace, right))
		}
	}
	return shapeSpace, b.expr(v, m)
}

func (b *builder) assign(n *node, nest int) []item {
	path, val := n.kids[0], n.kids[1]
	tvs := b.trivia(n)
	ta, tb, tc := &tvs[1], &tvs[2], &tvs[3]
	plainPath := true
	for _, a := range path.attrs {
		plainPath = plainPath && a.kind == kindAttrName
	}
	m := nest + 2
	head := b.attrPath(path, nest)
	if len(ta.comments) > 0 {
		if tr := ta.trail(b.src); tr != nil {
			head = seq(head, b.trailing(tr, nest))
		}
		for _, c := range ta.pre(b.src) {
			head = seq(head, hardLine, b.comment(c, nest+1))
		}
		head = seq(head, hardLine, text(nest+1, "="))
	} else {
		head = seq(head, hardSpace, text(nest+1, "="))
	}
	var part []item
	switch {
	case len(tb.comments) == 1 && tb.comments[0].token.Kind == syntax.TokBlockComment && tb.newlines == 0 && val.kind == kindString && !strings.Contains(b.tokenText(tb.comments[0].token), "\n"):
		b.used[tb.comments[0].token.Span.Start] = true
		part = seq(hardSpace, textOf(m, b.tokenText(tb.comments[0].token), textComment), hardSpace, group(b.expr(val, m)))
	case tb.newlines >= 2 && len(tb.comments) == 0:
		shape, doc := b.value(val, m)
		if shape == shapeHugBinary {
			doc = b.expr(val, m)
		}
		part = seq(emptyLine, group(doc))
	case len(tb.comments) > 0:
		tr := tb.trail(b.src)
		if tr != nil {
			head = seq(head, b.trailing(tr, nest+1))
		}
		part = hardLine
		for _, c := range tb.pre(b.src) {
			part = seq(part, b.comment(c, m), hardLine)
		}
		if tr == nil {
			part = seq(part, group(b.expr(val, m)))
			break
		}
		shape, doc := b.value(val, m)
		if shape == shapeBreak {
			part = seq(part, group(space, doc))
		} else {
			part = seq(part, group(doc))
		}
	default:
		shape, doc := b.value(val, m)
		part = placed(shape, doc, plainPath)
	}
	var semi []item
	if len(tc.comments) > 0 {
		if tr := tc.trail(b.src); tr != nil {
			part = appended(part, b.trailing(tr, m))
		}
		for _, c := range tc.pre(b.src) {
			semi = seq(semi, hardLine, b.comment(c, nest+1))
		}
		semi = seq(semi, hardLine)
	}
	return seq(head, part, semi, text(nest, ";"))
}

func placed(shape valueShape, doc []item, plainPath bool) []item {
	switch shape {
	case shapeHug, shapeHugBinary:
		if plainPath {
			return seq(hardSpace, group(doc))
		}
		return seq(space, priority(group(doc)))
	case shapeHugPriority:
		if plainPath {
			return seq(hardSpace, doc)
		}
		return seq(space, doc)
	case shapeBreak:
		if plainPath {
			return group(space, doc)
		}
		return seq(plainBrk, priority(group(space, doc)))
	case shapeSpace:
	}
	if plainPath {
		return seq(space, group(doc))
	}
	return seq(plainBrk, priority(space, group(doc)))
}

func (b *builder) lambdaEndsInBlock(v *node) bool {
	for v.kind == kindLambda && v.kids[0].kind == kindIdent {
		v = v.kids[1]
	}
	return isBlock(v) || b.indentedMulti(v)
}

func (b *builder) apply(n *node, nest int) []item {
	fn, args := n.kids[0], n.kids[1:]
	if b.simpleArgs(n) {
		out := b.simpleText(fn, nest)
		for _, a := range args[:len(args)-1] {
			out = seq(out, hardSpace, b.simpleText(a, nest+1))
		}
		last := args[len(args)-1]
		switch {
		case last.kind == kindParen:
			return seq(out, hardSpace, priority(b.parenOpen(last, nest+1)))
		case b.indentedMulti(last):
			return seq(out, hardSpace, priority(group(b.expr(last, nest+1))))
		}
		return seq(out, hardSpace, group(b.expr(last, nest+1)))
	}
	tvs := b.trivia(n)
	doc := transparent(b.firstArg(fn, nest))
	for i, a := range args[:len(args)-1] {
		doc = transparent(doc, b.argGap(&tvs[i+1], nest+1), priority(b.arg(a, nest+1)))
		doc = b.argTrail(doc, &tvs[i+2], nest+1)
	}
	last := len(args) - 1
	return seq(doc, b.argGap(&tvs[last+1], nest+1), b.lastArg(args[last], nest+1, len(args) == 1))
}

func (b *builder) argGap(tv *trivium, nest int) []item {
	cs := tv.pre(b.src)
	if tv.trail(b.src) != nil {
		if len(cs) == 0 {
			return hardLine
		}
	}
	if len(cs) == 0 {
		return space
	}
	out := hardLine
	for _, c := range cs {
		out = seq(out, b.comment(c, nest), lineGap(c.after))
	}
	return out
}

func (b *builder) argTrail(doc []item, tv *trivium, nest int) []item {
	tr := tv.trail(b.src)
	if tr == nil {
		return doc
	}
	g := doc[0]
	g.items = appended(g.items, b.trailing(tr, nest))
	return []item{measured(g)}
}

func (b *builder) simpleText(n *node, nest int) []item {
	switch n.kind {
	case kindParen:
		return seq(text(nest, "("), b.simpleInner(n.kids[0], nest+1), text(nest, ")"))
	case kindApply:
		return b.simpleInner(n, nest)
	}
	return b.expr(n, nest)
}

func (b *builder) simpleInner(n *node, nest int) []item {
	if n.kind != kindApply {
		return b.simpleText(n, nest)
	}
	out := b.simpleText(n.kids[0], nest)
	for _, a := range n.kids[1:] {
		out = seq(out, hardSpace, b.simpleText(a, nest+1))
	}
	return out
}

func (b *builder) firstArg(n *node, nest int) []item {
	if n.kind == kindList || n.kind == kindParen {
		return group(b.expr(n, nest))
	}
	return b.expr(n, nest)
}

func (b *builder) arg(n *node, nest int) []item {
	switch {
	case n.kind == kindAttrSet || n.kind == kindList:
		return b.expr(n, nest)
	case n.kind == kindString || n.kind == kindParen:
		return group(b.expr(n, nest))
	}
	return b.expr(n, nest)
}

func (b *builder) lastArg(n *node, nest int, only bool) []item {
	switch {
	case isBlock(n):
		return priority(b.expr(n, nest))
	case b.indentedMulti(n):
		return priority(group(b.expr(n, nest)))
	case n.kind == kindParen:
		i := n.kids[0]
		if isBlock(i) {
			return priority(group(b.expr(n, nest)))
		}
		if only {
			if hug := b.huggedOnce(i, nest); hug != nil {
				return priority(hug)
			}
		}
		return priority(b.parenOpen(n, nest))
	}
	return group(b.expr(n, nest))
}

func (b *builder) lambda(n *node, nest, depth int) []item {
	param, body := n.kids[0], n.kids[1]
	tvs := b.trivia(n)
	var head []item
	if param.kind == kindIdent {
		head = text(nest, b.tx(param)+":")
	} else {
		head = seq(b.pattern(param, nest), text(nest, ":"))
	}
	tb := &tvs[2]
	if len(tb.comments) > 0 || tb.newlines >= 2 || (param.kind == kindPattern && (tb.newlines >= 1 || b.multi(param) || hasDefault(param))) {
		tr := tb.trail(b.src)
		count := tb.newlines
		if tr != nil {
			head = seq(head, b.trailing(tr, nest))
			count = tr.after
		}
		pre := tb.pre(b.src)
		if len(pre) > 0 {
			count = pre[0].before
		}
		head = seq(head, lineGap(count))
		for _, c := range pre {
			head = seq(head, b.comment(c, nest), lineGap(c.after))
		}
		switch {
		case body.kind == kindIdent || body.kind == kindNumber || body.kind == kindURI || body.kind == kindAttrSet:
			return seq(head, b.expr(body, nest))
		case body.kind == kindLambda && param.kind == kindIdent && body.kids[0].kind == kindIdent:
			return seq(head, b.lambda(body, nest, 1))
		case body.kind == kindLet:
			return seq(head, b.let(body, nest))
		case body.kind == kindWith:
			return seq(head, b.with(body, nest))
		}
		return seq(head, group(b.expr(body, nest)))
	}
	switch {
	case isBlock(body):
		return seq(head, hardSpace, priority(b.expr(body, nest)))
	case b.indentedMulti(body):
		return seq(head, hardSpace, priority(group(b.expr(body, nest))))
	case body.kind == kindLambda && param.kind == kindIdent && body.kids[0].kind == kindIdent:
		return seq(head, hardSpace, b.lambda(body, nest, depth+1))
	case body.kind == kindLambda && body.kids[0].kind == kindPattern:
		return seq(head, space, b.lambda(body, nest, 1))
	case body.kind == kindLambda:
		return seq(head, space, group(b.expr(body, nest)))
	}
	sep := space
	if depth >= 3 && param.kind == kindIdent {
		sep = hardLine
	}
	switch body.kind {
	case kindIdent, kindNumber, kindURI:
		return seq(head, sep, b.expr(body, nest))
	case kindLet:
		return seq(head, sep, b.let(body, nest))
	case kindWith:
		return seq(head, sep, b.with(body, nest))
	}
	return seq(head, sep, group(b.expr(body, nest)))
}

func (b *builder) pattern(p *node, nest int) []item {
	els := b.significant(p)
	tvs := b.trivia(p)
	start := 0
	var before, after string
	if els[0].child != nil {
		before = b.tx(els[0].child)
		start = 2
	}
	closer := start
	for els[closer].child != nil || els[closer].token.Kind != syntax.TokRBrace {
		closer++
	}
	if closer+1 < len(els) {
		after = b.tx(els[closer+2].child)
	}
	hard := hasDefault(p)
	for i := start + 1; i <= closer; i++ {
		hard = hard || tvs[i].broken()
	}
	sep := space
	if hard {
		sep = hardLine
	}
	var inner []item
	entries := els[start+1 : closer+1]
	for at, e := range entries {
		tv := &tvs[start+1+at]
		tr := tv.trail(b.src)
		pre := tv.pre(b.src)
		if tr != nil {
			if len(inner) > 0 {
				inner = seq(inner, b.trailing(tr, nest+1))
			} else {
				pre = tv.comments
			}
		}
		for _, c := range pre {
			if len(inner) > 0 {
				inner = seq(inner, lineGap(c.before))
			}
			inner = seq(inner, b.comment(c, nest+1))
		}
		if e.child == nil && e.token.Kind == syntax.TokComma {
			if tr != nil || len(pre) > 0 {
				inner = seq(inner, hardLine)
			}
			inner = seq(inner, text(nest+1, ","))
			continue
		}
		if e.child == nil && e.token.Kind == syntax.TokRBrace {
			break
		}
		if len(inner) > 0 {
			count := tv.gapAfter()
			if count >= 2 {
				inner = seq(inner, emptyLine)
			} else {
				inner = seq(inner, sep)
			}
		}
		if e.child == nil {
			inner = seq(inner, text(nest+1, "..."))
			continue
		}
		if e.child.fallback == nil {
			inner = seq(inner, text(nest+1, b.tx(e.child.name)))
		} else {
			inner = seq(inner, group(b.formal(e.child, nest+1)))
		}
		if next := entries[at+1]; next.child != nil || next.token.Kind != syntax.TokComma {
			inner = seq(inner, textOf(nest+1, ",", textTrailing))
		}
	}
	var body []item
	if len(inner) == 0 {
		body = seq(text(nest, "{"), hardSpace, text(nest, "}"))
	} else {
		body = seq(text(nest, "{"), sep, inner, sep, text(nest, "}"))
	}
	var out []item
	if before != "" {
		out = text(nest, before+"@")
	}
	out = seq(out, group(body))
	if after != "" {
		out = seq(out, text(nest, "@"+after))
	}
	return out
}

func (b *builder) formal(f *node, nest int) []item {
	tvs := b.trivia(f)
	t1, t2 := &tvs[1], &tvs[2]
	head := text(nest, b.tx(f.name))
	if len(t1.comments) > 0 {
		if tr := t1.trail(b.src); tr != nil {
			head = seq(head, b.trailing(tr, nest))
		}
		for _, c := range t1.pre(b.src) {
			head = seq(head, hardLine, b.comment(c, nest+1))
		}
		head = seq(head, hardLine, text(nest+1, "?"))
	} else {
		head = seq(head, hardSpace, text(nest+1, "?"))
	}
	v := f.fallback
	if len(t2.comments) > 0 {
		if tr := t2.trail(b.src); tr != nil {
			head = seq(head, b.trailing(tr, nest+1))
		}
		head = seq(head, hardLine)
		for _, c := range t2.pre(b.src) {
			head = seq(head, b.comment(c, nest+2), hardLine)
		}
		if v.kind == kindIdent || v.kind == kindNumber || v.kind == kindURI {
			return seq(head, group(space, b.expr(v, nest+2)))
		}
		return seq(head, group(b.expr(v, nest+2)))
	}
	shape, doc := b.value(v, nest+2)
	switch shape {
	case shapeHug, shapeHugBinary:
		return seq(head, hardSpace, group(doc))
	case shapeHugPriority:
		return seq(head, hardSpace, doc)
	case shapeBreak:
		return seq(head, group(space, doc))
	case shapeSpace:
	}
	return seq(head, space, group(doc))
}

func (b *builder) with(n *node, nest int) []item {
	tvs := b.trivia(n)
	scope := group(b.expr(n.kids[0], nest+1))
	if tr := tvs[2].trail(b.src); tr != nil {
		scope = appended(scope, b.trailing(tr, nest+1))
	}
	head := text(nest, "with")
	if tr := tvs[1].trail(b.src); tr != nil {
		head = seq(head, b.trailing(tr, nest), hardLine)
	} else {
		head = seq(head, hardSpace)
	}
	for _, c := range tvs[1].pre(b.src) {
		head = seq(head, b.comment(c, nest), hardLine)
	}
	head = seq(head, scope)
	for _, c := range tvs[2].pre(b.src) {
		head = seq(head, hardLine, b.comment(c, nest))
	}
	if len(tvs[2].comments) > 0 {
		head = seq(head, hardLine)
	}
	h := group(head, text(nest, ";"))
	t3 := &tvs[3]
	if len(t3.comments) > 0 || t3.newlines >= 2 {
		tr := t3.trail(b.src)
		count := t3.newlines
		if tr != nil {
			h = appended(h, b.trailing(tr, nest))
			count = tr.after
		}
		pre := t3.pre(b.src)
		if len(pre) > 0 {
			count = pre[0].before
		}
		out := seq(h, lineGap(count))
		for _, c := range pre {
			out = seq(out, b.comment(c, nest), lineGap(c.after))
		}
		return seq(out, b.inlineBody(n.kids[1], nest))
	}
	return seq(h, space, b.inlineBody(n.kids[1], nest))
}

func (b *builder) inlineBody(body *node, nest int) []item {
	switch body.kind {
	case kindIdent, kindNumber, kindURI, kindAttrSet:
		return b.expr(body, nest)
	case kindWith:
		return b.with(body, nest)
	case kindLet:
		return b.let(body, nest)
	}
	return group(b.expr(body, nest))
}

func (b *builder) assertion(n *node, nest int) []item {
	tvs := b.trivia(n)
	cond := group(b.expr(n.kids[0], nest+1))
	if tr := tvs[2].trail(b.src); tr != nil {
		cond = appended(cond, b.trailing(tr, nest+1))
	}
	var gap []item
	for _, c := range tvs[1].comments {
		gap = seq(gap, hardLine, b.comment(c, nest+1))
	}
	if len(gap) > 0 {
		gap = seq(gap, hardLine)
	} else {
		gap = space
	}
	out := group(transparent(text(nest, "assert")), gap, cond)
	for _, c := range tvs[2].pre(b.src) {
		out = seq(out, hardLine, b.comment(c, nest))
	}
	if len(tvs[2].comments) > 0 {
		out = seq(out, hardLine)
	}
	out = seq(out, text(nest, ";"))
	t3 := &tvs[3]
	count := t3.newlines
	if tr := t3.trail(b.src); tr != nil {
		out = seq(out, b.trailing(tr, nest))
		count = tr.after
	}
	pre := t3.pre(b.src)
	if len(pre) > 0 {
		count = pre[0].before
	}
	out = seq(out, lineGap(count))
	for _, c := range pre {
		out = seq(out, b.comment(c, nest), lineGap(c.after))
	}
	body := n.kids[1]
	if body.kind == kindIdent || body.kind == kindNumber || body.kind == kindURI || body.kind == kindAttrSet {
		return seq(out, b.expr(body, nest))
	}
	return seq(out, group(b.expr(body, nest)))
}

func (b *builder) let(n *node, nest int) []item {
	tvs := b.trivia(n)
	bindings := n.kids[:len(n.kids)-1]
	body := n.kids[len(n.kids)-1]
	first := text(nest, "let")
	gap := 0
	for i := range bindings {
		first = b.letGap(first, &tvs[i+1], i == 0, nest, &gap)
		first = seq(first, lineGap(gap), group(b.expr(bindings[i], nest+1)))
	}
	first = b.letGap(first, &tvs[len(bindings)+1], len(bindings) == 0, nest, &gap)
	tb := &tvs[len(bindings)+2]
	inner := text(nest, "in")
	count := tb.newlines
	if len(tb.comments) > 0 {
		count = tb.comments[0].before
	}
	inner = seq(inner, lineGap(count))
	for _, c := range tb.comments {
		inner = seq(inner, b.comment(c, nest), lineGap(c.after))
	}
	if body.kind == kindIdent || body.kind == kindNumber || body.kind == kindURI || body.kind == kindAttrSet {
		inner = seq(inner, b.expr(body, nest))
	} else {
		inner = seq(inner, group(b.expr(body, nest)))
	}
	return seq(group(first), lineGap(gap), group(inner))
}

func (b *builder) letGap(out []item, tv *trivium, first bool, nest int, gap *int) []item {
	tr := tv.trail(b.src)
	count := tv.newlines
	if tr != nil {
		if first {
			out = seq(out, b.trailing(tr, nest))
		} else {
			out = appended(out, b.trailing(tr, nest+1))
		}
		count = tr.after
	}
	if pre := tv.pre(b.src); len(pre) > 0 {
		out = seq(out, lineGap(pre[0].before))
		for j, c := range pre {
			out = seq(out, b.comment(c, nest+1))
			if j < len(pre)-1 {
				out = seq(out, lineGap(c.after))
			}
		}
		count = pre[len(pre)-1].after
	}
	*gap = count
	return out
}

func (b *builder) condition(a *node, nest int) []item {
	if a.kind == kindIdent || a.kind == kindNumber || a.kind == kindURI || (a.kind == kindSelect && b.simple(a)) {
		return b.expr(a, nest)
	}
	return group(b.expr(a, nest))
}

func (b *builder) ifExpr(n *node, nest int, chained bool) []item {
	tvs := b.trivia(n)
	cond, then, otherwise := n.kids[0], n.kids[1], n.kids[2]
	t1, t2, t3, t4, t5 := &tvs[1], &tvs[2], &tvs[3], &tvs[4], &tvs[5]
	head := text(nest, "if")
	gap := space
	for _, c := range t1.comments {
		head = seq(head, hardLine, b.comment(c, nest+1))
		gap = hardLine
	}
	head = seq(head, gap, b.condition(cond, nest+1))
	tr := t2.trail(b.src)
	if tr != nil {
		head = seq(head, b.trailing(tr, nest+1))
	}
	pre := t2.pre(b.src)
	for _, c := range pre {
		head = seq(head, hardLine, b.comment(c, nest+1))
	}
	if tr != nil || len(pre) > 0 {
		head = seq(head, hardLine)
	} else {
		head = seq(head, space)
	}
	out := group(head, text(nest, "then"))
	tr3 := t3.trail(b.src)
	pre3 := t3.pre(b.src)
	if tr3 != nil {
		out = appended(out, b.trailing(tr3, nest))
	}
	if tr3 != nil || len(pre3) > 0 || chained {
		out = seq(out, hardLine)
	} else {
		out = seq(out, space)
	}
	for _, c := range pre3 {
		out = seq(out, b.comment(c, nest+1), hardLine)
	}
	branch := group(b.expr(then, nest+1))
	tr4 := t4.trail(b.src)
	pre4 := t4.pre(b.src)
	if tr4 != nil {
		branch = appended(branch, b.trailing(tr4, nest+1))
	}
	out = seq(out, branch)
	for _, c := range pre4 {
		out = seq(out, hardLine, b.comment(c, nest))
	}
	if tr4 != nil || len(pre4) > 0 || chained {
		out = seq(out, hardLine)
	} else {
		out = seq(out, space)
	}
	out = seq(out, text(nest, "else"))
	tr5 := t5.trail(b.src)
	pre5 := t5.pre(b.src)
	if tr5 != nil {
		out = seq(out, b.trailing(tr5, nest))
	}
	if tr5 != nil || len(pre5) > 0 {
		out = seq(out, hardLine)
		for _, c := range pre5 {
			out = seq(out, b.comment(c, nest+1), hardLine)
		}
		return seq(out, group(b.expr(otherwise, nest+1)))
	}
	if otherwise.kind == kindIf {
		return seq(out, hardSpace, b.ifExpr(otherwise, nest, true))
	}
	return seq(out, space, group(b.expr(otherwise, nest+1)))
}

func isComparison(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	}
	return false
}

func flattened(x *node) ([]*node, []*node) {
	var ops, joins []*node
	var walk func(y *node)
	walk = func(y *node) {
		if y.kind == kindBinary && y.op == x.op {
			walk(y.kids[0])
			joins = append(joins, y)
			walk(y.kids[1])
			return
		}
		ops = append(ops, y)
	}
	walk(x)
	return ops, joins
}

func (b *builder) operatorComments(x *node, nest int) ([]item, bool) {
	tvs := b.trivia(x)
	ta, tb := &tvs[1], &tvs[2]
	var out []item
	tr := ta.trail(b.src)
	if tr != nil {
		out = seq(out, b.trailing(tr, nest))
	}
	cs := append(append([]comment(nil), ta.pre(b.src)...), tb.comments...)
	for _, c := range cs {
		out = seq(out, hardLine, b.comment(c, nest))
	}
	return out, tr != nil || len(cs) > 0
}

func (b *builder) chainTail(ops, joins []*node, op string, nest int) []item {
	var out []item
	for i, o := range ops[1:] {
		cm, has := b.operatorComments(joins[i], nest)
		sep := space
		if has {
			sep = hardLine
		}
		out = seq(out, cm, sep, text(nest, op))
		if o.kind == kindBinary {
			out = seq(out, group(space, group(b.binary(o, nest+1))))
		} else {
			out = seq(out, b.rightOperand(o, nest+1))
		}
	}
	return out
}

func (b *builder) binaryHugged(v *node, nest int) []item {
	ops, joins := flattened(v)
	first := ops[0]
	var left []item
	switch {
	case first.kind == kindAttrSet:
		left = b.forced(first, nest)
	case first.kind == kindList:
		left = b.expr(first, nest)
	default:
		left = group(b.expr(first, nest))
	}
	return seq(left, b.chainTail(ops, joins, v.op, nest))
}

func (b *builder) leftOperand(l *node, nest int) []item {
	if l.kind == kindBinary {
		return group(b.binary(l, nest))
	}
	return b.operand(l, nest)
}

func (b *builder) binary(x *node, nest int) []item {
	l, r := x.kids[0], x.kids[1]
	same := (l.kind == kindBinary && l.op == x.op) || (r.kind == kindBinary && r.op == x.op)
	if isComparison(x.op) {
		cm, _ := b.operatorComments(x, nest)
		return seq(b.operand(l, nest), cm, softSpace, text(nest, x.op), hardSpace, b.operand(r, nest))
	}
	if same {
		ops, joins := flattened(x)
		return seq(b.leftOperand(ops[0], nest), b.chainTail(ops, joins, x.op, nest))
	}
	cm, has := b.operatorComments(x, nest)
	if has {
		return seq(b.leftOperand(l, nest), cm, hardLine, text(nest, x.op), hardSpace, b.operand(r, nest+1))
	}
	left := b.leftOperand(l, nest)
	if r.kind == kindBinary {
		if isComparison(r.op) {
			return seq(left, space, text(nest, x.op), group(space, b.binary(r, nest+1)))
		}
		return seq(left, space, text(nest, x.op), group(space, group(b.binary(r, nest+1))))
	}
	return seq(left, space, text(nest, x.op), b.rightOperand(r, nest+1))
}

func (b *builder) rightOperand(y *node, nest int) []item {
	if y.kind == kindApply && !b.simpleArgs(y) {
		return group(space, b.expr(y, nest))
	}
	return seq(hardSpace, b.operand(y, nest))
}

func (b *builder) operand(y *node, nest int) []item {
	switch y.kind {
	case kindIdent, kindNumber, kindURI, kindAttrSet, kindUnary:
		return b.expr(y, nest)
	case kindSelect:
		if y.kids[0].kind != kindParen {
			return b.expr(y, nest)
		}
	}
	return group(b.expr(y, nest))
}

func (b *builder) unary(x *node, nest int) []item {
	op := b.src[x.span.Start : x.span.Start+1]
	o := x.kids[0]
	if o.kind == kindParen {
		return seq(text(nest, op), group(b.expr(o, nest)))
	}
	return seq(text(nest, op), b.expr(o, nest))
}

func (b *builder) selection(x *node, nest int) []item {
	els := b.significant(x)
	t := els[0].child
	var out []item
	switch t.kind {
	case kindIdent, kindNumber, kindURI, kindPath, kindSelect:
		out = b.expr(t, nest)
	case kindAttrSet:
		out = seq(b.expr(t, nest), plainBrk)
	case kindParen:
		out = seq(group(b.expr(t, nest)), softBreak)
	default:
		out = seq(group(b.expr(t, nest)), plainBrk)
	}
	rest := els[1:]
	for j, e := range rest {
		switch {
		case e.child == nil && e.token.Kind == syntax.TokOrKeyword:
			out = seq(out, softSpace, text(nest+1, "or"), hardSpace)
		case e.child == nil:
			out = seq(out, text(nest, b.tokenText(e.token)))
		case e.child.kind == kindAttrName:
			out = seq(out, text(nest, b.tx(e.child)))
		case e.child == x.fallback:
			if c := e.child; c.kind == kindIdent || c.kind == kindNumber || c.kind == kindURI || c.kind == kindAttrSet {
				out = seq(out, b.expr(c, nest+1))
			} else {
				out = seq(out, group(b.expr(c, nest+1)))
			}
		case e.child.kind == kindString && j > 0 && b.simple(e.child):
			out = seq(out, group(b.expr(e.child, nest)))
		default:
			out = seq(out, b.expr(e.child, nest))
		}
	}
	return out
}

func (b *builder) hasAttr(x *node, nest int) []item {
	els := b.significant(x)
	t := els[0].child
	var out []item
	switch t.kind {
	case kindIdent, kindNumber, kindURI, kindSelect, kindAttrSet:
		out = b.expr(t, nest)
	default:
		out = group(b.expr(t, nest))
	}
	out = seq(out, space, text(nest+1, "?"), hardSpace)
	for _, e := range els[2:] {
		switch {
		case e.child == nil:
			out = seq(out, text(nest+1, b.tokenText(e.token)))
		case e.child.kind == kindAttrName:
			out = seq(out, text(nest+1, b.tx(e.child)))
		default:
			out = seq(out, b.expr(e.child, nest+1))
		}
	}
	return out
}

func (b *builder) inherit(x *node, nest int) []item {
	els := b.significant(x)
	tvs := b.trivia(x)
	multi := false
	for i := 1; i < len(els); i++ {
		multi = multi || tvs[i].broken()
	}
	sep := space
	if multi {
		sep = hardLine
	}
	out := text(nest, "inherit")
	for i, e := range els[1:] {
		tv := &tvs[i+1]
		if tr := tv.trail(b.src); tr != nil {
			out = appended(out, b.trailing(tr, nest+1))
		}
		for _, c := range tv.pre(b.src) {
			out = seq(out, hardLine, b.comment(c, nest+1))
		}
		switch {
		case e.child == nil:
			if multi {
				out = seq(out, hardLine)
			} else if len(els) == 2 {
				out = seq(out, space)
			} else {
				out = seq(out, plainBrk)
			}
			out = seq(out, text(nest+1, ";"))
		case e.child == x.from:
			out = seq(out, group(space, group(b.expr(e.child, nest+1))))
		case e.child.kind == kindAttrName:
			out = seq(out, sep, text(nest+1, b.tx(e.child)))
		default:
			out = seq(out, sep, group(b.expr(e.child, nest+1)))
		}
	}
	return out
}
