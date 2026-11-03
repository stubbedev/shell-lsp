package hostnav

import (
	"cmp"
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/stubbedev/shell-lsp/internal/assist"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/hadolint"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/projection"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const embeddedRoot = ".embedded"

type embeddedText struct {
	nix.HostedText
	file  *workspace.File
	group *Group
}

type embeddedLoader map[string]*workspace.File

func (l embeddedLoader) Load(path string) (*workspace.File, error) {
	file, known := l[path]
	if !known {
		return nil, fmt.Errorf("loading the embedded file %s: %w", path, fs.ErrNotExist)
	}
	return file, nil
}

func (l embeddedLoader) LoadAs(path string, language host.Language) (*workspace.File, error) {
	file, err := l.Load(path)
	if err != nil {
		return nil, err
	}
	if file.Native.Language != language {
		return nil, fmt.Errorf("loading the embedded file %s as %s: %w", path, language, fs.ErrNotExist)
	}
	return file, nil
}

func (embeddedLoader) NixFiles(string) ([]string, error) { return nil, nil }

func (embeddedLoader) NixIncludes(string) ([]model.Include, int64, error) { return nil, 0, nil }

func (l embeddedLoader) Present(path string) bool {
	_, known := l[path]
	return known
}

func (embeddedLoader) ScanCut() bool { return false }

func embeddedTexts(file *workspace.File) []*embeddedText {
	if file.Native.Nix().HostedCount() == 0 {
		return nil
	}
	return workspace.Memo(file, "hostnav.embedded", 0, func() []*embeddedText { return loadEmbedded(file) })
}

func loadEmbedded(file *workspace.File) []*embeddedText {
	var out []*embeddedText
	loader := embeddedLoader{}
	for _, hosted := range file.Native.Nix().Hosted() {
		language, known := host.FromName(hosted.Name)
		if !known || language == host.Nix {
			continue
		}
		path := filepath.Join(cmp.Or(file.Path, string(filepath.Separator))+embeddedRoot, embeddedHome(hosted), hosted.Name)
		doc := docstore.NewDocument(fileuri.FromPath(path), language.LanguageID(), file.Doc.Version, hosted.Via.Text())
		text := &embeddedText{HostedText: hosted, file: workspace.Parse(path, doc, nil, nil)}
		loader[path] = text.file
		out = append(out, text)
	}
	for _, text := range out {
		h, hosted := Of(text.file)
		if !hosted {
			panic("hostnav: an embedded " + text.Name + " was not analyzed as its language")
		}
		group, err := Build(context.Background(), h, Env{Loader: loader})
		if err != nil {
			panic("hostnav: an embedded file needs no import graph, yet building it failed: " + err.Error())
		}
		text.group = group
	}
	return out
}

func embeddedHome(hosted nix.HostedText) string {
	if hosted.Placed {
		return hosted.Family
	}
	return strconv.Itoa(hosted.Content.Start)
}

func embeddedAt(file *workspace.File, offset int) (*embeddedText, int, bool) {
	texts := embeddedTexts(file)
	i := slices.IndexFunc(texts, func(t *embeddedText) bool { return t.Content.Start <= offset && offset <= t.Content.End })
	if i < 0 {
		return nil, 0, false
	}
	inner, inside := texts[i].Via.Offset(projection.HostOffset(offset))
	return texts[i], int(inner), inside
}

type embeddedSymbol struct {
	text   *embeddedText
	symbol Symbol
}

func (g *Group) embeddedAtOffset(offset int) (*embeddedText, int, bool) {
	if g.Language != host.Nix {
		return nil, 0, false
	}
	t, inner, inside := embeddedAt(g.Self, offset)
	return t, inner, t != nil && inside
}

func (g *Group) embeddedSymbol(offset int) (Symbol, bool) {
	t, inner, inside := g.embeddedAtOffset(offset)
	if !inside {
		return Symbol{}, false
	}
	sym, found := t.group.At(inner)
	if !found {
		return Symbol{}, false
	}
	return Symbol{Name: sym.Name, Kind: sym.Kind, Span: t.placed(sym.Span), embedded: &embeddedSymbol{text: t, symbol: sym}}, true
}

func (t *embeddedText) placedHover(h Hover) Hover {
	return Hover{Markdown: nix.WithoutStandIns(h.Markdown), Span: t.placed(h.Span)}
}

func (g *Group) placedLocations(t *embeddedText, locations []Location) []Location {
	out := make([]Location, len(locations))
	for i, l := range locations {
		out[i] = Location{File: g.Self, Span: t.placed(l.Span)}
	}
	return out
}

func (g *Group) embeddedOccurrences(sym Symbol) []Occurrence {
	t := sym.embedded.text
	occs := t.group.Occurrences(sym.embedded.symbol)
	out := make([]Occurrence, len(occs))
	for i, o := range occs {
		o.File, o.Span = g.Self, t.placed(o.Span)
		out[i] = o
	}
	return out
}

func (g *Group) renameEmbedded(sym Symbol, newName string) ([]Occurrence, error) {
	t := sym.embedded.text
	occs, err := t.group.Rename(sym.embedded.symbol, newName)
	if err != nil {
		return nil, err
	}
	edits := make([]textedit.Edit, 0, len(occs))
	out := make([]Occurrence, len(occs))
	for i, o := range occs {
		edits = append(edits, o.Edits(newName)...)
		o.File, o.Span = g.Self, t.placed(o.Span)
		if o.Rewrite != nil {
			return nil, fmt.Errorf("%w: renaming `%s` rewrites more than its name, which embedded text cannot carry", navigation.ErrNotRenamable, sym.Name)
		}
		out[i] = o
	}
	if _, mapped := t.Via.Edits(edits); !mapped {
		return nil, fmt.Errorf("%w: `%s` is read through a Nix antiquotation or would change the string around it", navigation.ErrNotRenamable, sym.Name)
	}
	return out, nil
}

func (t *embeddedText) placed(span parser.Span) parser.Span {
	start, end := t.Via.HostSpan(span.Start, span.End)
	return parser.Span{Start: start, End: end}
}

func embeddedFixes(file *workspace.File, within parser.Span) []Fix {
	var out []Fix
	for _, t := range embeddedTexts(file) {
		out = append(out, t.derivedFixes(within)...)
	}
	return out
}

func (t *embeddedText) derivedFixes(within parser.Span) []Fix {
	var out []Fix
	for _, finding := range t.group.Findings() {
		if t.Via.TouchesStandIn(finding.Span.Start, finding.Span.End) {
			continue
		}
		placed := finding
		placed.Span, placed.Fixes = t.placed(finding.Span), nil
		if !touches(placed.Span, within) {
			continue
		}
		for _, fix := range t.group.derivedFixes(finding) {
			if edits, mapped := t.Via.Edits(fix.Edits); mapped && fix.Create == "" {
				out = append(out, Fix{Title: fix.Title, Finding: placed, Edits: edits, Preferred: fix.Preferred})
			}
		}
	}
	return out
}

func embeddedFindings(file *workspace.File, known []lint.Finding) []lint.Finding {
	var out []lint.Finding
	for _, t := range embeddedTexts(file) {
		missing := t.group.Language.Checks().Missing
		found := t.group.moduleFindings()
		if t.Placed {
			found = slices.DeleteFunc(found, func(f lint.Finding) bool { return f.Code == missing })
		}
		for _, finding := range t.Findings(found) {
			if !slices.ContainsFunc(known, func(k lint.Finding) bool { return k.Code == finding.Code && k.Span == finding.Span }) {
				out = append(out, finding)
			}
		}
	}
	return out
}

func EmbeddedComplete(file *workspace.File, offset int) ([]Item, bool) {
	t, inner, inside := embeddedAt(file, offset)
	if t == nil {
		return nil, false
	}
	if !inside {
		return nil, true
	}
	items := t.group.Complete(inner)
	for i := range items {
		items[i].Span = t.placed(items[i].Span)
	}
	return items, true
}

func (g *Group) embeddedSignature(offset int) (assist.Signature, bool, bool) {
	t, inner, inside := embeddedAt(g.Self, offset)
	if t == nil {
		return assist.Signature{}, false, false
	}
	if !inside {
		return assist.Signature{}, false, true
	}
	sig, ok := t.group.Signature(inner)
	if ok {
		return sig, true, true
	}
	shell, projected := t.file.Doc.Locate(t.file.Doc.Source().OffsetToPosition(inner, docstore.EncodingUTF16), docstore.EncodingUTF16)
	if !projected {
		return assist.Signature{}, false, true
	}
	sig, ok = t.group.ShellSignature(shell)
	return sig, ok, true
}

func embeddedDockerfiles(file *workspace.File, cfg hadolint.Config) []embeddedDockerfile {
	var out []embeddedDockerfile
	for _, t := range embeddedTexts(file) {
		if docker, ok := HadolintOf(t.file, cfg); ok && docker.dockerfile != nil {
			out = append(out, embeddedDockerfile{text: t, hadolint: docker})
		}
	}
	return out
}

type embeddedDockerfile struct {
	text     *embeddedText
	hadolint Hadolint
}

func (d embeddedDockerfile) findings() []lint.Finding {
	return d.text.Findings(d.hadolint.Host(d.hadolint.Findings()))
}

func (d embeddedDockerfile) holds(offset int) bool {
	return d.text.Content.Start <= offset && offset < d.text.Content.End
}

func (d embeddedDockerfile) suppress(f lint.Finding, at int) ([]lint.Finding, bool) {
	if !d.holds(at) {
		return nil, false
	}
	inner, _ := d.text.Via.Offset(projection.HostOffset(at))
	return d.hadolint.suppression.Apply([]lint.Finding{f}, func(int) int { return int(inner) }), true
}
