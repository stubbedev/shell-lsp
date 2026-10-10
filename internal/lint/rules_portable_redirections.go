package lint

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

var portableRedirectionsFamily = family{
	codes: map[Code]meta{
		3011: {severity: SeverityWarning},
		3020: {severity: SeverityWarning},
		3021: {severity: SeverityWarning},
		3022: {severity: SeverityWarning},
		3023: {severity: SeverityWarning},
		3025: {severity: SeverityWarning},
		3031: {severity: SeverityWarning},
	},
	rules: []rule{{node: checkPortableRedirection}},
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
		p.nonPortable(portabilityIssue{code: 3011, shells: strictShells, feature: "The here-string <<<", alternative: "a here-document or printf ... |"}, posSpan(r.OpPos, len("<<<")))
		return
	case syntax.RdrAll:
		p.nonPortable(portabilityIssue{code: 3020, shells: posixOrDash, feature: "Redirecting both streams with &>", alternative: ">file 2>&1"}, span)
	case syntax.DplOut:
		if r.N == nil && !isDescriptorTarget(r.Word) {
			p.nonPortable(portabilityIssue{code: 3021, shells: strictShells, feature: "Redirecting both streams to a file with >&", alternative: ">file 2>&1"}, span)
		}
		return
	case syntax.RdrOut, syntax.AppOut, syntax.RdrIn, syntax.RdrInOut, syntax.RdrClob, syntax.AppAll:
	default:
		return
	}
	target := parser.LiteralChars(r.Word)
	if device, ok := networkDevice(target); ok {
		p.nonPortable(portabilityIssue{code: 3025, shells: strictShells, feature: "The network pseudo-device " + device}, span)
		return
	}
	if isGlob(r.Word) {
		p.nonPortable(portabilityIssue{code: 3031, shells: strictShells, feature: "Redirecting to or from a glob"}, span)
	}
}

func checkDescriptorPrefix(p *pass, n *syntax.Lit, span parser.Span) {
	switch {
	case strings.HasPrefix(n.Value, "{"):
		p.nonPortable(portabilityIssue{code: 3022, shells: strictShells, feature: "The named descriptor " + n.Value}, span)
	case len(n.Value) > 1:
		p.nonPortable(portabilityIssue{code: 3023, shells: strictShells, feature: "The descriptor number " + n.Value + " (above 9)"}, span)
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
