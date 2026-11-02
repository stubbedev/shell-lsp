package nix

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

const maxSafeHops = 8

var (
	storeRoots = []string{"pkgs", "self", "inputs", "final", "prev", "stdenv"}
	safeNames  = []string{"runtimeShell"}
	safeCalls  = []string{"getExe", "getExe'", "getBin", "getDev", "getLib", "getOutput", "makeBinPath", "makeLibraryPath", "makeSearchPath", "placeholder", "callPackage", "mkDerivation", "derivation", "symlinkJoin", "buildEnv", "escapeShellArg", "escapeShellArgs"}
)

func (f *file) quotingRisks(found []shellString) []lint.Finding {
	var out []lint.Finding
	for _, s := range found {
		if s.hosted != nil {
			continue
		}
		bodies := f.heredocBodies(s.str, heredocDelimiter)
		f.eachHole(s.str, func(hole *syntax.Interpolation) {
			span := hole.Span()
			if strings.HasPrefix(f.src[span.End:], "/") || f.safe(hole.Value, 0) || f.inComment(s.str, span.Start) || slices.ContainsFunc(bodies, func(b model.Span) bool { return b.Start <= span.Start && span.End <= b.End }) {
				return
			}
			out = append(out, lint.HostFinding(lint.CodeNixUnescapedAntiquotation, span, fmt.Sprintf("`%s` reaches the shell as it is, so a space or quote in its value changes the command; use `lib.escapeShellArg` unless the value cannot hold one", oneLine(f.src[span.Start:span.End]))))
		})
	}
	return out
}

func oneLine(text string) string {
	first, rest, multi := strings.Cut(text, "\n")
	if multi && rest != "" {
		return first + " …"
	}
	return first
}

func (f *file) inComment(str *syntax.String, at int) bool {
	start := max(strings.LastIndexByte(f.src[:at], '\n')+1, str.Content.Start)
	quote := byte(0)
	for i := start; i < at; i++ {
		c := f.src[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '#' && (i == start || f.src[i-1] == ' ' || f.src[i-1] == '\t'):
			return true
		}
	}
	return false
}

func (f *file) safe(e syntax.Expr, hops int) bool {
	if hops > maxSafeHops {
		return false
	}
	switch e := syntax.Unparenthesized(e).(type) {
	case *syntax.Int, *syntax.Float, *syntax.Path:
		return true
	case *syntax.String:
		return f.safeString(e, hops)
	case *syntax.Ident:
		return slices.Contains(safeNames, e.Name) || f.safeBinding(e, hops)
	case *syntax.Select:
		root, plain := syntax.Unparenthesized(e.Target).(*syntax.Ident)
		return plain && e.Default == nil && slices.Contains(storeRoots, root.Name)
	case *syntax.If:
		return f.safe(e.Then, hops+1) && f.safe(e.Else, hops+1)
	case *syntax.Binary:
		return e.Op == syntax.OpAdd && f.safe(e.Left, hops+1) && f.safe(e.Right, hops+1)
	case *syntax.Apply:
		return f.safeCall(e, hops)
	}
	return false
}

func (f *file) safeBinding(id *syntax.Ident, hops int) bool {
	ref, ok := f.table.Of(id)
	if !ok || ref.Kind != scope.Definition || len(ref.Def.Bindings) != 1 {
		return false
	}
	a, assigned := ref.Def.Bindings[0].(*syntax.Assign)
	return assigned && len(a.Path) == 1 && f.safe(a.Value, hops+1)
}

func (f *file) safeString(str *syntax.String, hops int) bool {
	for _, part := range str.Parts {
		switch part := part.(type) {
		case *syntax.Literal:
			if strings.ContainsFunc(part.Text, func(r rune) bool {
				return !isASCIIAlnum(r) && !strings.ContainsRune("._/:=@%+,-", r)
			}) {
				return false
			}
		case *syntax.Interpolation:
			if !f.safe(part.Value, hops+1) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (f *file) safeCall(e *syntax.Apply, hops int) bool {
	name := calleeName(syntax.Unparenthesized(e.Fn))
	last := e.Args[len(e.Args)-1]
	switch {
	case slices.Contains(safeCalls, name), knownWriter(name):
		return true
	case name == "toString", name == "optionalString":
		return f.safe(last, hops+1)
	case name == "concatMapStrings" || name == "concatMapStringsSep" || name == "concatStringsSep" || name == "map":
		return slices.ContainsFunc(e.Args, func(arg syntax.Expr) bool { return escapes(arg) })
	}
	return false
}

func escapes(e syntax.Expr) bool {
	switch e := syntax.Unparenthesized(e).(type) {
	case *syntax.Ident, *syntax.Select:
		return slices.Contains(shellEscapes, calleeName(e))
	case *syntax.Apply:
		return calleeName(syntax.Unparenthesized(e.Fn)) == "map" && len(e.Args) > 0 && escapes(e.Args[0])
	}
	return false
}
