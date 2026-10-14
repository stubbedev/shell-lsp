package lint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/glob"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/textline"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const (
	codePatternNeedsOption = nativeBase + 2016
	codeUnsetCaptures      = nativeBase + 2017
	codeReplacementMatch   = nativeBase + 2018
	codeBadPattern         = nativeBase + 2019
	codeRegexEscape        = nativeBase + 2022
)

var patternsFamily = family{
	codes: map[Code]meta{
		codePatternNeedsOption: {severity: SeverityWarning},
		codeUnsetCaptures:      {severity: SeverityWarning},
		codeReplacementMatch:   {severity: SeverityWarning},
		codeBadPattern:         {severity: SeverityError},
		codeRegexEscape:        {severity: SeverityWarning},
	},
	nodes:  []nodeRule{onNode(checkReplacementAmpersand), onNode(checkRegexEscapes)},
	finish: []func(*pass){checkPatterns, checkUnsetCaptures},
}

func (p *pass) patternSites() []glob.Site {
	if p.sites == nil {
		sites := glob.For(p.tree, p.shellOptions()).All()
		p.sites = &sites
	}
	return *p.sites
}

func checkPatterns(p *pass) {
	for _, site := range p.patternSites() {
		p.checkBadPattern(site)
		if site.Options.Dynamic {
			continue
		}
		reported := map[glob.Option]bool{}
		for _, need := range site.Pattern.Needs {
			if !reported[need.Option] {
				reported[need.Option] = p.checkPatternNeed(site, need)
			}
		}
	}
}

func (p *pass) checkBadPattern(site glob.Site) {
	if p.dialect != dialect.Zsh {
		return
	}
	for _, e := range site.Pattern.Errors {
		p.report(codeBadPattern, e.Span, "zsh rejects this pattern with \"bad pattern\": "+e.Message+".")
	}
}

func (p *pass) script() bool { return strings.HasPrefix(p.tree.Text(), "#!") }

func (p *pass) reportNeed(need glob.Need, message, standalone, sourced, enable string) {
	severity := p.meta(codePatternNeedsOption).severity
	if p.script() {
		message += standalone
	} else {
		message += sourced
		severity = SeverityInfo
	}
	p.reportWithSeverity(codePatternNeedsOption, severity, need.Span, message, p.enableOptionFix(need, enable)...)
}

func parsedWithCommand(role glob.Role) bool {
	return role == glob.RoleFilenames || role == glob.RoleCase
}

func (p *pass) checkPatternNeed(site glob.Site, need glob.Need) bool {
	text := p.spanText(need.Span)
	enable := need.Option.Enable(p.dialect)
	if need.Option == glob.ExtGlob && parsedWithCommand(site.Role) {
		return p.checkParsedExtglob(need, text, enable)
	}
	if need.Option.On(site.Options) {
		return false
	}
	switch need.Option {
	case glob.ExtendedGlob:
		p.reportNeed(need, fmt.Sprintf("%s is an extended glob operator, and this file never runs `%s`", text, enable),
			", so zsh matches it literally.",
			"; unless whatever sources or autoloads this file has turned the option on, zsh matches it literally.", enable)
	case glob.ExtGlob:
		p.reportNeed(need, fmt.Sprintf("%s is an extended glob, and this file never runs `%s`", text, enable),
			", so bash matches it literally.",
			"; unless the script that sources this file has turned the option on, bash matches it literally.", enable)
	case glob.GlobStar:
		p.reportNeed(need, fmt.Sprintf("%s only descends into subdirectories with `%s`", text, enable),
			", which this file never runs; without it, it matches like *.",
			", which this file never runs; unless the script that sources it does, it matches like *.", enable)
	}
	return true
}

