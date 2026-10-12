package docstore

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/projection"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func openDoc(t *testing.T, text string) *Store {
	t.Helper()

	store := NewStore()
	store.Open(lsp.TextDocumentItem{URI: "file:///t.sh", LanguageID: "shellscript", Version: 1, Text: text})
	return store
}

func mustDoc(t *testing.T, store *Store) *Document {
	t.Helper()

	doc, ok := store.View().Document("file:///t.sh")
	if !ok {
		t.Fatal("document not found")
	}
	return doc
}

func TestEquivalentFileURIsNameOneDocument(t *testing.T) {
	t.Parallel()

	const opened, spelled = "file:///home/u/a%2Bb.sh", "file:///home/u/./a+b.sh"
	store := NewStore()
	store.Open(lsp.TextDocumentItem{URI: opened, Version: 1, Text: "a\n"})
	err := store.Change(spelled, 2, []lsp.TextDocumentContentChangeEvent{{Text: "b\n"}})
	if err != nil {
		t.Fatalf("Change(%q) error = %v", spelled, err)
	}
	doc, ok := store.View().Document(spelled)
	if !ok || doc.URI != opened || doc.Text() != "b\n" {
		t.Fatalf("Document(%q) = %+v, %v; want the document opened as %q with the change applied", spelled, doc, ok, opened)
	}
	store.Close(spelled)
	if _, ok := store.View().Document(opened); ok {
		t.Errorf("Document(%q) still open after Close(%q)", opened, spelled)
	}
}

func TestNormalizeStripsBOMAndCRLF(t *testing.T) {
	t.Parallel()

	got := NormalizeCanonical("\uFEFFa\r\nb\rc\n")
	if got != "a\nb\nc\n" {
		t.Errorf("NormalizeCanonical() = %q", got)
	}
	if NormalizeCanonical("plain") != "plain" {
		t.Error("NormalizeCanonical() changed plain text")
	}
}

func TestByteOrderMarkIsRemembered(t *testing.T) {
	t.Parallel()

	store := openDoc(t, "\ufeffecho hi\n")
	if doc := mustDoc(t, store); !doc.ByteOrderMark() || doc.Text() != "echo hi\n" {
		t.Fatalf("ByteOrderMark() = %t, Text() = %q", doc.ByteOrderMark(), doc.Text())
	}
	if err := store.Change("file:///t.sh", 2, []lsp.TextDocumentContentChangeEvent{{Text: "echo bye\n"}}); err != nil {
		t.Fatalf("Change() error = %v", err)
	}
	if mustDoc(t, store).ByteOrderMark() {
		t.Error("ByteOrderMark() stayed true after the whole text was replaced without one")
	}
}

func TestOpenNormalizesContent(t *testing.T) {
	t.Parallel()

	store := openDoc(t, "\ufeff#!/bin/bash\r\necho hi\r\n")
	if got := mustDoc(t, store).Text(); got != "#!/bin/bash\necho hi\n" {
		t.Errorf("Text() = %q", got)
	}
}

func TestCarriageReturnsFollowEdits(t *testing.T) {
	t.Parallel()

	store := openDoc(t, "a\r\nb\rc\nd")
	if got := mustDoc(t, store).CarriageReturns(); !slices.Equal(got, []int{1, 3}) {
		t.Fatalf("CarriageReturns() = %v, want the ends of the CRLF and CR lines", got)
	}
	err := store.Change("file:///t.sh", 2, []lsp.TextDocumentContentChangeEvent{{
		Range: &lsp.Range{Start: lsp.Position{Line: 1, Character: 1}, End: lsp.Position{Line: 2, Character: 0}},
		Text:  "\n",
	}})
	if err != nil {
		t.Fatalf("Change() error = %v", err)
	}
	if got := mustDoc(t, store).CarriageReturns(); !slices.Equal(got, []int{1}) {
		t.Errorf("CarriageReturns() after replacing the CR = %v, want only the CRLF", got)
	}
}

