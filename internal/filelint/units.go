package filelint

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const keptRuns = 2

type unitKey [sha256.Size]byte

type rememberedReport struct {
	report  lint.Report
	sources uint64
	lastRun uint64
}

type Memory struct {
	mu        sync.Mutex
	run       uint64
	reports   map[unitKey]*rememberedReport
	sharedKey unitKey
	shared    *parser.Tree
}

func NewMemory() *Memory {
	return &Memory{reports: map[unitKey]*rememberedReport{}}
}

func (m *Memory) Check(file *workspace.File, env navigation.Env, s Settings) lint.Report {
	run := m.begin()
	var merged lint.Report
	for _, variant := range file.Variants() {
		merge(&merged, m.check(run, variant, env, s))
	}
	m.forgetBefore(run)
	return merged
}

func settingsKey(file *workspace.File, s Settings) string {
	opts := options(file, s)
	return fmt.Sprintf("%v|%v|%v|%v|%v", opts.Exclude, opts.Optional, opts.Profile, opts.NoExtendedAnalysis, opts.RCFile)
}

func (m *Memory) begin() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.run++
	return m.run
}

func (m *Memory) forgetBefore(run uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, remembered := range m.reports {
		if remembered.lastRun+keptRuns <= run {
			delete(m.reports, key)
		}
	}
}

func (m *Memory) check(run uint64, file *workspace.File, env navigation.Env, s Settings) lint.Report {
	shared, apart := file.Doc.Units(file.Scripts)
	if len(apart) == 0 {
		opts := options(file, s)
		opts.Sources = followed(file, env)
		return shown(file, lint.Run(file.Tree, navigation.Imports(file, env), opts))
	}
	c := unitCheck{memory: m, run: run, settings: settingsKey(file, s), file: file, env: env, lint: s}
	var merged lint.Report
	if len(shared) > 0 {
		c.shared = placeUnit(shared)
		c.sharedKey = c.keyOf(c.shared, unitKey{})
		merge(&merged, c.sharedReport())
	}
	for _, scripts := range apart {
		u := placeUnit(scripts)
		merge(&merged, c.report(u, c.keyOf(u, c.sharedKey), c.sharedImport()))
	}
	lint.SortFindings(merged.Findings)
	return shown(file, merged)
}

type unitCheck struct {
	memory    *Memory
	run       uint64
	settings  string
	file      *workspace.File
	env       navigation.Env
	lint      Settings
	shared    scriptUnit
	sharedKey unitKey
	buffer    []byte
}

func (c *unitCheck) sharedReport() lint.Report {
	key := c.sharedKey
	if c.allSourced() {
		return c.report(c.shared, key, nil)
	}
	key = sha256.Sum256(append(key[:], c.file.Doc.Text()...))
	return c.report(c.shared, key, func() []parser.Import { return []parser.Import{{Tree: c.file.Tree}} })
}

func (c *unitCheck) allSourced() bool {
	return !slices.ContainsFunc(c.shared.scripts, func(s parser.Span) bool { return !c.file.Doc.Sourced(s.Start) })
}

func (c *unitCheck) keyOf(u scriptUnit, neighbours unitKey) unitKey {
	doc := c.file.Doc
	buffer := append(c.buffer[:0], c.settings...)
	buffer = append(buffer, 0)
	buffer = append(buffer, byte(c.file.Tree.Dialect), byte(c.file.Tree.Flavor))
	buffer = append(buffer, neighbours[:]...)
	for _, script := range u.scripts {
		text := doc.Text()[script.Start:script.End]
		buffer = binary.AppendVarint(buffer, int64(len(text)))
		buffer = append(buffer, text...)
		buffer = doc.AppendLayout(buffer, script)
		if doc.Sourced(script.Start) {
			buffer = append(buffer, 1)
		}
		buffer = append(buffer, 0)
	}
	for _, offset := range doc.CarriageReturns() {
		if local, inside := u.local(offset); inside {
			buffer = binary.AppendVarint(buffer, int64(local))
		}
	}
	c.buffer = buffer
	return sha256.Sum256(buffer)
}