func (p *pass) checkParsedExtglob(need glob.Need, text, enable string) bool {
	top, ok := p.topLevelStatement(need.Span.Start)
	if !ok {
		return false
	}
	enablers := p.shellOptions().EnabledBy(need.Option.String(), need.Span.Start)
	if slices.ContainsFunc(enablers, func(c parser.Command) bool { return c.Span.End <= top.Start }) {
		return false
	}
	if slices.ContainsFunc(enablers, func(c parser.Command) bool { return c.Span.Start >= top.Start && c.Span.End <= need.Span.Start }) {
		p.report(codePatternNeedsOption, need.Span, fmt.Sprintf("bash reads %s together with the `%s` before it, on one line or in one function, so the option is not on yet when bash parses the extended glob; enable it on a line of its own above.", text, enable), p.enableOptionFix(need, enable)...)
		return true
	}
	p.reportNeed(need, fmt.Sprintf("%s is an extended glob, which bash rejects as a syntax error unless `%s` ran before it read this line", text, enable),
		".",
		", here or in the script that sources this file.", enable)
	return true
}

func (p *pass) topLevelStatement(offset int) (parser.Span, bool) {
	unit, previousEnd := -1, -1
	for _, stmt := range p.tree.WrittenStmts() {
		span, ok := p.span(stmt)
		if !ok {
			continue
		}
		if unit < 0 || p.tree.Line(span.Start) > p.tree.Line(previousEnd) {
			unit = span.Start
		}
		previousEnd = max(span.End-1, span.Start)
		if span.Start <= offset && offset < span.End {
			return parser.Span{Start: unit, End: span.End}, true
		}
	}
	return parser.Span{}, false
}

func (p *pass) enableOptionFix(need glob.Need, enable string) []Fix {
	at, ok := p.topLevelStatement(need.Span.Start)
	if p.dialect == dialect.Zsh {
		if fn, inFunction := p.tree.EnclosingFunction(p.tree.ScopeAt(need.Span.Start)); inFunction {
			at, ok = p.firstStatementOf(fn.Decl)
			enable = "setopt local_options " + need.Option.String()
		}
	}
	if !ok || p.tree.Synthetic(at.Start) || !textline.BlankBefore(p.tree.Text(), at.Start) {
		return nil
	}
	start := textline.Start(p.tree.Text(), at.Start)
	indent := textline.Indent(p.tree.Text(), at.Start)
	return []Fix{reviewFix("Run `"+enable+"` first", textedit.Edit{Start: start, End: start, NewText: indent + enable + "\n"})}
}

func (p *pass) firstStatementOf(fn *syntax.FuncDecl) (parser.Span, bool) {
	if fn.Body == nil {
		return parser.Span{}, false
	}
	block, ok := fn.Body.Cmd.(*syntax.Block)
	if !ok || len(block.Stmts) == 0 {
		return parser.Span{}, false
	}
	return p.span(block.Stmts[0])
}

var captureProducers = map[dialect.Dialect]map[string]func(glob.Pattern) bool{
	dialect.Zsh: {
		"match": capturesGroups, "mbegin": capturesGroups, "mend": capturesGroups,
		"MATCH": capturesWhole, "MBEGIN": capturesWhole, "MEND": capturesWhole,
	},
	dialect.Bash: {"BASH_REMATCH": nil},
}

func capturesGroups(p glob.Pattern) bool { return p.Captures > 0 }

func capturesWhole(p glob.Pattern) bool { return p.SetsMatch }

func checkUnsetCaptures(p *pass) {
	names, ok := captureProducers[p.dialect]
	if !ok {
		return
	}
	var unset []parser.Reference
	for _, ref := range p.tree.References {
		if _, watched := names[ref.Name]; watched && !p.tree.Defines(ref.Name) {
			unset = append(unset, ref)
		}
	}
	if len(unset) == 0 || p.matchesRegex() {
		return
	}
	reported := map[string]bool{}
	for _, ref := range unset {
		produced := names[ref.Name]
		if reported[ref.Name] || produced != nil && slices.ContainsFunc(p.patternSites(), func(s glob.Site) bool { return produced(s.Pattern) }) {
			continue
		}
		reported[ref.Name] = true
		p.report(codeUnsetCaptures, ref.Span, captureMessage(p.dialect, ref.Name))
	}
}

func captureMessage(d dialect.Dialect, name string) string {
	switch {
	case d == dialect.Bash:
		return "$" + name + " is only set by [[ string =~ regex ]], and nothing in this file runs one."
	case strings.ToUpper(name) == name:
		return "$" + name + " is only set by a pattern with (#m) or by =~, and nothing in this file uses either."
	}
	return "$" + name + " is only set by a pattern with (#b) or by =~, and nothing in this file uses either."
}