func TestLineCount(t *testing.T) {
	t.Parallel()

	store := openDoc(t, "a\nbb\nccc")
	if got := mustDoc(t, store).LineCount(); got != 3 {
		t.Errorf("LineCount() = %d, want 3", got)
	}
}

func TestLineBounds(t *testing.T) {
	t.Parallel()

	doc := mustDoc(t, openDoc(t, "a\nbb\nccc"))
	for line, want := range map[int][2]int{0: {0, 1}, 1: {2, 4}, 2: {5, 8}, 3: {8, 8}, -1: {8, 8}} {
		if start, end := doc.LineBounds(line); start != want[0] || end != want[1] {
			t.Errorf("LineBounds(%d) = %d, %d, want %v", line, start, end, want)
		}
	}
}

func TestOffsetToPositionRoundTrip(t *testing.T) {
	t.Parallel()

	text := "plain ascii\nhéllo wörld\n日本語テキスト\nemoji 😀 tail\n"
	store := openDoc(t, text)
	doc := mustDoc(t, store)

	encodings := []Encoding{EncodingUTF8, EncodingUTF16, EncodingUTF32}
	for _, enc := range encodings {
		for offset := 0; offset <= len(text); {
			pos := doc.OffsetToPosition(offset, enc)
			if back := doc.PositionToOffset(pos, enc); back != offset {
				t.Fatalf("%s: offset %d -> position %+v -> %d", enc, offset, pos, back)
			}
			if offset == len(text) {
				break
			}
			_, size := utf8.DecodeRuneInString(text[offset:])
			offset += size
		}
	}
}

func TestPositionToOffsetClamps(t *testing.T) {
	t.Parallel()

	store := openDoc(t, "ab\ncd\n")
	doc := mustDoc(t, store)

	tests := []struct {
		pos    lsp.Position
		offset int
	}{
		{pos: lsp.Position{Line: 99, Character: 0}, offset: 6},
		{pos: lsp.Position{Line: 1, Character: 99}, offset: 5},
		{pos: lsp.Position{Line: 0, Character: 1}, offset: 1},
	}
	for _, tc := range tests {
		if got := doc.PositionToOffset(tc.pos, EncodingUTF16); got != tc.offset {
			t.Errorf("PositionToOffset(%+v) = %d, want %d", tc.pos, got, tc.offset)
		}
	}
}

func TestUTF16SurrogateSnapsToRuneStart(t *testing.T) {
	t.Parallel()

	store := openDoc(t, "a😀b")
	doc := mustDoc(t, store)

	tests := []struct {
		character uint32
		offset    int
	}{
		{character: 0, offset: 0},
		{character: 1, offset: 1},
		{character: 2, offset: 1},
		{character: 3, offset: 5},
		{character: 4, offset: 6},
	}
	for _, tc := range tests {
		pos := lsp.Position{Line: 0, Character: tc.character}
		if got := doc.PositionToOffset(pos, EncodingUTF16); got != tc.offset {
			t.Errorf("character %d -> offset %d, want %d", tc.character, got, tc.offset)
		}
	}
	pos := doc.OffsetToPosition(5, EncodingUTF16)
	if pos.Character != 3 {
		t.Errorf("offset 5 -> character %d, want 3", pos.Character)
	}
}

func TestChangeFullReplacesContent(t *testing.T) {
	t.Parallel()

	store := openDoc(t, "old content")
	if err := store.Change("file:///t.sh", 2, []lsp.TextDocumentContentChangeEvent{{Text: "new\r\ncontent"}}); err != nil {
		t.Fatalf("Change() error = %v", err)
	}
	if got := mustDoc(t, store).Text(); got != "new\ncontent" {
		t.Errorf("Text() = %q", got)
	}
	if mustDoc(t, store).Version != 2 {
		t.Errorf("Version = %d, want 2", mustDoc(t, store).Version)
	}
}

