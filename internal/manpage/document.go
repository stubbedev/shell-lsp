package manpage

import (
	"regexp"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/scan"
)

const (
	controlChars  = ".'"
	blockOpen     = `\{`
	blockClose    = `\}`
	definitionEnd = ".."
)

var (
	bareWord       = regexp.MustCompile(`^[a-z][a-z0-9_-]*[a-z0-9]$`)
	crossReference = regexp.MustCompile(`^([^()]+)\([0-9][A-Za-z]*\)$`)
)

type headingKind uint8

const (
	noHeading headingKind = iota
	sectionHeading
	subsectionHeading
)

type listKind uint8

const (
	listPlain listKind = iota
	listTagged
)

type builder struct {
	st         roffState
	doc        document
	text       strings.Builder
	kind       blockKind
	tag        string
	nofill     bool
	tagLines   int
	tagText    strings.Builder
	skipDepth  int
	skipUntil  string
	lastIf     bool
	mdoc       bool
	name       string
	lists      []listKind
	headingDue headingKind
	indent     int
	depth      int
}

func parseDocument(src string) document {
	b := &builder{st: roffState{strings: map[string]string{}}}
	b.mdoc = isMdoc(src)
	for _, line := range joinContinuations(strings.Split(strings.ReplaceAll(src, "\r", ""), "\n")) {
		b.line(line)
	}
	b.flush()
	return b.doc
}

func joinContinuations(lines []string) []string {
	out := make([]string, 0, len(lines))
	var pending strings.Builder
	for _, line := range lines {
		if continues(line) {
			pending.WriteString(line[:len(line)-1])
			continue
		}
		pending.WriteString(line)
		out = append(out, pending.String())
		pending.Reset()
	}
	if pending.Len() > 0 {
		out = append(out, pending.String())
	}
	return out
}

func continues(line string) bool {
	return scan.Continued(line) && !strings.HasPrefix(line, `.\"`)
}

func isMdoc(src string) bool {
	for line := range strings.SplitSeq(src, "\n") {
		switch {
		case strings.HasPrefix(line, ".Dd"):
			return true
		case strings.HasPrefix(line, ".TH"), strings.HasPrefix(line, ".SH"):
			return false
		}
	}
	return false
}

func (b *builder) line(line string) {
	if b.skipUntil != "" {
		if strings.Join(strings.Fields(line), "") == b.skipUntil {
			b.skipUntil = ""
		}
		return
	}
	if b.skipDepth > 0 {
		b.skipDepth += strings.Count(line, blockOpen) - strings.Count(line, blockClose)
		return
	}
	line = strings.ReplaceAll(line, blockClose, "")
	if line == "" || line == "." {
		if !b.nofill {
			b.flush()
		} else {
			b.text.WriteByte('\n')
		}
		return
	}
	if strings.IndexByte(controlChars, line[0]) >= 0 {
		b.request(line)
		return
	}
	b.textLine(line)
}

func (b *builder) textLine(line string) {
	if !b.nofill && b.tagLines == 0 && b.headingDue == noHeading && (line[0] == ' ' || line[0] == '\t') {
		b.promoteHeading()
	}
	b.appendRendered(b.st.inline(line))
}

func (b *builder) appendRendered(text string) {
	switch b.headingDue {
	case sectionHeading:
		b.startSection(text)
		return
	case subsectionHeading:
		b.startSubsection(text)
		return
	case noHeading:
	}
	if b.tagLines > 0 {
		b.appendWords(&b.tagText, text)
		b.tagLines--
		if b.tagLines == 0 {
			b.kind, b.tag = taggedBlock, strings.TrimSpace(b.tagText.String())
			b.tagText.Reset()
		}
		return
	}
	if b.nofill {
		b.text.WriteString(text)
		b.text.WriteByte('\n')
		return
	}
	b.appendWords(&b.text, text)
}

func (b *builder) appendWords(sb *strings.Builder, text string) {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return
	}
	if sb.Len() > 0 && !strings.HasSuffix(sb.String(), " ") {
		sb.WriteByte(' ')
	}
	sb.WriteString(text)
}

func (b *builder) flush() {
	text := strings.TrimRight(b.text.String(), " \n")
	if b.kind != literalBlock {
		text = strings.TrimSpace(text)
	}
	if text != "" || b.kind == taggedBlock {
		if len(b.doc.sections) == 0 {
			b.doc.sections = append(b.doc.sections, section{})
		}
		last := &b.doc.sections[len(b.doc.sections)-1]
		last.blocks = append(last.blocks, block{kind: b.kind, tag: b.tag, text: text, depth: b.depth})
	}
	b.text.Reset()
	b.tag = ""
	b.depth = 0
	b.kind = paragraphBlock
	if b.nofill {
		b.kind = literalBlock
	}
}

func (b *builder) startSection(name string) {
	b.flush()
	b.headingDue, b.indent = noHeading, 0
	b.doc.sections = append(b.doc.sections, section{name: strings.ToUpper(strings.TrimSpace(name))})
}

func (b *builder) startSubsection(name string) {
	b.flush()
	b.headingDue, b.indent = noHeading, 0
	b.kind = headingBlock
	b.appendWords(&b.text, name)
	b.flush()
}

func (b *builder) promoteHeading() {
	text := strings.TrimSpace(b.text.String())
	if b.kind != paragraphBlock || !isTerm(text) {
		b.flush()
		return
	}
	b.text.Reset()
	b.startTag(text)
}

