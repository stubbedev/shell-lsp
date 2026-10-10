package lint

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

type Severity uint8

const (
	SeverityError Severity = iota + 1
	SeverityWarning
	SeverityInfo
	SeverityStyle
)

type Tag uint8

const (
	TagNone Tag = iota
	TagUnnecessary
	TagDeprecated
)

type Code uint16

func (c Code) String() string { return "SC" + strconv.Itoa(int(c)) }

func ParseCode(s string) (Code, error) {
	digits := strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(s)), "SC")
	n, err := strconv.ParseUint(digits, 10, 16)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("expected a check code like SC2034, got %q", s)
	}
	return Code(n), nil
}

type Finding struct {
	Code     Code
	Severity Severity
	Tag      Tag
	Span     parser.Span
	Message  string
	Fix      []textedit.Edit
}

type Source struct {
	Command parser.Span
	Tree    *parser.Tree
}

type DialectSource uint8

const (
	DialectDetected DialectSource = iota
	DialectForced
	DialectGuessed
)

type Options struct {
	Exclude       []Code
	Optional      []string
	DialectSource DialectSource
	Sources       []Source
}

func Check(tree *parser.Tree, imports []parser.Import, opts Options) []Finding {
	p := newPass(tree, imports, opts)
	p.shellChosen = opts.DialectSource == DialectForced || p.settings.shell
	p.dialectGuessed = opts.DialectSource == DialectGuessed && !p.settings.shell
	if name, foreign := foreignInterpreter(p.text); foreign && !p.shellChosen {
		first, _, _ := strings.Cut(p.text, "\n")
		p.report(1071, parser.Span{Start: 0, End: len(first)}, name+" is not a shell, so this file is not linted.")
	} else {
		p.run()
	}
	suppressed := p.suppressions()
	out := p.findings[:0]
	for _, f := range p.findings {
		if slices.Contains(opts.Exclude, f.Code) || suppressed.covers(f) {
			continue
		}
		out = append(out, f)
	}
	slices.SortStableFunc(out, func(a, b Finding) int {
		return cmp.Or(cmp.Compare(a.Span.Start, b.Span.Start), cmp.Compare(a.Code, b.Code), cmp.Compare(a.Span.End, b.Span.End))
	})
	return slices.CompactFunc(out, func(a, b Finding) bool { return a.Code == b.Code && a.Span == b.Span })
}

var foreignFamily = family{codes: map[Code]meta{1071: {severity: SeverityError}}}

var foreignInterpreters = []string{"awk", "csh", "expect", "fish", "perl", "python", "ruby", "tcsh"}

func foreignInterpreter(text string) (string, bool) {
	name, ok := dialect.Interpreter(text)
	if !ok {
		return "", false
	}
	return name, slices.ContainsFunc(foreignInterpreters, func(prefix string) bool { return strings.HasPrefix(name, prefix) })
}
