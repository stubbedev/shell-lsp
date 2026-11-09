package format

import (
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

const (
	stringBlanks           = " "
	flatInterpolationWidth = 30
	plainPathLength        = 4
	simpleListLength       = 6
	interpolationOpen      = "${"
	compactFormals         = 2
	compactInherit         = 3
)

func commentText(nest int, s string) []item { return textOf(nest, s, textComment) }

func notesDoc(list []trivium, nest int) []item {
	var out []item
	for _, t := range list {
		switch t.kind {
		case blankLine:
			out = seq(out, emptyLine)
		case lineNote:
			out = seq(out, commentText(nest, "#"+t.text), hardLine)
		case languageNote:
			out = seq(out, commentText(nest, "/* "+t.text+" */"), hardSpace)
		case blockNote, docNote:
			out = seq(out, blockDoc(t, nest), hardLine)
		}
	}
	return out
}

func blockDoc(t trivium, nest int) []item {
	open := "/*"
	if t.kind == docNote {
		open = "/**"
	}
	if t.text != "" {
		return commentText(nest, t.text)
	}
	out := commentText(nest, open)
	for _, row := range t.lines {
		if strings.TrimSpace(row) == "" {
			out = seq(out, emptyLine)
			continue
		}
		line := commentText(nest, row)
		line[0].offset = indentWidth
		out = seq(out, hardLine, line)
	}
	return seq(out, hardLine, commentText(nest, "*/"))
}

func tokenPre(t *token, nest int) []item {
	if t.inlinePre() {
		return notesDoc(t.pre, nest)
	}
	return seq(hardLine, notesDoc(t.pre, nest))
}

func tokenTrail(t *token, nest int) []item {
	if t.trail == "" {
		return nil
	}
	return seq(hardSpace, textOf(nest, "# "+t.trail, textTrailingComment), hardLine)
}

func tokenDoc(t *token, nest int) []item {
	return seq(tokenPre(t, nest), text(nest, t.text), tokenTrail(t, nest))
}

func tokenBare(t *token, nest int) []item {
	return seq(tokenPre(t, nest), text(nest, t.text))
}

func rootDoc(e expr, end *token) []item {
	return group(exprDoc(e, 0), tokenPre(end, 0))
}

func isEmptySet(s *setTerm) bool {
	return len(s.entries) == 0 && s.open.trail == "" && !hasComment(s.close.pre) && len(s.open.pre) == 0
}

func isEmptyList(l *listTerm) bool {
	return len(l.entries) == 0 && l.open.trail == "" && !hasComment(l.close.pre)
}

func hasComment(list []trivium) bool {
	for _, t := range list {
		if t.kind != blankLine {
			return true
		}
	}
	return false
}

func isMultilineSet(s *setTerm) bool { return s.open.line != s.close.line }

func isMultilineList(l *listTerm) bool { return l.open.line != l.close.line }

func isIndentedMulti(e expr) bool {
	s, ok := e.(*stringTerm)
	if !ok || !s.indented {
		return false
	}
	rows := splitRows(s.pieces)
	return len(rows) > 2 || (len(rows) == 2 && !rows[0].blank())
}

func isAbsorbable(e expr) bool {
	switch e := e.(type) {
	case *setTerm:
		return !isEmptySet(e) || isMultilineSet(e)
	case *listTerm:
		return !isEmptyList(e) || isMultilineList(e) || len(e.open.pre) > 0
	}
	return isIndentedMulti(e)
}

func isLone(e expr) bool { return firstToken(e).lone() }

func isSimple(e expr) bool {
	switch e := e.(type) {
	case *plainTerm:
		return e.tok.lone()
	case *uriTerm:
		return e.tok.lone()
	case *pathTerm:
		return e.tok.lone() && len(e.pieces) == 1 && e.pieces[0].interp == nil
	case *stringTerm:
		return simpleString(e)
	case *selectTerm:
		if e.or != nil || !isSimple(e.target) {
			return false
		}
		return simpleSelectors(e.path)
	case *parenTerm:
		if e.simple == simplicityUnknown {
			e.simple = verdict(e.open.lone() && e.close.lone() && isSimple(e.inner))
		}
		return e.simple == simplicityYes
	case *applyExpr:
		if e.simple == simplicityUnknown {
			e.simple = verdict(simpleApply(e))
		}
		return e.simple == simplicityYes
	}
	return false
}

func simpleSelectors(path []selector) bool {
	for _, s := range path {
		if s.dot != nil && !s.dot.lone() {
			return false
		}
		switch {
		case s.name != nil:
			if !s.name.lone() {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func plainSelect(s *selectTerm) bool {
	if s.or != nil || !isSimple(s.target) {
		return false
	}
	for _, sel := range s.path {
		if sel.name == nil {
			return false
		}
	}
	return true
}

func simpleInterp(in *interpolation) bool {
	if !in.open.lone() || len(in.close.pre) > 0 {
		return false
	}
	if s, ok := in.value.(*selectTerm); ok && plainSelect(s) {
		return true
	}
	return isSimple(in.value)
}

func simpleString(s *stringTerm) bool {
	if s.simplicity == simplicityUnknown {
		s.simplicity = simplicityNo
		if s.open.lone() && !isIndentedMulti(s) {
			*s.probing++
			if !flatHard(stringContent(s, 0)) {
				s.simplicity = simplicityYes
			}
			*s.probing--
		}
	}
	return s.simplicity == simplicityYes
}

func flatHard(doc []item) bool {
	for i := range doc {
		switch doc[i].tag {
		case tagText:
		case tagSpacing:
			switch doc[i].space {
			case lineHard, lineEmpty, lineCount:
				return true
			case spaceHard, spaceSoft, spacePlain, breakSoft, breakPlain:
			}
		case tagGroup:
			if flatHard(doc[i].items) {
				return true
			}
		}
	}
	return false
}

func simpleApply(a *applyExpr) bool {
	if len(a.args) > 2 || !isSimple(a.fn) {
		return false
	}
	for _, x := range a.args {
		if !isSimple(x) {
			return false
		}
	}
	return true
}

func verdict(yes bool) simplicity {
	if yes {
		return simplicityYes
	}
	return simplicityNo
}

func simpleCall(a *applyExpr) bool {
	if a.call == simplicityUnknown {
		a.call = verdict(computeSimpleCall(a))
	}
	return a.call == simplicityYes
}

func computeSimpleCall(a *applyExpr) bool {
	if len(a.args) > 2 || !isSimple(a.fn) {
		return false
	}
	for _, x := range a.args[:len(a.args)-1] {
		if !isSimple(x) {
			return false
		}
	}
	last := a.args[len(a.args)-1]
	if p, ok := last.(*parenTerm); ok {
		return p.open.lone() && p.close.lone() && simpleParenInner(p.inner)
	}
	if st, ok := last.(*stringTerm); ok && !isIndentedMulti(st) && st.open.lone() {
		return true
	}
	if sel, ok := last.(*selectTerm); ok && plainSelect(sel) {
		return true
	}
	return isSimple(last) || (isIndentedMulti(last) && isLone(last))
}

func simpleParenInner(e expr) bool {
	if isSimple(e) || (isIndentedMulti(e) && firstToken(e).inlinePre()) {
		return true
	}
	a, ok := e.(*applyExpr)
	return ok && simpleCall(a)
}

func exprDoc(e expr, n int) []item {
	switch e := e.(type) {
	case *plainTerm:
		return tokenDoc(e.tok, n)
	case *uriTerm:
		return tokenDoc(e.tok, n)
	case *pathTerm:
		return pathDoc(e, n)
	case *stringTerm:
		return stringDoc(e, n)
	case *listTerm:
		return group(listDoc(e, n))
	case *setTerm:
		return setDoc(e, n, false)
	case *selectTerm:
		return selectDoc(e, n)
	case *parenTerm:
		return group(parenDoc(e, n))
	case *withExpr:
		return withDoc(e, n)
	case *letExpr:
		return letDoc(e, n)
	case *assertExpr:
		return assertDoc(e, n)
	case *ifExpr:
		return ifDoc(e, n, false)
	case *lambdaExpr:
		return lambdaDoc(e, n, 1)
	case *applyExpr:
		return applyDoc(e, n)
	case *binaryExpr:
		return binaryDoc(e, n)
	case *hasAttrExpr:
		return hasAttrDoc(e, n)
	case *unaryExpr:
		return unaryDoc(e, n)
	}
	panic("format: no layout for a parsed Nix expression")
}

func groupedTerm(e expr, n int) []item {
	switch e.(type) {
	case *listTerm, *parenTerm, *stringTerm:
		return exprDoc(e, n)
	}
	return group(exprDoc(e, n))
}

func inlineBody(e expr, n int) []item {
	if l, ok := e.(*lambdaExpr); ok {
		if _, named := l.param.(*nameParam); !named {
			return exprDoc(e, n)
		}
	}
	if b, ok := e.(*binaryExpr); ok && comparison(b.op) {
		return exprDoc(e, n)
	}
	switch e.(type) {
	case *plainTerm, *setTerm, *withExpr, *letExpr, *unaryExpr, *pathTerm:
		return exprDoc(e, n)
	}
	if _, ok := e.(*selectTerm); ok {
		return exprDoc(e, n)
	}
	return groupedTerm(e, n)
}

func interpDoc(in *interpolation, n int, lone bool) []item {
	if !lone {
		return flatDoc(in, n)
	}
	if len(in.close.pre) > 0 {
		return interpOpened(in, n)
	}
	return group(enclosed(in.open, in.value, in.close, n))
}

func pathDoc(p *pathTerm, n int) []item {
	var out []item
	for _, piece := range p.pieces {
		if piece.interp == nil {
			out = seq(out, text(n, piece.text))
			continue
		}
		out = seq(out, flatDoc(piece.interp, n))
	}
	return seq(tokenPre(p.tok, n), out, tokenTrail(p.tok, n))
}

func hasCommentsIn(e expr) bool {
	f, l := firstToken(e), lastToken(e)
	return hasComment(f.pre) || (f.trail != "" && !opensABody(e)) || l.trail != ""
}

func opensABody(e expr) bool {
	switch e.(type) {
	case *setTerm, *listTerm:
		return true
	}
	return false
}

func looseSimple(e expr) bool {
	switch v := e.(type) {
	case *plainTerm:
		return v.tok.lone()
	case *uriTerm:
		return v.tok.lone()
	case *pathTerm:
		return v.tok.lone() && len(v.pieces) == 1 && v.pieces[0].interp == nil
	case *stringTerm:
		return v.open.lone() && !isIndentedMulti(v)
	case *selectTerm:
		if v.or != nil || !looseSimple(v.target) {
			return false
		}
		for _, s := range v.path {
			if s.name == nil {
				return false
			}
		}
		return true
	case *parenTerm:
		return v.open.lone() && v.close.lone() && looseSimple(v.inner)
	case *applyExpr:
		if len(v.args) > 2 || !looseSimple(v.fn) {
			return false
		}
		for _, x := range v.args {
			if !looseSimple(x) {
				return false
			}
		}
		return true
	}
	return false
}

func flatDoc(in *interpolation, n int) []item {
	if simpleInterp(in) {
		return rigid(seq(tokenDoc(in.open, n), exprDoc(in.value, n), tokenDoc(in.close, n)))
	}
	if a, ok := in.value.(*applyExpr); ok && in.open.lone() && len(in.close.pre) == 0 && looseSimple(a) {
		return seq(tokenDoc(in.open, n), group(applyDoc(a, n)), tokenDoc(in.close, n))
	}
	if in.open.lone() && len(in.close.pre) == 0 && hugsInterpolation(in.value) {
		return group(tokenDoc(in.open, n), exprDoc(in.value, n), tokenDoc(in.close, n))
	}
	if hasCommentsIn(in.value) || !in.open.lone() || len(in.close.pre) > 0 {
		return interpOpened(in, n)
	}
	if a, ok := in.value.(*applyExpr); ok && simpleCall(a) {
		return seq(tokenDoc(in.open, n), group(applyDoc(a, n)), tokenDoc(in.close, n))
	}
	inner := exprDoc(in.value, n+1)
	if !flatFits(inner) {
		return group(interpOpen(in, n, group(inner)))
	}
	return group(rigid(seq(tokenDoc(in.open, n), lowered(inner), tokenDoc(in.close, n))))
}

func lowered(doc []item) []item {
	out := make([]item, len(doc))
	for i, it := range doc {
		switch it.tag {
		case tagText:
			it.nest--
		case tagGroup:
			it.items = lowered(it.items)
		case tagSpacing:
		}
		out[i] = it
	}
	return out
}

func interpOpened(in *interpolation, n int) []item {
	return group(interpOpen(in, n, group(exprDoc(in.value, n+1))))
}

func interpOpen(in *interpolation, n int, inner []item) []item {
	return seq(tokenDoc(in.open, n), plainBrk, inner, plainBrk, closeDoc(in.close, n))
}

func flatFits(doc []item) bool {
	width := 0
	return flatWithin(doc, &width)
}

func flatWithin(doc []item, width *int) bool {
	for i := range doc {
		it := &doc[i]
		switch it.tag {
		case tagText:
			if it.kind == textTrailing {
				continue
			}
			*width += utf8.RuneCountInString(it.text)
			if strings.TrimLeft(it.text, "(") == "if" {
				*width++
			}
		case tagSpacing:
			switch it.space {
			case lineHard, lineEmpty, lineCount:
				return false
			case spaceHard, spacePlain, spaceSoft:
				*width++
			case breakSoft, breakPlain:
			}
		case tagGroup:
			if !flatWithin(it.items, width) {
				return false
			}
		}
		if *width > flatInterpolationWidth {
			return false
		}
	}
	return true
}

func hugsInterpolation(e expr) bool {
	switch v := e.(type) {
	case *stringTerm:
		return v.open.lone() || isIndentedMulti(v)
	case *parenTerm:
		if !v.open.lone() || len(firstToken(v.inner).pre) > 0 {
			return false
		}
		if a, ok := v.inner.(*applyExpr); ok {
			return v.close.lone() && simpleCall(a)
		}
		return hugsInterpolation(v.inner)
	}
	return isAbsorbable(e)
}

func stringBody(s *stringTerm, n int) []item {
	if *s.probing > 0 {
		if simpleString(s) {
			return nil
		}
		return hardLine
	}
	return stringContent(s, n)
}

func stringContent(s *stringTerm, n int) []item {
	if s.indented {
		return indentedDoc(s, n)
	}
	out := text(n, `"`)
	for i, r := range splitRows(s.pieces) {
		if i > 0 {
			out = seq(out, text(n, "\n"))
		}
		out = seq(out, rowDoc(r, n, 0))
	}
	return seq(out, text(n, `"`))
}

func stringDoc(s *stringTerm, n int) []item {
	return seq(tokenPre(s.open, n), group(stringBody(s, n)), tokenTrail(s.open, n))
}

var quotedEscapes = strings.NewReplacer("''$", `\$`, "'''", "''")

type row []stringPiece

func splitRows(pieces []stringPiece) []row {
	rows := []row{nil}
	for _, p := range pieces {
		if p.interp != nil {
			rows[len(rows)-1] = append(rows[len(rows)-1], p)
			continue
		}
		for i, line := range strings.Split(p.text, "\n") {
			if i > 0 {
				rows = append(rows, nil)
			}
			if line != "" {
				rows[len(rows)-1] = append(rows[len(rows)-1], stringPiece{text: line})
			}
		}
	}
	return rows
}

func (r row) blank() bool {
	for _, p := range r {
		if p.interp != nil || strings.Trim(p.text, stringBlanks) != "" {
			return false
		}
	}
	return true
}

func (r row) lead() int {
	if len(r) == 0 || r[0].interp != nil {
		return 0
	}
	return len(r[0].text) - len(strings.TrimLeft(r[0].text, " "))
}

func quotedText(s string) string {
	return quotedEscapes.Replace(s)
}

func oneLineIndented(r row, n int) []item {
	if r.blank() {
		return text(n, `""`)
	}
	r = trimmedRow(r)
	plain := true
	for _, p := range r {
		if p.interp == nil && strings.ContainsAny(p.text, `"\`) {
			plain = false
		}
	}
	delim := `"`
	if !plain {
		delim = "''"
	}
	out := text(n, delim)
	lone := len(r) == 1 && r[0].interp != nil
	nest := n
	for _, p := range r {
		switch {
		case p.interp != nil && plain:
			out = seq(out, interpDoc(p.interp, n, lone))
		case p.interp != nil:
			out = seq(out, interpDoc(p.interp, n+1, lone))
		case plain:
			out = seq(out, text(n, quotedText(p.text)))
		default:
			out = seq(out, text(nest, p.text))
		}
		if !plain {
			nest = n + 1
		}
	}
	return seq(out, text(n, delim))
}

func allBlank(rows []row) bool {
	for _, r := range rows {
		if !r.blank() {
			return false
		}
	}
	return true
}

func trimmedRow(r row) row {
	if len(r) == 0 || r[0].interp != nil {
		return r
	}
	out := append(row(nil), r...)
	out[0].text = strings.TrimLeft(out[0].text, stringBlanks)
	if out[0].text == "" {
		out = out[1:]
	}
	return out
}

func indentedDoc(s *stringTerm, n int) []item {
	rows := splitRows(s.pieces)
	if len(rows) == 1 || (len(rows) == 2 && allBlank(rows)) {
		return oneLineIndented(rows[0], n)
	}
	if len(rows) == 2 && rows[0].blank() {
		return oneLineIndented(rows[1], n)
	}
	margin := -1
	for _, r := range rows {
		if r.blank() {
			continue
		}
		if l := r.lead(); margin < 0 || l < margin {
			margin = l
		}
	}
	onlyBlank := margin < 0
	margin = max(margin, 0)
	pending := 0
	if rows[0].blank() {
		rows = rows[1:]
		pending = 1
	}
	out := seq(text(n, "''"), plainBrk)
	for i, r := range rows {
		if i > 0 {
			pending++
		}
		if r.blank() && (onlyBlank || i == len(rows)-1 || r.emptyAfter(margin)) {
			continue
		}
		if i > 0 && pending > 0 {
			out = seq(out, lines(pending))
		}
		pending = 0
		out = seq(out, rowDoc(r, n+1, margin))
	}
	if rows[len(rows)-1].blank() {
		return seq(out, lines(max(pending, 1)), text(n, "''"))
	}
	return seq(out, text(n, "''"))
}

func (r row) emptyAfter(margin int) bool {
	return len(r) == 0 || len(r[0].text) <= margin
}

func rowDoc(r row, n, margin int) []item {
	var out []item
	shift := 0
	lone := false
	if count := len(r); count == 1 && r[0].interp != nil {
		lone = true
	} else if count == 2 && r[0].interp == nil && r[1].interp != nil && strings.Trim(r[0].text, " ") == "" {
		lone = true
	}
	for j, p := range r {
		if p.interp != nil {
			doc := interpDoc(p.interp, n, lone)
			if shift > 0 {
				doc = shifted(doc, shift)
			}
			out = seq(out, doc)
			continue
		}
		s := p.text
		if j == 0 {
			s = s[min(margin, len(s)):]
			shift = len(s) - len(strings.TrimLeft(s, " "))
		}
		if s == "" {
			continue
		}
		doc := text(n, s)
		if shift > 0 && j > 0 {
			doc = shifted(doc, shift)
		}
		out = seq(out, doc)
	}
	return out
}

func shifted(doc []item, by int) []item {
	out := make([]item, len(doc))
	for i, it := range doc {
		switch it.tag {
		case tagText:
			it.offset += int32(by) //nolint:gosec // G115: an offset is bounded by the width of a source line
		case tagGroup:
			it.items = shifted(it.items, by)
		case tagSpacing:
		}
		out[i] = it
	}
	return out
}

func trailingEntry(t *token, n int, edge []item) []item {
	if t.trail == "" {
		return nil
	}
	return seq(edge, textOf(n, "# "+t.trail, textTrailingComment), hardLine)
}

func listDoc(l *listTerm, n int) []item { return listLayout(l, n, hardLine) }

func listLayout(l *listTerm, n int, between []item) []item {
	open := tokenBare(l.open, n)
	edge := space
	if isMultilineList(l) {
		edge = hardLine
	}
	if isEmptyList(l) {
		return seq(open, emptyEdge(l.open, l.close), afterNotes(l.close, n))
	}
	lead := edge
	if len(l.entries) == 0 {
		lead = hardLine
	}
	out := seq(open, trailingEntry(l.open, n+1, lead), edge)
	for i, e := range l.entries {
		if i > 0 {
			out = seq(out, between)
		}
		out = seq(out, notesDoc(e.comments, n+1), group(exprDoc(e.value, n+1)))
	}
	if len(l.close.pre) > 0 {
		out = seq(out, hardLine, notesDoc(l.close.pre, n+1))
	}
	return seq(out, edge, afterNotes(l.close, n))
}

func emptyEdge(open, closing *token) []item {
	switch {
	case open.line == closing.line:
		return hardSpace
	case len(closing.pre) > 0:
		return emptyLine
	}
	return hardLine
}

func afterNotes(t *token, n int) []item {
	return seq(text(n, t.text), tokenTrail(t, n))
}

func setDoc(s *setTerm, n int, force bool) []item {
	var head []item
	if s.keyword != nil {
		head = seq(tokenDoc(s.keyword, n), hardSpace)
	}
	open := tokenBare(s.open, n)
	if isEmptySet(s) {
		return seq(head, open, emptyEdge(s.open, s.close), afterNotes(s.close, n))
	}
	edge := space
	if force || isMultilineSet(s) {
		edge = hardLine
	}
	out := seq(head, open, trailingEntry(s.open, n+1, edge), edge)
	out = seq(out, entriesDoc(s.entries, n+1))
	if len(s.close.pre) > 0 {
		out = seq(out, hardLine, notesDoc(s.close.pre, n+1))
	}
	return seq(out, edge, afterNotes(s.close, n))
}

func entriesDoc(entries []setEntry, n int) []item {
	var out []item
	for i, e := range entries {
		if i > 0 {
			out = seq(out, hardLine)
		}
		out = seq(out, notesDoc(e.comments, n), group(bindingDoc(e.binding, n)))
	}
	return out
}

func selectorDoc(s selector, n int) []item {
	var out []item
	if s.dot != nil {
		out = tokenDoc(s.dot, n)
	}
	switch {
	case s.name != nil:
		return seq(out, tokenDoc(s.name, n))
	case s.text != nil:
		return seq(out, stringDoc(s.text, n))
	}
	return seq(out, flatDoc(s.inter, n))
}

func pathOf(path []selector, n int) []item {
	var out []item
	for _, s := range path {
		out = seq(out, selectorDoc(s, n))
	}
	return out
}

func plainPath(path []selector) bool {
	if len(path) > plainPathLength {
		return false
	}
	for _, s := range path {
		if s.name == nil {
			return false
		}
	}
	return true
}

func bindingDoc(b binding, n int) []item {
	switch b := b.(type) {
	case *assignBinding:
		return assignDoc(b, n)
	case *inheritBinding:
		return inheritDoc(b, n)
	}
	panic("format: no layout for a parsed Nix binding")
}

func assignDoc(a *assignBinding, n int) []item {
	head := seq(pathOf(a.path, n), hardSpace, tokenDoc(a.equals, n+1))
	rhs := assignRHS(a, n+2)
	semi := tokenDoc(a.semi, n)
	if plainPath(a.path) {
		return seq(head, rhs, semi)
	}
	return seq(head, plainBrk, priority(rhs), semi)
}

func forcedDoc(e expr, n int) []item {
	switch e := e.(type) {
	case *setTerm:
		return setDoc(e, n, true)
	case *listTerm:
		return listDoc(e, n)
	}
	return groupedTerm(e, n)
}

func forcesSet(s *setTerm) bool {
	for _, e := range s.entries {
		if _, ok := e.binding.(*assignBinding); ok {
			return true
		}
	}
	return false
}

func assignRHS(a *assignBinding, m int) []item {
	if w, ok := a.value.(*withExpr); ok && a.equals.trail != "" && isAbsorbable(w.body) {
		return group(plainBrk, withAbsorbed(w, m))
	}
	return rhsDoc(a.value, m)
}

func rhsDoc(v expr, m int) []item {
	switch v := v.(type) {
	case *setTerm:
		if isAbsorbable(v) {
			return seq(hardSpace, group(setDoc(v, m, forcesSet(v))))
		}
		return group(space, exprDoc(v, m))
	case *listTerm:
		if isAbsorbable(v) {
			return seq(hardSpace, group(listDoc(v, m)))
		}
		return group(space, exprDoc(v, m))
	case *stringTerm:
		return seq(hardSpace, group(stringDoc(v, m)))
	case *uriTerm:
		return seq(hardSpace, group(tokenDoc(v.tok, m)))
	case *pathTerm:
		return seq(hardSpace, group(pathDoc(v, m)))
	case *parenTerm:
		if parenOfAbsorbable(v) {
			return seq(hardSpace, group(parenDoc(v, m)))
		}
		return seq(hardSpace, priority(parenOpen(v, m+1)))
	case *plainTerm:
		return group(space, tokenDoc(v.tok, m))
	case *selectTerm:
		return group(space, selectDoc(v, m))
	case *applyExpr:
		if simpleCall(v) {
			return seq(hardSpace, group(applyDoc(v, m)))
		}
		return liftedCall(v, m)
	case *withExpr:
		if isAbsorbable(v.body) {
			return group(space, withAbsorbed(v, m))
		}
		return group(space, withDoc(v, m))
	case *lambdaExpr:
		return lambdaRHS(v, m)
	case *binaryExpr:
		return binaryRHS(v, m)
	}
	return seq(space, group(exprDoc(v, m)))
}

func liftedCall(a *applyExpr, n int) []item {
	first := firstToken(a)
	pre := tokenPre(first, n)
	saved := first.pre
	first.pre = nil
	doc := seq(pre, group(space, callDoc(a.args, n, exprDoc(a.fn, n))))
	first.pre = saved
	return doc
}

func withAbsorbed(w *withExpr, n int) []item {
	return seq(tokenDoc(w.keyword, n), hardSpace, group(exprDoc(w.scope, n+1)), tokenDoc(w.semi, n), hardSpace, priority(forcedDoc(w.body, n)))
}

func paramIsSimple(p param) bool {
	switch p := p.(type) {
	case *setParam:
		return paramIsCompact(p)
	case *aliasParam:
		return paramIsSimple(p.first) && paramIsSimple(p.second)
	}
	return true
}

func hasDefault(p *setParam) bool {
	for _, f := range p.formals {
		if f.fallback != nil {
			return true
		}
	}
	return false
}

func lambdaRHS(l *lambdaExpr, m int) []item {
	if !lambdaEndsAbsorbable(l) {
		return seq(space, group(lambdaDoc(l, m, 1)))
	}
	if _, named := l.param.(*nameParam); named || widthBreaksHead(l) {
		return group(space, lambdaDoc(l, m, 1))
	}
	return seq(hardSpace, group(lambdaDoc(l, m, 1)))
}

func widthBreaksHead(l *lambdaExpr) bool {
	_, named := l.param.(*nameParam)
	return !named && len(l.colon.pre) == 0
}

func lambdaEndsAbsorbable(l *lambdaExpr) bool {
	for chainsIntoBody(l) {
		if !firstToken(l.body).inlinePre() {
			return false
		}
		switch b := l.body.(type) {
		case *lambdaExpr:
			if !firstToken(b).lone() || !b.colon.lone() {
				return false
			}
			l = b
			continue
		case *withExpr:
			_, named := l.param.(*nameParam)
			return !named && isAbsorbable(b.body)
		}
		return parenAbsorbs(l.body)
	}
	return false
}

func bodyOnColonLine(l *lambdaExpr) bool {
	return l.colon.trail == "" && firstToken(l.body).line == l.colon.line
}

func chainsIntoBody(l *lambdaExpr) bool {
	switch p := l.param.(type) {
	case *nameParam:
		return true
	case *setParam:
		return paramIsCompact(p) && bodyOnColonLine(l)
	}
	return false
}

func binaryRHS(b *binaryExpr, m int) []item {
	switch b.op {
	case syntax.OpUpdate, syntax.OpConcat, syntax.OpAdd:
	default:
		return seq(space, group(binaryDoc(b, m)))
	}
	operands, _ := flattenBinary(b)
	if isAbsorbable(operands[0]) && len(firstToken(operands[0]).pre) == 0 {
		return seq(hardSpace, group(binaryAbsorbed(b, m-1)))
	}
	if len(operands) == 2 && isAbsorbable(b.right) {
		if _, nested := b.left.(*binaryExpr); !nested && b.opTok.lone() {
			return group(space, operandDoc(b.left, m), space, transparent(tokenDoc(b.opTok, m), hardSpace, priority(forcedDoc(b.right, m))))
		}
	}
	return seq(space, group(binaryDoc(b, m)))
}

func binaryAbsorbed(b *binaryExpr, n int) []item {
	operands, ops := flattenBinary(b)
	return seq(forcedOperand(operands[0], n), chainDoc(operands, ops, n))
}

func forcedOperand(e expr, n int) []item {
	if list, ok := e.(*listTerm); ok {
		return listDoc(list, n)
	}
	return forcedDoc(e, n)
}

func inheritDoc(i *inheritBinding, n int) []item {
	multi := i.semi.line != i.keyword.line || len(i.names) > compactInherit
	sep := space
	if multi {
		sep = hardLine
	}
	out := tokenDoc(i.keyword, n)
	if i.from != nil {
		out = seq(out, group(space, group(parenDoc(i.from, n+1))))
	}
	for _, s := range i.names {
		if s.name != nil {
			out = seq(out, sep, tokenDoc(s.name, n+1))
			continue
		}
		out = seq(out, sep, group(selectorDoc(s, n+1)))
	}
	switch {
	case multi:
		out = seq(out, hardLine)
	case len(i.names) == 0:
		out = seq(out, space)
	default:
		out = seq(out, plainBrk)
	}
	return seq(out, tokenDoc(i.semi, n+1))
}

func selectDoc(s *selectTerm, n int) []item {
	var out []item
	switch t := s.target.(type) {
	case *plainTerm:
		out = exprDoc(t, n)
		if strings.ContainsAny(t.tok.text[:1], "0123456789.") {
			out = seq(out, hardSpace)
		}
	case *pathTerm:
		out = seq(exprDoc(t, n), hardSpace)
	case *setTerm:
		out = seq(exprDoc(t, n), plainBrk)
	case *parenTerm:
		out = seq(group(parenDoc(t, n)), softBreak)
	case *listTerm, *stringTerm, *uriTerm:
		out = seq(group(exprDoc(t, n)), plainBrk)
	default:
		out = exprDoc(t, n)
	}
	out = seq(out, pathOf(s.path, n))
	if s.or == nil {
		return out
	}
	out = seq(out, softSpace, tokenDoc(s.or, n+1), hardSpace)
	return seq(out, inlineFallback(s.fallback, n+1))
}

func inlineFallback(t term, n int) []item {
	switch t.(type) {
	case *plainTerm, *setTerm, *pathTerm, *selectTerm:
		return exprDoc(t, n)
	}
	return groupedTerm(t, n)
}

func raisedParen(p *parenTerm, n int) []item {
	open := *p.open
	note := trivium{kind: lineNote, text: " " + open.trail}
	open.trail = ""
	open.pre = append(append([]trivium(nil), open.pre...), note)
	return enclosed(&open, p.inner, p.close, n)
}

func raisedInner(p *parenTerm, n int) []item {
	open := *p.open
	open.trail = ""
	first := firstToken(p.inner)
	saved := first.pre
	first.pre = append([]trivium{{kind: lineNote, text: " " + p.open.trail}}, saved...)
	doc := seq(tokenDoc(&open, n), plainBrk, group(exprDoc(p.inner, n+1)), plainBrk, closeDoc(p.close, n))
	first.pre = saved
	return doc
}

func closeDoc(t *token, n int) []item {
	return seq(tokenPre(t, n+1), text(n, t.text), tokenTrail(t, n))
}

func parenDoc(p *parenTerm, n int) []item {
	if p.open.trail != "" {
		return raisedParen(p, n)
	}
	return enclosed(p.open, p.inner, p.close, n)
}

func parenOpen(p *parenTerm, n int) []item {
	if p.open.trail != "" {
		return raisedInner(p, n)
	}
	return seq(tokenDoc(p.open, n), plainBrk, group(exprDoc(p.inner, n+1)), plainBrk, closeDoc(p.close, n))
}

func enclosed(open *token, inner expr, closing *token, n int) []item {
	o, c := tokenDoc(open, n), closeDoc(closing, n)
	body := func() []item { return group(exprDoc(inner, n+1)) }
	switch v := inner.(type) {
	case *setTerm, *listTerm, *stringTerm, *parenTerm:
		if parenAbsorbs(v) {
			return seq(o, group(exprDoc(inner, n+1)), c)
		}
	case *applyExpr:
		if simpleCall(v) {
			return seq(o, body(), c)
		}
		if f := firstToken(v); len(f.pre) > 0 || (f.trail != "" && f.text == "(") {
			pre := tokenPre(f, n+1)
			saved := f.pre
			f.pre = nil
			doc := seq(o, pre, group(callDoc(v.args, n+1, exprDoc(v.fn, n+1)), plainBrk), hardLine, c)
			f.pre = saved
			return doc
		}
		if open.text == interpolationOpen && lastToken(v).trail != "" {
			break
		}
		return seq(o, group(hugApply(v, n+1), plainBrk), c)
	case *selectTerm:
		if !parenAbsorbs(v.target) {
			return seq(o, body(), plainBrk, c)
		}
	case *lambdaExpr:
		if !lambdaEndsAbsorbable(v) {
			break
		}
		if widthBreaksHead(v) {
			return seq(o, group(plainBrk, exprDoc(inner, n+1), plainBrk), c)
		}
		return seq(o, body(), c)
	case *withExpr:
		if isAbsorbable(v.body) {
			return seq(o, group(plainBrk, withAbsorbed(v, n+1)), c)
		}
	}
	return seq(o, plainBrk, body(), plainBrk, c)
}

func hugApply(a *applyExpr, n int) []item {
	return callDoc(a.args, n, group(plainBrk, exprDoc(a.fn, n)))
}

func applyDoc(a *applyExpr, n int) []item {
	if simpleCall(a) {
		return simpleCallDoc(a, n)
	}
	return callDoc(a.args, n, exprDoc(a.fn, n))
}

func simpleCallDoc(a *applyExpr, n int) []item {
	out := simpleText(a.fn, n)
	for _, x := range a.args[:len(a.args)-1] {
		out = seq(out, hardSpace, simpleText(x, n+1))
	}
	last := a.args[len(a.args)-1]
	if v, ok := last.(*parenTerm); ok {
		if isIndentedMulti(v.inner) {
			return seq(out, hardSpace, priority(group(parenDoc(v, n+1))))
		}
		if hug := parenHugOnce(v, n+1); hug != nil {
			return seq(out, hardSpace, priority(hug))
		}
		return seq(out, hardSpace, priority(parenOpen(v, n+1)))
	}
	if isIndentedMulti(last) {
		return seq(out, hardSpace, priority(group(exprDoc(last, n+1))))
	}
	return seq(out, hardSpace, group(exprDoc(last, n+1)))
}

func simpleText(e expr, n int) []item {
	switch v := e.(type) {
	case *parenTerm:
		return seq(text(n, v.open.text), simpleInner(v.inner, n+1), text(n, v.close.text))
	case *applyExpr:
		return simpleInner(v, n)
	case *stringTerm:
		return rigid(stringBody(v, n))
	}
	return rigid(exprDoc(e, n))
}

func simpleInner(e expr, n int) []item {
	a, ok := e.(*applyExpr)
	if !ok {
		return simpleText(e, n)
	}
	out := simpleText(a.fn, n)
	for _, x := range a.args {
		out = seq(out, hardSpace, simpleText(x, n+1))
	}
	return out
}

func callDoc(args []expr, n int, fnItems []item) []item {
	doc := transparent(fnItems)
	m := n + 1
	count := len(args)
	last := count - 1
	pair := make([]bool, count)
	for i := count - 2; i >= 0; i-- {
		if isList(args[i]) && isList(args[i+1]) && !pair[i+1] {
			pair[i] = true
		}
	}
	for i := 0; i < last; i++ {
		if pair[i] && i+1 == last {
			return seq(doc, space, group(pairList(args[i], m)), space, group(pairList(args[last], m)))
		}
		if pair[i] {
			doc = transparent(doc, group(space, group(pairList(args[i], m)), space, group(pairList(args[i+1], m))))
			i++
			continue
		}
		if sel, ok := args[i].(*selectTerm); ok {
			doc = transparent(doc, space, group(selectDoc(sel, m)))
			continue
		}
		doc = transparent(doc, space, priority(argDoc(args[i], m)))
	}
	if pair[max(last-1, 0)] && last > 0 {
		return doc
	}
	return seq(doc, space, lastArg(args[last], m))
}

func isList(e expr) bool {
	_, ok := e.(*listTerm)
	return ok
}

func pairList(e expr, n int) []item {
	l, ok := e.(*listTerm)
	if !ok {
		panic("format: only lists pair up as arguments")
	}
	return listArgDoc(l, n)
}

func listArgDoc(l *listTerm, n int) []item {
	if simpleList(l) {
		return listLayout(l, n, space)
	}
	return listLayout(l, n, hardLine)
}

func simpleList(l *listTerm) bool {
	if l.open.trail != "" || l.close.trail != "" || len(l.entries) > simpleListLength {
		return false
	}
	for _, e := range l.entries {
		if hasComment(e.comments) || !isSimple(e.value) {
			return false
		}
	}
	return true
}

func argDoc(e expr, n int) []item {
	switch v := e.(type) {
	case *listTerm:
		if simpleList(v) {
			return listArgDoc(v, n)
		}
		return group(listArgDoc(v, n))
	case *setTerm:
		return setDoc(v, n, false)
	case *stringTerm:
		return stringDoc(v, n)
	case *parenTerm:
		return group(parenDoc(v, n))
	}
	return exprDoc(e, n)
}

func lastArg(e expr, n int) []item {
	switch v := e.(type) {
	case *setTerm:
		if isAbsorbable(v) {
			return priority(setDoc(v, n, false))
		}
	case *listTerm:
		if isAbsorbable(v) {
			return priority(listDoc(v, n))
		}
	case *parenTerm:
		if parenOfAbsorbable(v) {
			return priority(group(parenDoc(v, n)))
		}
		if hug := parenHugOnce(v, n); hug != nil {
			return priority(hug)
		}
		return priority(parenOpen(v, n))
	}
	if isIndentedMulti(e) {
		return priority(group(exprDoc(e, n)))
	}
	return group(exprDoc(e, n))
}

func parenHugOnce(p *parenTerm, n int) []item {
	if p.open.trail != "" || len(p.open.pre) > 0 {
		return nil
	}
	switch v := p.inner.(type) {
	case *lambdaExpr:
		if n2, ok := v.param.(*nameParam); ok && n2.name.lone() && v.colon.lone() && parenAbsorbs(v.body) {
			return seq(tokenDoc(p.open, n), tokenDoc(n2.name, n), tokenDoc(v.colon, n), hardSpace, forcedDoc(v.body, n), tokenDoc(p.close, n))
		}
	case *applyExpr:
		if fn, ok := v.fn.(*plainTerm); ok && fn.tok.lone() && p.close.lone() && len(v.args) == 1 && isAbsorbable(v.args[0]) {
			return seq(tokenDoc(p.open, n), tokenDoc(fn.tok, n), hardSpace, forcedDoc(v.args[0], n), tokenDoc(p.close, n))
		}
	}
	return nil
}

func lambdaDoc(l *lambdaExpr, n, depth int) []item { return chainedLambda(l, n, depth, false) }

func colonPinsChain(l *lambdaExpr) bool {
	return len(l.colon.pre) > 0 && l.colon.trail == "" && lambdaEndsAbsorbable(l)
}

func chainedLambda(l *lambdaExpr, n, depth int, pinned bool) []item {
	pinned = pinned || colonPinsChain(l)
	widthBreaks := !pinned && widthBreaksHead(l)
	head := seq(paramDoc(l.param, n), tokenDoc(l.colon, n))
	if depth == 1 {
		head = group(head)
	}
	body := l.body
	name, named := l.param.(*nameParam)
	if named {
		head = seq(tokenDoc(name.name, n), tokenDoc(l.colon, n))
		if depth == 1 {
			head = seq(plainBrk, head)
		}
	}
	if _, alias := l.param.(*aliasParam); alias || (!named && !paramIsSimple(l.param) && bodyOnColonLine(l)) {
		if set, ok := body.(*setTerm); ok && isAbsorbable(set) {
			return seq(head, space, group(setDoc(set, n, true)))
		}
		return seq(head, space, inlineBody(body, n))
	}
	if !named && !bodyOnColonLine(l) {
		return seq(head, hardLine, inlineBody(body, n))
	}
	switch v := body.(type) {
	case *lambdaExpr:
		if _, next := v.param.(*nameParam); next && named {
			return seq(head, hardSpace, lambdaDoc(v, n, depth+1))
		}
		if _, next := v.param.(*nameParam); !next {
			if pinned {
				return seq(head, hardSpace, chainedLambda(v, n, depth+1, true))
			}
			return seq(head, space, lambdaDoc(v, n, depth+1))
		}
		if lambdaEndsAbsorbable(v) {
			if widthBreaks && firstToken(v).lone() && v.colon.lone() {
				return seq(head, space, priority(group(lambdaDoc(v, n, 2))))
			}
			return seq(head, priority(group(space, lambdaDoc(v, n, 2))))
		}
		return seq(head, space, group(lambdaDoc(v, n, 1)))
	}
	if isAbsorbable(body) || parenOfAbsorbable(body) {
		if widthBreaks {
			return seq(head, space, openingPriority(absorbedBody(body, n, named)))
		}
		return seq(head, hardSpace, priority(absorbedBody(body, n, named)))
	}
	if w, ok := body.(*withExpr); ok && isAbsorbable(w.body) {
		if named {
			return seq(head, priority(group(space, withAbsorbed(w, n))))
		}
		if widthBreaks {
			return seq(head, space, openingPriority(withDoc(w, n)))
		}
		return seq(head, hardSpace, priority(withDoc(w, n)))
	}
	sep := space
	if depth >= 3 && named {
		sep = hardLine
	}
	return seq(head, sep, inlineBody(body, n))
}

func parenOfAbsorbable(e expr) bool {
	p, ok := e.(*parenTerm)
	return ok && p.open.lone() && firstToken(p.inner).inlinePre() && parenAbsorbs(p.inner)
}

func parenAbsorbs(e expr) bool { return isAbsorbable(e) || parenOfAbsorbable(e) }

func absorbedBody(e expr, n int, named bool) []item {
	switch v := e.(type) {
	case *setTerm:
		return setDoc(v, n, false)
	case *listTerm:
		if named {
			return listDoc(v, n)
		}
		return group(listDoc(v, n))
	case *parenTerm:
		return group(parenDoc(v, n))
	}
	return group(exprDoc(e, n))
}

func paramDoc(p param, n int) []item {
	switch p := p.(type) {
	case *nameParam:
		return tokenDoc(p.name, n)
	case *setParam:
		return group(setParamDoc(p, n))
	case *aliasParam:
		return seq(paramDoc(p.first, n), tokenDoc(p.at, n), paramDoc(p.second, n))
	}
	panic("format: no layout for a parsed Nix parameter")
}

func setParamDoc(p *setParam, n int) []item {
	open := tokenBare(p.open, n)
	if len(p.formals) == 0 {
		return seq(open, formalsEdge(p), tokenDoc(p.close, n))
	}
	sep := space
	if !paramIsCompact(p) {
		sep = hardLine
	}
	out := seq(open, sep)
	for i := range p.formals {
		f := &p.formals[i]
		if i > 0 {
			out = seq(out, sep)
		}
		out = seq(out, notesDoc(f.comments, n+1), formalDoc(f, n+1, i == len(p.formals)-1))
	}
	if len(p.close.pre) > 0 {
		out = seq(out, hardLine, notesDoc(p.close.pre, n+1))
	}
	return seq(out, sep, afterNotes(p.close, n))
}

func paramIsCompact(p *setParam) bool {
	named := len(p.formals)
	if named > 0 && p.formals[named-1].name.text == "..." {
		named--
	}
	return named <= compactFormals && !hasDefault(p) && p.open.line == p.close.line && p.close.trail == "" && !formalsCommented(p)
}

func formalsCommented(p *setParam) bool {
	for i := range p.formals {
		f := &p.formals[i]
		if f.name.trail != "" || len(f.comments) > 0 || (f.comma != nil && !f.comma.lone()) {
			return true
		}
	}
	return false
}

func formalsEdge(p *setParam) []item {
	if p.open.line == p.close.line {
		return hardSpace
	}
	if len(p.close.pre) > 0 {
		return nil
	}
	return hardLine
}

func formalDoc(f *formalEntry, n int, last bool) []item {
	if f.name.text == "..." {
		return tokenDoc(f.name, n)
	}
	comma := f.comma
	if last && comma != nil && comma.lone() {
		comma = nil
	}
	body := tokenDoc(f.name, n)
	if f.fallback != nil {
		body = seq(body, hardSpace, tokenDoc(f.mark, n+1), rhsDoc(f.fallback, n+2))
		if comma != nil {
			return group(body, commaDoc(comma, n, last))
		}
		return seq(group(body), textOf(n, ",", textTrailing))
	}
	if comma != nil {
		return seq(body, commaDoc(comma, n, last))
	}
	return seq(body, textOf(n, ",", textTrailing))
}

func commaDoc(comma *token, n int, last bool) []item {
	if last || len(comma.pre) == 0 || comma.trail != "" {
		return tokenDoc(comma, n)
	}
	return seq(afterNotes(comma, n), hardLine, notesDoc(comma.pre, n))
}

func withDoc(w *withExpr, n int) []item {
	head := group(tokenDoc(w.keyword, n), hardSpace, group(exprDoc(w.scope, n+1)), tokenDoc(w.semi, n))
	return seq(head, space, inlineBody(w.body, n))
}

func assertDoc(a *assertExpr, n int) []item {
	keyword := tokenDoc(a.keyword, n)
	var head []item
	switch c := a.cond.(type) {
	case *applyExpr:
		args := append([]expr{c.fn}, c.args...)
		head = callDoc(args, n, keyword)
	default:
		head = seq(transparent(keyword), space, lastArg(a.cond, n+1))
	}
	return seq(group(head), tokenDoc(a.semi, n), hardLine, inlineBody(a.body, n))
}

func letDoc(l *letExpr, n int) []item {
	first := tokenDoc(l.keyword, n)
	if len(l.entries) > 0 {
		first = seq(first, hardLine, entriesDoc(l.entries, n+1))
	}
	if len(l.in.pre) > 0 {
		first = seq(first, hardLine, notesDoc(l.in.pre, n+1))
	}
	inner := seq(afterNotes(l.in, n), hardLine, inlineBody(l.body, n))
	return seq(group(first), hardLine, group(inner))
}

func ifDoc(i *ifExpr, n int, chained bool) []item {
	head := group(tokenDoc(i.ifTok, n), space, inlineBody(i.cond, n+1), space, tokenDoc(i.thenTok, n))
	sep := space
	if chained {
		sep = hardLine
	}
	out := seq(head, sep, group(exprDoc(i.then, n+1)), sep, tokenDoc(i.elseTok, n))
	if next, ok := i.otherwise.(*ifExpr); ok {
		return seq(out, hardSpace, ifDoc(next, n, true))
	}
	return seq(out, space, group(exprDoc(i.otherwise, n+1)))
}

func flattenBinary(b *binaryExpr) ([]expr, []*token) {
	var operands []expr
	var ops []*token
	var walk func(e expr)
	walk = func(e expr) {
		if x, ok := e.(*binaryExpr); ok && x.op == b.op {
			walk(x.left)
			ops = append(ops, x.opTok)
			walk(x.right)
			return
		}
		operands = append(operands, e)
	}
	walk(b)
	return operands, ops
}

func chainDoc(operands []expr, ops []*token, n int) []item {
	var out []item
	for i, o := range operands[1:] {
		out = seq(out, space, tokenDoc(ops[i], n))
		if x, ok := o.(*binaryExpr); ok {
			if comparison(x.op) {
				out = seq(out, group(space, binaryDoc(x, n+1)))
				continue
			}
			out = seq(out, group(space, group(binaryDoc(x, n+1))))
			continue
		}
		out = seq(out, rightOperand(o, n+1))
	}
	return out
}

func leftOperand(e expr, n int) []item {
	if x, ok := e.(*binaryExpr); ok {
		if comparison(x.op) {
			return binaryDoc(x, n)
		}
		return group(binaryDoc(x, n))
	}
	return operandDoc(e, n)
}

func binaryDoc(b *binaryExpr, n int) []item {
	if comparison(b.op) {
		left := operandDoc(b.left, n)
		if x, ok := b.left.(*binaryExpr); ok && comparison(x.op) {
			left = binaryDoc(x, n)
		}
		return seq(left, softSpace, tokenDoc(b.opTok, n), hardSpace, operandDoc(b.right, n))
	}
	operands, ops := flattenBinary(b)
	return seq(leftOperand(operands[0], n), chainDoc(operands, ops, n))
}

func rightOperand(e expr, n int) []item {
	if a, ok := e.(*applyExpr); ok && !simpleCall(a) {
		return liftedCall(a, n)
	}
	return seq(hardSpace, operandDoc(e, n))
}

func operandDoc(e expr, n int) []item {
	switch e.(type) {
	case *plainTerm, *setTerm, *unaryExpr, *pathTerm, *selectTerm:
		return exprDoc(e, n)
	}
	return groupedTerm(e, n)
}

func hasAttrDoc(h *hasAttrExpr, n int) []item {
	var target []item
	switch h.target.(type) {
	case *plainTerm, *uriTerm, *setTerm, *selectTerm:
		target = exprDoc(h.target, n)
	default:
		target = groupedTerm(h.target, n)
	}
	return seq(target, space, tokenDoc(h.mark, n+1), hardSpace, pathOf(h.path, n+1))
}

func startsWithPath(e expr) bool {
	switch e := e.(type) {
	case *pathTerm:
		return true
	case *selectTerm:
		return startsWithPath(e.target)
	case *applyExpr:
		return startsWithPath(e.fn)
	}
	return false
}

func unaryDoc(u *unaryExpr, n int) []item {
	var parts [][]item
	for {
		parts = append(parts, tokenDoc(u.opTok, n))
		next, chained := u.operand.(*unaryExpr)
		if !chained {
			break
		}
		u = next
	}
	if u.opTok.text == "-" && startsWithPath(u.operand) {
		parts = append(parts, hardSpace)
	}
	switch u.operand.(type) {
	case *plainTerm, *selectTerm, *setTerm:
		return seq(append(parts, exprDoc(u.operand, n))...)
	}
	return seq(append(parts, groupedTerm(u.operand, n))...)
}
