package docstore

import (
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/lsp"
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

func TestOpenNormalizesContent(t *testing.T) {
	t.Parallel()

	store := openDoc(t, "\ufeff#!/bin/bash\r\necho hi\r\n")
	if got := mustDoc(t, store).Text(); got != "#!/bin/bash\necho hi\n" {
		t.Errorf("Text() = %q", got)
	}
}

func TestLineCount(t *testing.T) {
	t.Parallel()

	store := openDoc(t, "a\nbb\nccc")
	if got := mustDoc(t, store).LineCount(); got != 3 {
		t.Errorf("LineCount() = %d, want 3", got)
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

	runes := []rune("aé😀中\nx ")
	for seed := range 200 {
		rng := rand.New(rand.NewPCG(uint64(seed), 0xDECAFBAD))
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

			start := lsp.Position{Line: uint32(line), Character: uint32(startChar)}
			end := lsp.Position{Line: uint32(line), Character: uint32(endChar)}
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
			if got := mustDoc(t, store).Text(); got != model {
				t.Fatalf("seed %d after version %d: store = %q, model = %q", seed, version, got, model)
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
