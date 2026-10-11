package lint

import (
	"regexp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/expand"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var fileOpsFamily = family{
	codes: map[Code]meta{
		2114: {severity: SeverityWarning},
		2115: {severity: SeverityWarning},
		2174: {severity: SeverityWarning},
		2186: {severity: SeverityWarning, tag: TagDeprecated},
		2224: {severity: SeverityError},
		2225: {severity: SeverityError},
		2226: {severity: SeverityWarning},
		2230: {severity: SeverityInfo, optional: "deprecate-which"},
		2253: {severity: SeverityWarning},
		2267: {severity: SeverityInfo, tag: TagDeprecated},
	},
	commands: []commandRule{
		{match: matchBase, names: []string{"rm"}, check: checkRecursiveRemoval},
		{match: matchBase, names: []string{"mkdir"}, check: checkMkdirMode},
		{match: matchBase, names: []string{"mv"}, check: checkMissingDestination(2224, "")},
		{match: matchBase, names: []string{"cp"}, check: checkMissingDestination(2225, "")},
		{match: matchBase, names: []string{"ln"}, check: checkMissingDestination(2226, " Give . explicitly to link into the current directory.")},
		{match: matchBase, names: []string{"chmod"}, check: checkChmodLowerR},
		{match: matchBase, names: []string{"tempfile"}, check: checkTempfile},
		{match: matchBase, names: []string{"which"}, check: checkWhich},
		{match: matchBase, names: []string{"xargs"}, check: checkXargsReplace},
	},
}

const maxBraceAlternatives = 1000

var importantPaths = func() map[string]bool {
	out := map[string]bool{}
	for _, base := range []string{"", "/bin", "/etc", "/home", "/mnt", "/usr", "/usr/share", "/usr/local", "/var", "/lib", "/dev", "/media", "/boot", "/lib64", "/usr/bin"} {
		for _, suffix := range []string{"", "/", "/*", "/*/*"} {
			if path := base + suffix; path != "" {
				out[path] = true
			}
		}
	}
	return out
}()

func checkRecursiveRemoval(p *pass, c command) {
	flags := parseFlags(c.args, "")
	if !flags.has('r', "recursive") && !flags.has('R', "") {
		return
	}
	for _, arg := range flags.operands {
		checkRemovedPath(p, arg)
	}
}

func (p *pass) emptyGuardFix(arg *syntax.Word) []Fix {
	var edits []textedit.Edit
	syntax.Walk(arg, func(n syntax.Node) bool {
		pe, ok := n.(*syntax.ParamExp)
		if !ok {
			return true
		}
		if span, ok := p.span(pe); ok && pe.Param != nil && parser.IsName(pe.Param.Value) && parser.IsPlainParam(pe) {
			edits = append(edits, textedit.Edit{Start: span.Start, End: span.End, NewText: "${" + pe.Param.Value + ":?}"})
		}
		return false
	})
	if len(edits) == 0 {
		return nil
	}
	return []Fix{reviewFix("Abort when the variable is empty with ${var:?}", edits...)}
}

func checkRemovedPath(p *pass, arg *syntax.Word) {
	split := *arg
	syntax.SplitBraces(&split)
	count := 0
	for alternative, err := range expand.BracesSeq(nil, &split) {
		if count++; err != nil || count > maxBraceAlternatives {
			return
		}
		text, literal := parser.Text(alternative, "")
		path := normalizePath(text)
		switch {
		case !importantPaths[path]:
		case literal:
			p.reportNode(2114, arg, "This removes "+path+" recursively; make sure that is intended, or guard the path.")
			return
		case !guardsEmpty(alternative):
			p.reportNode(2115, arg, "If the expansions here are empty, this removes "+path+"; use ${var:?} so an empty variable aborts instead.", p.emptyGuardFix(arg)...)
			return
		}
	}
}

func guardsEmpty(w *syntax.Word) bool {
	guarded := false
	for _, part := range w.Parts {
		parser.Walk(part, func(n syntax.Node) bool {
			pe, ok := n.(*syntax.ParamExp)
			if ok && pe.Exp != nil {
				switch pe.Exp.Op {
				case syntax.ErrorUnsetOrNull, syntax.DefaultUnsetOrNull, syntax.AssignUnsetOrNull:
					guarded = true
				}
			}
			return !guarded
		})
	}
	return guarded
}

var (
	repeatedSlashes = regexp.MustCompile(`/+`)
	repeatedStars   = regexp.MustCompile(`\*+`)
)

func normalizePath(path string) string {
	path = repeatedStars.ReplaceAllString(repeatedSlashes.ReplaceAllString(path, "/"), "*")
	if trimmed := strings.TrimRight(path, "/"); trimmed != "" {
		return trimmed
	}
	return path
}

var singleRelativeName = regexp.MustCompile(`^(\.\.?/)+[^/]+$`)

func checkMkdirMode(p *pass, c command) {
	flags := parseFlags(c.args, "m")
	if !flags.has('p', "parents") || !flags.has('m', "mode") || len(c.args) < 2 {
		return
	}
	nested := false
	for _, arg := range c.args[1:] {
		lit, ok := parser.Literal(arg)
		if !ok || (strings.Contains(lit, "/") && !singleRelativeName.MatchString(lit)) {
			nested = true
			break
		}
	}
	if !nested {
		return
	}
	for _, arg := range c.args {
		lit, _ := parser.Literal(arg)
		if strings.HasPrefix(lit, "--mode") || (isShortCluster(lit) && lit[0] == '-' && strings.Contains(lit, "m")) {
			p.reportNode(2174, arg, "With -p, -m only sets the mode of the deepest directory; the parents get the default mode. Create the parents separately or chmod them afterwards.")
			return
		}
	}
}

func checkMissingDestination(code Code, hint string) func(p *pass, c command) {
	return func(p *pass, c command) {
		flags := parseFlags(c.args, "")
		if len(flags.operands) != 1 || flags.has('t', "") || mayExpandToSeveralWords(flags.operands[0]) ||
			slices.ContainsFunc(flags.long, abbreviatesTargetDirectory) {
			return
		}
		p.reportNode(code, c.call, c.name+" is given only one path and no destination."+hint)
	}
}

func abbreviatesTargetDirectory(long string) bool {
	return long != "" && strings.HasPrefix("target-directory", long)
}

func checkChmodLowerR(p *pass, c command) {
	for _, arg := range c.args {
		if parser.LiteralIs(arg, "-r") {
			p.reportNode(2253, arg, "chmod -r removes read permission; use -R to change modes recursively.")
		}
	}
}

func checkTempfile(p *pass, c command) {
	p.reportNode(2186, c.word, "tempfile is deprecated and missing from many systems; use mktemp instead.")
}

func checkWhich(p *pass, c command) {
	var fixes []Fix
	span, ok := p.span(c.word)
	if ok && c.word.Lit() == "which" && len(c.args) > 0 && !slices.ContainsFunc(c.args, func(w *syntax.Word) bool { return strings.HasPrefix(roughText(w), "-") }) {
		fixes = append(fixes, reviewFix("Replace which with command -v", textedit.Edit{Start: span.Start, End: span.End, NewText: "command -v"}))
	}
	p.reportNode(2230, c.word, "which is not standard and behaves differently between systems; use command -v instead.", fixes...)
}

var xargsOptions = optionGrammar{short: "0oprtxa:d:R:S:J:L:l:n:P:s:e:E:i:I:", stopAtOperand: true}

func checkXargsReplace(p *pass, c command) {
	options, ok := xargsOptions.parse(c.args)
	if !ok {
		return
	}
	for _, opt := range options.options {
		if opt.name == "i" {
			p.reportNode(2267, opt.word, "xargs -i is deprecated; use -I {} instead.")
			return
		}
	}
}