func (p *pass) matchesRegex() bool {
	found := false
	parser.Walk(p.tree.File, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.BinaryTest:
			found = found || n.Op == syntax.TsReMatch
		case *syntax.ModuleTest:
			found = found || slices.ContainsFunc(n.Args, func(w *syntax.Word) bool { return parser.LiteralIs(w, "-pcre-match") })
		case *syntax.CallExpr:
			name, _ := parser.CallName(n)
			found = found || name == "pcre_match" || name == "regexp-replace"
		}
		return !found
	})
	return found
}

func checkReplacementAmpersand(p *pass, pe *syntax.ParamExp) {
	if p.dialect != dialect.Bash || pe.Repl == nil || pe.Repl.With == nil || p.optionsAt(pe).NoPatsubAmp {
		return
	}
	for _, part := range pe.Repl.With.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			continue
		}
		span, ok := p.span(lit)
		if !ok {
			continue
		}
		for i := 0; i < len(lit.Value); i++ {
			switch lit.Value[i] {
			case '\\':
				i++
			case '&':
				at := span.Start + i
				p.report(codeReplacementMatch, parser.Span{Start: at, End: at + 1},
					"Since bash 5.2, an unquoted & in the replacement stands for the matched text; write \\& for a literal &, which every bash reads the same way.",
					reviewFix("Escape the &", textedit.Edit{Start: at, End: at, NewText: `\`}))
			}
		}
	}
}

var digitClasses = map[byte]string{'d': "[0-9]", 'D': "[^0-9]"}

const gnuEscapes = "wWsSbB"

func checkRegexEscapes(p *pass, test *syntax.BinaryTest) {
	w, ok := test.Y.(*syntax.Word)
	if test.Op != syntax.TsReMatch || !ok || p.dialect != dialect.Bash && p.dialect != dialect.Zsh {
		return
	}
	literalQuotes := p.dialect.Features().QuotedRegexLiteral
	for _, part := range w.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			p.checkRegexEscapesIn(part, part.Value, 0, false, literalQuotes)
		case *syntax.SglQuoted:
			if !part.Dollar {
				p.checkRegexEscapesIn(part, part.Value, len("'"), true, literalQuotes)
			}
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				if lit, ok := inner.(*syntax.Lit); ok {
					p.checkRegexEscapesIn(lit, lit.Value, 0, true, literalQuotes)
				}
			}
		}
	}
}

func (p *pass) checkRegexEscapesIn(node syntax.Node, text string, skip int, quoted, literalQuotes bool) {
	span, ok := p.span(node)
	if !ok {
		return
	}
	for i := 0; i+1 < len(text); i++ {
		if text[i] != '\\' {
			continue
		}
		c := text[i+1]
		at := parser.Span{Start: span.Start + skip + i, End: span.Start + skip + i + 2}
		escape := "\\" + string(c)
		switch class, digits := digitClasses[c]; {
		case digits:
			matches := "the letter `" + string(c) + "`"
			if quoted && literalQuotes {
				matches = "a backslash followed by `" + string(c) + "`"
			}
			message := fmt.Sprintf("POSIX regular expressions have no `%s`, so =~ matches it as %s; write `%s` for a digit.", escape, matches, class)
			if c == 'D' {
				message = fmt.Sprintf("POSIX regular expressions have no `%s`, so =~ matches it as %s; write `%s` for a non-digit.", escape, matches, class)
			}
			var fixes []Fix
			if !quoted || !literalQuotes {
				fixes = []Fix{reviewFix("Replace "+escape+" with "+class, textedit.Edit{Start: at.Start, End: at.End, NewText: class})}
			}
			p.report(codeRegexEscape, at, message, fixes...)
		case literalQuotes && !quoted && strings.IndexByte(gnuEscapes, c) >= 0:
			p.report(codeRegexEscape, at, fmt.Sprintf("In bash a backslash in the =~ word quotes the next character, so `%s` matches the letter `%c`; to use the `%s` escape, keep the regular expression in a variable and match against `$re`.", escape, c, escape))
		}
		i++
	}
}