func (c *unitCheck) sharedImport() func() []parser.Import {
	if len(c.shared.scripts) == 0 {
		return nil
	}
	return func() []parser.Import { return []parser.Import{{Tree: c.sharedTree()}} }
}

func (c *unitCheck) report(u scriptUnit, key unitKey, neighbours func() []parser.Import) lint.Report {
	generation := c.env.Loader.Generation()
	if remembered, ok := c.memory.recall(key, c.run, generation); ok {
		return u.placed(remembered)
	}
	origin := &unitOrigin{doc: c.file.Doc, unit: u}
	tree := parser.ParseScripts(nil, u.text(c.file.Doc.Text()), u.localSpans(), c.file.Tree.Shell, origin)
	tree.Errors = u.localErrors(c.file.Tree.Errors)
	own := &workspace.File{Path: c.file.Path, URI: c.file.URI, Doc: c.file.Doc, Tree: tree, Detection: c.file.Detection, Native: c.file.Native}
	imports := navigation.Imports(own, c.env)
	if neighbours != nil {
		imports = append(imports, neighbours()...)
	}
	opts := options(own, c.lint)
	opts.Sources = followed(own, c.env)
	opts.Sourced = func(offset int) bool { return c.file.Doc.Sourced(u.start(offset)) }
	opts.ReadByHost = func(offset int, name string) bool { return c.file.Doc.ReadByHost(u.start(offset), name) }
	opts.Normalized = lint.Normalized{CarriageReturns: u.carriageReturns(c.file.Doc)}
	report := lint.Run(tree, imports, opts)
	if !origin.lineAsked.Load() {
		c.memory.remember(key, c.run, report, sourcesStamp(tree, generation))
	}
	return u.placed(report)
}

func sourcesStamp(tree *parser.Tree, generation uint64) uint64 {
	if slices.ContainsFunc(tree.Commands, parser.Command.IsSourceBuiltin) {
		return generation + 1
	}
	return 0
}

func (c *unitCheck) sharedTree() *parser.Tree {
	if tree, held := c.memory.sharedTree(c.sharedKey); held {
		return tree
	}
	tree := parser.ParseScripts(nil, c.shared.text(c.file.Doc.Text()), c.shared.localSpans(), c.file.Tree.Shell, nil)
	c.memory.keepSharedTree(c.sharedKey, tree)
	return tree
}

func (m *Memory) recall(key unitKey, run, generation uint64) (lint.Report, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	remembered, held := m.reports[key]
	if !held || remembered.sources != 0 && remembered.sources != generation+1 {
		return lint.Report{}, false
	}
	remembered.lastRun = run
	return remembered.report, true
}

func (m *Memory) remember(key unitKey, run uint64, report lint.Report, sources uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reports[key] = &rememberedReport{report: report, sources: sources, lastRun: run}
}

func (m *Memory) sharedTree(key unitKey) (*parser.Tree, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.shared, m.shared != nil && m.sharedKey == key
}

func (m *Memory) keepSharedTree(key unitKey, tree *parser.Tree) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sharedKey, m.shared = key, tree
}

type scriptUnit struct {
	scripts []parser.Span
	locals  []int
	size    int
}

func placeUnit(scripts []parser.Span) scriptUnit {
	u := scriptUnit{scripts: scripts, locals: make([]int, len(scripts))}
	for i, script := range scripts {
		u.locals[i] = u.size
		u.size += script.End - script.Start
	}
	return u
}

func (u scriptUnit) text(document string) string {
	if len(u.scripts) == 1 {
		return document[u.scripts[0].Start:u.scripts[0].End]
	}
	buffer := make([]byte, 0, u.size)
	for _, script := range u.scripts {
		buffer = append(buffer, document[script.Start:script.End]...)
	}
	return string(buffer)
}

