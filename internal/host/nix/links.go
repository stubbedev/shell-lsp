package nix

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

const (
	DirectoryEntry  = "default.nix"
	importName      = "import"
	callPackageName = "callPackage"
	importsAttr     = "imports"
)

type PathLiteral struct {
	Span   model.Span
	Path   string
	Loaded bool
}

func (d *Document) PathLiterals() []PathLiteral {
	loaded := map[*syntax.Path]bool{}
	mark := func(e syntax.Expr) {
		if p, ok := syntax.Unparenthesized(e).(*syntax.Path); ok {
			loaded[p] = true
		}
	}
	var out []PathLiteral
	d.inspect(func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Apply:
			if name := calleeName(n.Fn); name == importName || name == callPackageName {
				mark(n.Args[0])
			}
		case *syntax.Assign:
			if list, ok := syntax.Unparenthesized(n.Value).(*syntax.List); ok && lastIdent(n.Path) == importsAttr {
				for _, item := range list.Items {
					mark(item)
				}
			}
		case *syntax.Path:
			if local(n) {
				out = append(out, PathLiteral{Span: n.Span(), Path: d.text(n.Span()), Loaded: loaded[n]})
			}
		}
		return true
	})
	return out
}

func (d *Document) FlakeLinks() []FlakeLink {
	var out []FlakeLink
	d.inspect(func(n syntax.Node) bool {
		a, assigned := n.(*syntax.Assign)
		if !assigned || lastIdent(a.Path) != flakeURLAttr {
			return true
		}
		if str, literal := syntax.Unparenthesized(a.Value).(*syntax.String); literal {
			if target, known := forgePage(literalOf(str)); known {
				out = append(out, FlakeLink{Span: str.Content, Target: target})
			}
		}
		return true
	})
	return out
}

const flakeURLAttr = "url"

type FlakeLink struct {
	Span   model.Span
	Target string
}

var forges = map[string]string{"github": "https://github.com/", "gitlab": "https://gitlab.com/", "sourcehut": "https://git.sr.ht/"}

func forgePage(ref string) (string, bool) {
	scheme, rest, found := strings.Cut(ref, ":")
	base, known := forges[scheme]
	if !found || !known {
		return "", false
	}
	rest, _, _ = strings.Cut(rest, "?")
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	page := base + parts[0] + "/" + parts[1]
	if len(parts) == 3 && parts[2] != "" && scheme != "sourcehut" {
		page += "/tree/" + parts[2]
	}
	return page, true
}

func local(p *syntax.Path) bool {
	if p.Kind != syntax.RelativePath && p.Kind != syntax.AbsolutePath {
		return false
	}
	return !slices.ContainsFunc(p.Parts, func(part syntax.Part) bool {
		_, interpolated := part.(*syntax.Interpolation)
		return interpolated
	})
}

func loadable(p *syntax.Path) bool {
	if p.Kind == syntax.HomePath {
		return !slices.ContainsFunc(p.Parts, func(part syntax.Part) bool {
			_, interpolated := part.(*syntax.Interpolation)
			return interpolated
		})
	}
	return local(p)
}

type Prose struct {
	Span      model.Span
	Continues bool
}

func (d *Document) Prose() []Prose {
	var out []Prose
	for _, c := range d.tree.Comments {
		out = append(out, Prose{Span: c.Span})
	}
	d.inspect(func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.URI:
			out = append(out, Prose{Span: n.Span()})
		case *syntax.String:
			for i, part := range n.Parts {
				if _, literal := part.(*syntax.Literal); !literal {
					continue
				}
				continues := false
				if i+1 < len(n.Parts) {
					_, continues = n.Parts[i+1].(*syntax.Interpolation)
				}
				out = append(out, Prose{Span: part.Span(), Continues: continues})
			}
		}
		return true
	})
	slices.SortStableFunc(out, func(a, b Prose) int { return cmp.Compare(a.Span.Start, b.Span.Start) })
	return out
}