func isTerm(text string) bool {
	if text == "" || strings.Contains(text, ". ") {
		return false
	}
	if strings.HasPrefix(text, "-") {
		return true
	}
	first, _, _ := strings.Cut(text, " ")
	name, _ := splitReference(strings.TrimSuffix(first, ","))
	return bareWord.MatchString(name) && !endsSentence(text)
}

func splitReference(word string) (string, bool) {
	if m := crossReference.FindStringSubmatch(word); m != nil {
		return m[1], true
	}
	return word, false
}

func endsSentence(text string) bool {
	return strings.HasSuffix(text, ":") || strings.HasSuffix(text, ".") && !strings.HasSuffix(text, "...")
}

func (b *builder) startTag(tag string) {
	b.kind, b.tag, b.depth = taggedBlock, tag, b.level()
}

func (b *builder) level() int {
	return b.indent + max(len(b.lists)-1, 0)
}

func (b *builder) currentSection() string {
	if len(b.doc.sections) == 0 {
		return ""
	}
	return b.doc.sections[len(b.doc.sections)-1].name
}

func (b *builder) request(line string) {
	name, rest := splitRequest(line)
	if strings.HasPrefix(name, `\"`) || name == "" {
		return
	}
	switch name {
	case "de", "de1", "am", "ig":
		b.skipUntil = definitionEnd
		return
	case "ds", "ds1":
		key, value, _ := strings.Cut(rest, " ")
		b.st.strings[key] = b.st.inline(strings.TrimPrefix(value, `"`))
		return
	case "if":
		cond, body := splitCondition(rest)
		b.lastIf = evaluate(cond)
		b.conditional(b.lastIf, body)
		return
	case "ie":
		cond, body := splitCondition(rest)
		b.lastIf = evaluate(cond)
		b.conditional(b.lastIf, body)
		return
	case "el":
		b.conditional(!b.lastIf, rest)
		return
	}
	if b.mdoc {
		b.mdocRequest(name, rest)
		return
	}
	b.manRequest(name, rest)
}

func splitCondition(rest string) (string, string) {
	rest = strings.TrimLeft(rest, " \t")
	if rest == "" {
		return "", ""
	}
	if rest[0] == '\'' {
		end := strings.IndexByte(rest[1:], '\'')
		if end >= 0 {
			if second := strings.IndexByte(rest[end+2:], '\''); second >= 0 {
				return rest[:end+2+second+1], strings.TrimLeft(rest[end+2+second+1:], " \t")
			}
		}
	}
	cond, body, _ := strings.Cut(rest, " ")
	return cond, body
}

func evaluate(cond string) bool {
	negate := strings.HasPrefix(cond, "!")
	cond = strings.TrimPrefix(cond, "!")
	result := cond == "n" || strings.HasPrefix(cond, `\n(.g`) || strings.HasPrefix(cond, `\n[.g]`)
	return result != negate
}

func (b *builder) conditional(taken bool, body string) {
	body = strings.TrimLeft(body, " \t")
	opens := strings.HasPrefix(body, blockOpen)
	body = strings.TrimPrefix(body, blockOpen)
	body = strings.TrimPrefix(body, `\`)
	if !taken {
		if opens {
			b.skipDepth = 1 + strings.Count(body, blockOpen) - strings.Count(body, blockClose)
		}
		return
	}
	body = strings.TrimSpace(strings.ReplaceAll(body, blockClose, ""))
	if body != "" {
		b.line(body)
	}
}

func (b *builder) manRequest(name, rest string) {
	args := requestArgs(rest)
	switch name {
	case "SH":
		if len(args) == 0 {
			b.flush()
			b.headingDue = sectionHeading
			return
		}
		b.startSection(b.st.inline(strings.Join(args, " ")))
	case "PP", "P", "LP", "HP", "sp", "Sp", "br", "SY", "YS":
		b.flush()
		if name == "SY" && len(args) > 0 {
			b.textLine(strings.Join(args, " "))
		}
	case "SS":
		if len(args) == 0 {
			b.flush()
			b.headingDue, b.indent = subsectionHeading, 0
			return
		}
		b.startSubsection(b.st.inline(strings.Join(args, " ")))
	case "RE":
		b.flush()
		b.indent = max(b.indent-1, 0)
	case "TP":
		b.flush()
		b.tagLines, b.depth = 1, b.level()
	case "TQ":
		b.kind, b.tagLines = taggedBlock, 1
		b.tagText.WriteString(b.tag + ", ")
	case "IP":
		if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
			b.promoteHeading()
			return
		}
		b.flush()
		b.startTag(b.st.inline(args[0]))
	case "RS":
		b.promoteHeading()
		b.indent++
	case "B", "I", "SM", "SB", "CW":
		if len(args) > 0 {
			b.textLine(strings.Join(args, " "))
		}
	case "BR", "BI", "IB", "IR", "RB", "RI":
		b.textLine(strings.Join(args, ""))
	case "OP":
		b.textLine("[" + strings.Join(args, " ") + "]")
	case "UR", "MT":
	case "UE", "ME":
		if len(args) > 0 {
			b.textLine(args[0])
		}
	case "nf", "EX", "Vb":
		b.flush()
		b.nofill, b.kind = true, literalBlock
	case "fi", "EE", "Ve":
		b.flush()
		b.nofill = false
		b.kind = paragraphBlock
	}
}