func (u scriptUnit) localSpans() []parser.Span {
	out := make([]parser.Span, len(u.scripts))
	for i, script := range u.scripts {
		out[i] = parser.Span{Start: u.locals[i], End: u.locals[i] + script.End - script.Start}
	}
	return out
}

func (u scriptUnit) start(local int) int {
	i := max(sort.SearchInts(u.locals, local+1)-1, 0)
	return u.scripts[i].Start + local - u.locals[i]
}

func (u scriptUnit) end(local int) int {
	if local == 0 {
		return u.scripts[0].Start
	}
	return u.start(local-1) + 1
}

func (u scriptUnit) local(global int) (int, bool) {
	i := sort.Search(len(u.scripts), func(i int) bool { return u.scripts[i].End > global })
	if i == len(u.scripts) || global < u.scripts[i].Start {
		return 0, false
	}
	return u.locals[i] + global - u.scripts[i].Start, true
}

func (u scriptUnit) localErrors(errs []parser.Error) []parser.Error {
	var out []parser.Error
	for _, e := range errs {
		start, inside := u.local(e.Span.Start)
		if !inside {
			continue
		}
		e.Span = parser.Span{Start: start, End: start + e.Span.End - e.Span.Start}
		out = append(out, e)
	}
	return out
}

func (u scriptUnit) carriageReturns(doc *docstore.Document) []int {
	var out []int
	for _, offset := range doc.CarriageReturns() {
		if local, inside := u.local(offset); inside {
			out = append(out, local)
		}
	}
	return out
}

func (u scriptUnit) placedSpan(s parser.Span) parser.Span {
	return parser.Span{Start: u.start(s.Start), End: max(u.start(s.Start), u.end(s.End))}
}

func (u scriptUnit) placedFindings(findings []lint.Finding) []lint.Finding {
	if len(findings) == 0 {
		return nil
	}
	out := make([]lint.Finding, len(findings))
	for i, f := range findings {
		f.Span = u.placedSpan(f.Span)
		f.Fixes = u.placedFixes(f.Fixes)
		out[i] = f
	}
	return out
}

func (u scriptUnit) placedFixes(fixes []lint.Fix) []lint.Fix {
	if len(fixes) == 0 {
		return nil
	}
	out := make([]lint.Fix, len(fixes))
	for i, fix := range fixes {
		edits := make([]textedit.Edit, len(fix.Edits))
		for j, e := range fix.Edits {
			placed := u.placedSpan(parser.Span{Start: e.Start, End: e.End})
			edits[j] = textedit.Edit{Start: placed.Start, End: placed.End, NewText: e.NewText}
		}
		fix.Edits = edits
		out[i] = fix
	}
	return out
}

func (u scriptUnit) placed(report lint.Report) lint.Report {
	out := lint.Report{Findings: u.placedFindings(report.Findings), Suppressed: u.placedFindings(report.Suppressed)}
	for _, d := range report.Disabled {
		d.Span = u.placedSpan(d.Span)
		d.Suppressed = slices.Clone(d.Suppressed)
		out.Disabled = append(out.Disabled, d)
	}
	return out
}

type unitOrigin struct {
	doc       *docstore.Document
	unit      scriptUnit
	lineAsked atomic.Bool
}

func (o *unitOrigin) SourceLine(offset int) int {
	o.lineAsked.Store(true)
	return o.doc.SourceLine(o.unit.start(offset))
}

func (o *unitOrigin) Synthetic(offset int) bool { return o.doc.Synthetic(o.unit.start(offset)) }

func (o *unitOrigin) StandIn(offset int) bool { return o.doc.StandIn(o.unit.start(offset)) }

func (o *unitOrigin) ShellOf(offset int) int { return o.doc.ShellOf(o.unit.start(offset)) }
