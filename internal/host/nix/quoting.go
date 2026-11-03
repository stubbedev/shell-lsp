package nix

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/projection"
)

const maxSafeHops = 8

var (
	storeRoots = []string{"pkgs", "self", "inputs", "final", "prev", "stdenv"}
	safeNames  = []string{"runtimeShell"}
	safeCalls  = []string{"getExe", "getExe'", "getBin", "getDev", "getLib", "getOutput", "makeBinPath", "makeLibraryPath", "makeSearchPath", "placeholder", "callPackage", "mkDerivation", "derivation", "symlinkJoin", "buildEnv", "escapeShellArg", "escapeShellArgs"}
)

func UnescapedAntiquotationsCheck() string {
	info, known := lint.Describe(lint.CodeNixUnescapedAntiquotation)
	if !known {
		panic("nix: " + lint.CodeNixUnescapedAntiquotation.String() + " is not in the catalog")
	}
	return info.Optional
}

func UnescapedAntiquotationFindings(text string) []lint.Finding {
	tree := syntax.Parse(text)
	if len(tree.Errors) > 0 {
		return nil
	}
	f := &file{src: text, tree: tree, table: scope.Resolve(tree)}
	f.discover()
	f.discoverWrapped()
	kept := slices.DeleteFunc(unsuppressed(tree, f.quotingRisks()), func(finding lint.Finding) bool {
		return finding.Code != lint.CodeNixUnescapedAntiquotation
	})
	lint.SortHostFindings(kept)
	return kept
}

func (f *file) quotingRisks() []lint.Finding {
	var risky []*syntax.Interpolation
	for _, s := range f.found {
		if s.hosted != nil {
			continue
		}
		f.eachHole(s.str, func(hole *syntax.Interpolation) {
			if !f.followedByPath(hole) && !f.safe(hole.Value, 0) {
				risky = append(risky, hole)
			}
		})
	}
	if len(risky) == 0 {
		return nil
	}
	m, _ := f.projection()
	words := m.ArgumentWords()
	out := make([]lint.Finding, 0, len(risky))
	for _, hole := range risky {
		span := hole.Span()
		if at, _ := m.Offset(projection.HostOffset(span.Start)); inWord(words, int(at)) {
			out = append(out, lint.HostFinding(lint.CodeNixUnescapedAntiquotation, span, fmt.Sprintf("`%s` becomes part of a command's argument as it is, so a space or quote in its value changes the command; use `lib.escapeShellArg` unless the value cannot hold one", oneLine(f.src[span.Start:span.End]))))
		}
	}
	return out
}

func (f *file) followedByPath(hole *syntax.Interpolation) bool {
	end := hole.Span().End
	return end < len(f.src) && f.src[end] == '/'
}

func inWord(words []parser.Span, at int) bool {
	i := sort.Search(len(words), func(i int) bool { return words[i].End > at })
	return i < len(words) && words[i].Start <= at
}

func oneLine(text string) string {
	first, rest, multi := strings.Cut(text, "\n")
	if multi && rest != "" {
		return first + " …"
	}
	return first
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
		return slices.Contains(safeNames, f.libraryName(e)) || f.safeBinding(e, hops)
	case *syntax.Select:
		root, plain := syntax.Unparenthesized(e.Target).(*syntax.Ident)
		return plain && e.Default == nil && !definedHere(f.table, e) && slices.Contains(storeRoots, root.Name)
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
	name := f.callName(e.Fn)
	last := e.Args[len(e.Args)-1]
	switch {
	case slices.Contains(safeCalls, name), knownWriter(name):
		return true
	case name == "toString", name == "optionalString":
		return f.safe(last, hops+1)
	case name == "concatMapStrings" || name == "concatMapStringsSep" || name == "concatStringsSep" || name == "map":
		return slices.ContainsFunc(e.Args, f.escapes)
	}
	return f.safeResult(e, hops)
}

func (f *file) safeResult(e *syntax.Apply, hops int) bool {
	id, plain := syntax.Unparenthesized(e.Fn).(*syntax.Ident)
	if !plain {
		return false
	}
	ref, ok := f.table.Of(id)
	if !ok || ref.Kind != scope.Definition || len(ref.Def.Bindings) != 1 {
		return false
	}
	a, assigned := ref.Def.Bindings[0].(*syntax.Assign)
	if !assigned || len(a.Path) != 1 {
		return false
	}
	body := syntax.Unparenthesized(a.Value)
	for range e.Args {
		lambda, isLambda := body.(*syntax.Lambda)
		if !isLambda {
			return false
		}
		body = syntax.Unparenthesized(lambda.Body)
	}
	return f.safe(body, hops+1)
}

func (f *file) escapes(e syntax.Expr) bool {
	switch e := syntax.Unparenthesized(e).(type) {
	case *syntax.Ident, *syntax.Select:
		return slices.Contains(shellEscapes, f.libraryName(e))
	case *syntax.Apply:
		return f.callName(e.Fn) == "map" && len(e.Args) > 0 && f.escapes(e.Args[0])
	}
	return false
}