func TestChangeIncremental(t *testing.T) {
	t.Parallel()

	store := openDoc(t, "hello world\n")
	err := store.Change("file:///t.sh", 2, []lsp.TextDocumentContentChangeEvent{{
		Range: &lsp.Range{
			Start: lsp.Position{Line: 0, Character: 6},
			End:   lsp.Position{Line: 0, Character: 11},
		},
		Text: "there",
	}})
	if err != nil {
		t.Fatalf("Change() error = %v", err)
	}
	if got := mustDoc(t, store).Text(); got != "hello there\n" {
		t.Errorf("Text() = %q", got)
	}
}

func TestChangeAcrossLines(t *testing.T) {
	t.Parallel()

	store := openDoc(t, "keep\nremove\nkeep\n")
	err := store.Change("file:///t.sh", 2, []lsp.TextDocumentContentChangeEvent{{
		Range: &lsp.Range{
			Start: lsp.Position{Line: 1, Character: 0},
			End:   lsp.Position{Line: 2, Character: 0},
		},
		Text: "",
	}})
	if err != nil {
		t.Fatalf("Change() error = %v", err)
	}
	if got := mustDoc(t, store).Text(); got != "keep\nkeep\n" {
		t.Errorf("Text() = %q", got)
	}
}

func TestChangeRejectsStaleVersion(t *testing.T) {
	t.Parallel()

	store := openDoc(t, "x")
	if err := store.Change("file:///t.sh", 1, []lsp.TextDocumentContentChangeEvent{{Text: "y"}}); err == nil {
		t.Error("same version accepted, want rejection")
	}
	if err := store.Change("file:///t.sh", 0, []lsp.TextDocumentContentChangeEvent{{Text: "y"}}); err == nil {
		t.Error("older version accepted, want rejection")
	}
	if got := mustDoc(t, store).Text(); got != "x" {
		t.Errorf("stale change mutated content: %q", got)
	}
}

func TestChangeUnopenedDocument(t *testing.T) {
	t.Parallel()

	store := NewStore()
	err := store.Change("file:///missing.sh", 1, []lsp.TextDocumentContentChangeEvent{{Text: "y"}})
	if err == nil {
		t.Error("change to unopened document accepted, want rejection")
	}
}

func TestCloseRemovesDocument(t *testing.T) {
	t.Parallel()

	store := openDoc(t, "x")
	store.Close("file:///t.sh")
	if _, ok := store.View().Document("file:///t.sh"); ok {
		t.Error("document still present after Close()")
	}
}

func TestSnapshotsAreStableAcrossChanges(t *testing.T) {
	t.Parallel()

	store := openDoc(t, "first")
	before := mustDoc(t, store)
	if err := store.Change("file:///t.sh", 2, []lsp.TextDocumentContentChangeEvent{{Text: "second"}}); err != nil {
		t.Fatalf("Change() error = %v", err)
	}
	if before.Text() != "first" {
		t.Errorf("old snapshot mutated: %q", before.Text())
	}
	if mustDoc(t, store).Text() != "second" {
		t.Errorf("new snapshot = %q", mustDoc(t, store).Text())
	}
}

func modelOffset(text string, pos lsp.Position) int {
	lines := strings.SplitAfter(text, "\n")
	line := int(pos.Line)
	if line >= len(lines) {
		return len(text)
	}
	seg := strings.TrimSuffix(lines[line], "\n")
	start := lineStartByte(lines, line)
	var units uint32
	for i, r := range seg {
		size := uint32(1)
		if r > 0xFFFF {
			size = 2
		}
		if units+size > pos.Character {
			return start + i
		}
		units += size
	}
	return start + len(seg)
}

func lineStartByte(lines []string, line int) int {
	n := 0
	for i := range line {
		n += len(lines[i])
	}
	return n
}

