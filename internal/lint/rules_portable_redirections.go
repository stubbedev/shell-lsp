package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var portableRedirectionsFamily = family{
	codes: map[Code]meta{
		3011: {shells: strictShells, summary: "Here-strings with <<< are undefined in POSIX sh."},
		3020: {shells: posixOrDash, summary: "Redirecting both streams with &> is undefined in POSIX sh and dash."},
		3021: {shells: strictShells, summary: "Redirecting both streams to a file with >& is undefined in POSIX sh."},
		3022: {shells: strictShells, summary: "Named file descriptors like {fd}> are undefined in POSIX sh."},
		3023: {shells: strictShells, summary: "File descriptor numbers above 9 are undefined in POSIX sh."},
		3025: {shells: strictShells, summary: "Network pseudo-devices such as /dev/tcp are undefined in POSIX sh."},
		3031: {shells: strictShells, summary: "Redirecting to or from a glob is undefined in POSIX sh."},
	},
	rules: []rule{{on: nodes(&syntax.Redirect{}), node: checkPortableRedirection}},
}

var networkDevices = []string{"/dev/tcp/", "/dev/udp/"}

func checkPortableRedirection(p *pass, n syntax.Node) {
	r, ok := n.(*syntax.Redirect)
	if !ok || !strictShells.Has(p.dialect) {
		return
	}
	span, ok := p.span(r)
	if !ok {
		return
	}
	if r.N != nil {
		checkDescriptorPrefix(p, r.N, span)
	}
	switch r.Op {
	case syntax.WordHdoc:
		p.nonPortable(portabilityIssue{code: 3011, feature: "The here-string <<<", alternative: "a here-document or printf ... |"}, posSpan(r.OpPos, len("<<<")))
		return
	case syntax.RdrAll:
		op, end := parser.Offset(r.OpPos), parser.Offset(r.Word.End())
		p.nonPortable(portabilityIssue{code: 3020, feature: "Redirecting both streams with &>", alternative: ">file 2>&1"}, span,
			safeFix("Replace &> with > and 2>&1", textedit.Edit{Start: op, End: op + len("&>"), NewText: ">"}, textedit.Edit{Start: end, End: end, NewText: " 2>&1"}))
	case syntax.DplOut:
		if r.N == nil && !isDescriptorTarget(r.Word) {
			p.nonPortable(portabilityIssue{code: 3021, feature: "Redirecting both streams to a file with >&", alternative: ">file 2>&1"}, span)
		}
		return
	case syntax.RdrOut, syntax.AppOut, syntax.RdrIn, syntax.RdrInOut, syntax.RdrClob, syntax.AppAll:
	default:
		return
	}
	target := parser.LiteralChars(r.Word)
	if device, ok := networkDevice(target); ok {
		p.nonPortable(portabilityIssue{code: 3025, feature: "The network pseudo-device " + device}, span)
		return
	}
	if isGlob(r.Word) {
		p.nonPortable(portabilityIssue{code: 3031, feature: "Redirecting to or from a glob"}, span)
	}
}

func checkDescriptorPrefix(p *pass, n *syntax.Lit, span parser.Span) {
	switch {
	case strings.HasPrefix(n.Value, "{"):
		p.nonPortable(portabilityIssue{code: 3022, feature: "The named descriptor " + n.Value}, span)
	case len(n.Value) > 1:
		p.nonPortable(portabilityIssue{code: 3023, feature: "The descriptor number " + n.Value + " (above 9)"}, span)
	}
}

func isDescriptorTarget(w *syntax.Word) bool {
	return strings.Trim(strings.TrimSuffix(parser.LiteralChars(w), "-"), "0123456789") == ""
}

func networkDevice(target string) (string, bool) {
	for _, prefix := range networkDevices {
		if strings.HasPrefix(target, prefix) {
			return strings.TrimSuffix(prefix, "/"), true
		}
	}
	return "", false
}
