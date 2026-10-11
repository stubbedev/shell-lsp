package assist

import (
	"cmp"
	"regexp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/completion"
	"github.com/stubbedev/shell-lsp/internal/hover"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type FunctionLookup func(name string) (*parser.Tree, parser.Function, bool)

type ParamLabel struct {
	Start, End int
	Doc        string
}

type Signature struct {
	Label  string
	Doc    string
	Params []ParamLabel
	Active int
}

func SignatureHelp(tree *parser.Tree, offset int, lookup FunctionLookup) (Signature, bool) {
	command, argIndex, ok := completion.CommandAt(tree, offset)
	if !ok {
		return Signature{}, false
	}
	if fnTree, fn, ok := lookup(command); ok {
		sig := hover.FunctionSignature(fnTree, fn)
		out := Signature{Label: sig.Label(), Doc: sig.Doc, Active: -1}
		pos := len(sig.Name)
		for _, p := range sig.Params {
			pos++
			out.Params = append(out.Params, ParamLabel{Start: pos, End: pos + len(p.Name), Doc: p.Doc})
			pos += len(p.Name)
		}
		if sig.Variadic {
			pos++
			out.Params = append(out.Params, ParamLabel{Start: pos, End: pos + len("[args...]")})
		}
		switch {
		case argIndex < len(sig.Params):
			out.Active = argIndex
		case sig.Variadic:
			out.Active = len(out.Params) - 1
		}
		return out, true
	}
	if entry, ok := shelldocs.Builtin(command); ok && entry.Dialects.Has(tree.Dialect) {
		return Signature{Label: entry.Synopsis, Doc: entry.Doc, Active: -1}, true
	}
	return Signature{}, false
}

type HintKind uint8

const (
	HintType HintKind = iota + 1
	HintParameter
	HintPath
)

type Hint struct {
	Offset  int
	Label   string
	Kind    HintKind
	Tooltip string
}

func InlayHints(file *workspace.File, within parser.Span, lookup FunctionLookup, env navigation.Env) []Hint {
	tree := file.Tree
	var hints []Hint
	inRange := func(offset int) bool { return within.Start <= offset && offset <= within.End }
	for _, def := range tree.Definitions {
		label := attributeLabel(def)
		if label != "" && inRange(def.NameSpan.End) {
			hints = append(hints, Hint{Offset: def.NameSpan.End, Label: label, Kind: HintType})
		}
	}
	type signatureKey struct {
		tree *parser.Tree
		name string
	}
	signatures := map[signatureKey]hover.Signature{}
	for _, cmd := range tree.Commands {
		if !inRange(cmd.Span.Start) && !inRange(cmd.Span.End) {
			continue
		}
		fnTree, fn, ok := lookup(cmd.Name)
		if !ok {
			continue
		}
		key := signatureKey{tree: fnTree, name: fn.Name}
		sig, seen := signatures[key]
		if !seen {
			sig = hover.FunctionSignature(fnTree, fn)
			signatures[key] = sig
		}
		for i, arg := range cmd.Args {
			if i >= len(sig.Params) {
				break
			}
			p := sig.Params[i]
			span, ok := tree.NodeSpan(arg)
			if !ok || strings.HasPrefix(p.Name, "$") || redundant(tree.Text()[span.Start:span.End], p.Name) {
				continue
			}
			hints = append(hints, Hint{Offset: span.Start, Label: p.Name + ":", Kind: HintParameter, Tooltip: p.Doc})
		}
	}
	for _, ref := range navigation.Sources(file, env) {
		if ref.Target == nil || !inRange(ref.Arg.End) {
			continue
		}
		if _, literal := parser.Literal(ref.Command.Args[0]); literal {
			continue
		}
		hints = append(hints, Hint{Offset: ref.Arg.End, Label: "→ " + navigation.RelativeName(file.Path, ref.Target.Path), Kind: HintPath, Tooltip: ref.Target.Path})
	}
	slices.SortStableFunc(hints, func(a, b Hint) int { return cmp.Compare(a.Offset, b.Offset) })
	return hints
}

func attributeLabel(def parser.Definition) string {
	switch {
	case def.Kind != parser.DefDeclare && def.Kind != parser.DefBuiltin:
		return ""
	case def.Attrs.Has(parser.AttrAssoc):
		return ": associative array"
	case def.Attrs.Has(parser.AttrArray):
		return ": array"
	case def.Attrs.Has(parser.AttrInteger):
		return ": integer"
	case def.Attrs.Has(parser.AttrNameref):
		return ": nameref"
	case def.Attrs.Has(parser.AttrReadonly):
		return ": readonly"
	}
	return ""
}

func redundant(arg, name string) bool {
	arg = strings.Trim(arg, `"'`)
	return arg == name || arg == "$"+name || arg == "${"+name+"}"
}

type Link struct {
	Span   parser.Span
	Target string
}

var urlPattern = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `]+`)

func DocumentLinks(file *workspace.File, env navigation.Env) []Link {
	var links []Link
	for _, ref := range navigation.Sources(file, env) {
		if ref.Target == nil {
			continue
		}
		links = append(links, Link{Span: ref.Arg, Target: ref.Target.URI})
		if ref.Directive != nil {
			links = append(links, Link{Span: *ref.Directive, Target: ref.Target.URI})
		}
	}
	for _, c := range file.Tree.Comments {
		text := file.Tree.Text()[c.Span.Start:c.Span.End]
		for _, m := range urlPattern.FindAllStringIndex(text, -1) {
			url := strings.TrimRight(text[m[0]:m[1]], ".,;:)]}!?")
			links = append(links, Link{Span: parser.Span{Start: c.Span.Start + m[0], End: c.Span.Start + m[0] + len(url)}, Target: url})
		}
	}
	return links
}