func TestRandomChangeSequencesMatchPlainTextModel(t *testing.T) {
	t.Parallel()

	alphabets := [][]rune{[]rune("aé😀中\nx "), []rune("abcdefgh\n x=$é")}
	for seed := range 200 {
		rng := rand.New(rand.NewPCG(uint64(seed), 0xDECAFBAD))
		runes := alphabets[seed%len(alphabets)]
		text := randomText(rng, runes, 1+rng.IntN(40))
		store := openDoc(t, text)
		model := NormalizeCanonical(text)
		version := int32(1)

		for range 1 + rng.IntN(12) {
			modelLines := strings.SplitAfter(model, "\n")
			line := rng.IntN(len(modelLines))
			seg := strings.TrimSuffix(modelLines[line], "\n")
			var maxChar uint32
			var units uint32
			for _, r := range seg {
				if r > 0xFFFF {
					units += 2
				} else {
					units++
				}
				maxChar = units
			}
			startChar := rng.IntN(int(maxChar) + 1)
			endChar := startChar + rng.IntN(int(maxChar)+1-startChar)
			endLine := line
			if rng.IntN(3) == 0 {
				endLine = line + rng.IntN(len(modelLines)-line)
				endChar = rng.IntN(int(maxChar) + 1)
			}

			start := lsp.Position{Line: uint32(line), Character: uint32(startChar)}
			end := lsp.Position{Line: uint32(endLine), Character: uint32(endChar)}
			insert := randomText(rng, runes, rng.IntN(5))

			model = applyToModel(model, start, end, insert)

			version++
			err := store.Change("file:///t.sh", version, []lsp.TextDocumentContentChangeEvent{{
				Range: &lsp.Range{Start: start, End: end},
				Text:  insert,
			}})
			if err != nil {
				t.Fatalf("seed %d: Change() error = %v", seed, err)
			}
			doc := mustDoc(t, store)
			if got := doc.Text(); got != model {
				t.Fatalf("seed %d after version %d: store = %q, model = %q", seed, version, got, model)
			}
			fresh := NewDocument("file:///t.sh", "", version, model)
			if !slices.Equal(doc.lineStart, fresh.lineStart) {
				t.Fatalf("seed %d after version %d: line starts %v, want %v", seed, version, doc.lineStart, fresh.lineStart)
			}
			if doc.nonASCII != fresh.nonASCII {
				t.Fatalf("seed %d after version %d: %d non-ASCII bytes tracked, want %d", seed, version, doc.nonASCII, fresh.nonASCII)
			}
			assertConversionsMatchDecoding(t, doc)
		}
	}
}

func assertConversionsMatchDecoding(t *testing.T, doc *Document) {
	t.Helper()
	decoding := *doc
	decoding.nonASCII = -1
	for _, enc := range []Encoding{EncodingUTF8, EncodingUTF16, EncodingUTF32} {
		for offset := 0; offset <= len(doc.text); offset++ {
			if got, want := doc.OffsetToPosition(offset, enc), decoding.OffsetToPosition(offset, enc); got != want {
				t.Fatalf("%q: OffsetToPosition(%d, %v) = %+v, want %+v", doc.text, offset, enc, got, want)
			}
		}
		for line := range uint32(len(doc.lineStart) + 1) {
			for char := range uint32(12) {
				pos := lsp.Position{Line: line, Character: char}
				if got, want := doc.PositionToOffset(pos, enc), decoding.PositionToOffset(pos, enc); got != want {
					t.Fatalf("%q: PositionToOffset(%+v, %v) = %d, want %d", doc.text, pos, enc, got, want)
				}
			}
		}
	}
}

func applyToModel(model string, start, end lsp.Position, insert string) string {
	s := modelOffset(model, start)
	e := modelOffset(model, end)
	if e < s {
		s, e = e, s
	}
	return model[:s] + NormalizeCanonical(insert) + model[e:]
}

func randomText(rng *rand.Rand, alphabet []rune, n int) string {
	var b strings.Builder
	for range n {
		b.WriteRune(alphabet[rng.IntN(len(alphabet))])
	}
	return b.String()
}

func TestProjectedDocumentSpeaksInHostPositions(t *testing.T) {
	t.Parallel()

	host := NewDocument("file:///w/Makefile", "makefile", 3, "x:\n\techo $$é $(CC)\n")
	b := projection.NewBuilder(host.Text())
	b.Region(func(_ string, _, _ int, text string) (string, bool) { return strings.ReplaceAll(text, "$", "$$"), true })
	b.Opaque(4, 4, "(\n")
	b.Verbatim(4, 9)
	b.Escape(9, 11, "$")
	b.Verbatim(11, 14)
	b.Opaque(14, 19, "$$")
	b.Opaque(19, 19, "\n)\n")
	doc := Project(host, b.Map())

	if doc.Text() != "(\necho $é $$\n)\n" || !doc.Projected() || doc.Source() != host || doc.Version != 3 || doc.URI != host.URI {
		t.Fatalf("projection = %q, %+v", doc.Text(), doc)
	}
	dollar := strings.Index(doc.Text(), "$é")
	for _, enc := range []Encoding{EncodingUTF8, EncodingUTF16} {
		if got := doc.OffsetToPosition(dollar, enc); got != (lsp.Position{Line: 1, Character: 6}) {
			t.Errorf("%v: the $ of $é is at %+v", enc, got)
		}
	}
	if got := doc.RangeFor(dollar, dollar+len("$é"), EncodingUTF16); got.End != (lsp.Position{Line: 1, Character: 9}) {
		t.Errorf("$é ends at %+v in UTF-16", got.End)
	}
	if offset, inside := doc.Locate(lsp.Position{Line: 1, Character: 2}, EncodingUTF16); !inside || offset != 3 {
		t.Errorf("Locate in echo = %d, %v", offset, inside)
	}
	if _, inside := doc.Locate(lsp.Position{Line: 0, Character: 1}, EncodingUTF16); inside {
		t.Error("the rule line is inside the shell")
	}
	edits, ok := doc.TextEdits([]textedit.Edit{{Start: dollar, End: dollar + len("$é"), NewText: `"$é"`}}, EncodingUTF16)
	want := []lsp.TextEdit{{Range: lsp.Range{Start: lsp.Position{Line: 1, Character: 6}, End: lsp.Position{Line: 1, Character: 9}}, NewText: `"$$é"`}}
	if !ok || !slices.Equal(edits, want) {
		t.Errorf("TextEdits = %+v, %v, want %+v", edits, ok, want)
	}
	if _, ok := doc.TextEdits([]textedit.Edit{{Start: 0, End: 0, NewText: "#!/bin/sh\n"}}, EncodingUTF16); ok {
		t.Error("an edit before the first recipe was written into the Makefile")
	}
	if !doc.Synthetic(0) || doc.Synthetic(dollar) {
		t.Errorf("Synthetic(0) = %v, Synthetic(%d) = %v", doc.Synthetic(0), dollar, doc.Synthetic(dollar))
	}
	at := doc.Positions(EncodingUTF16)
	rule := lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 0, Character: 2}}
	if span, inShell := at.Span(rule); inShell {
		t.Errorf("the rule line maps to %+v in the shell", span)
	}
	across := lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 1, Character: 3}}
	if span, inShell := at.Span(across); !inShell || span != (parser.Span{Start: 0, End: 4}) {
		t.Errorf("a range from the rule into the recipe = %+v, %v, want the recipe part", span, inShell)
	}
	if label := EncodingUTF8.Units("größe w"); label != 9 || EncodingUTF16.Units("größe w") != 7 || EncodingUTF16.Units("😀") != 2 {
		t.Errorf("units of größe w = %d in UTF-8", label)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("PositionToOffset on a projected document hid whether the position is in the shell")
			}
		}()
		doc.PositionToOffset(lsp.Position{Line: 1, Character: 2}, EncodingUTF16)
	}()

	defer func() {
		if recover() == nil {
			t.Error("Project accepted a map built from other text")
		}
	}()
	Project(NewDocument("file:///w/other", "", 1, "other"), b.Map())
}
