package docstore

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/lsp"
)

var fuzzInsertions = []string{"a", "é", "😀", "中", " ", "\n", "\r\n", "\r", "\uFEFF"}

type clientLine struct{ start, end int }

func clientLines(text string) []clientLine {
	var lines []clientLine
	start := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\r':
			lines = append(lines, clientLine{start, i})
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			start = i + 1
		case '\n':
			lines = append(lines, clientLine{start, i})
			start = i + 1
		}
	}
	return append(lines, clientLine{start, len(text)})
}

func clientOffset(text string, pos lsp.Position, enc Encoding) int {
	lines := clientLines(text)
	if int(pos.Line) >= len(lines) {
		return len(text)
	}
	line := lines[pos.Line]
	offset := line.start
	var units uint32
	for offset < line.end {
		r, size := utf8.DecodeRuneInString(text[offset:line.end])
		next := units + codeUnits(r, size, enc)
		if next > pos.Character {
			break
		}
		units = next
		offset += size
	}
	return offset
}

func FuzzChangesMatchClientText(f *testing.F) {
	f.Add("echo hi\r\nx=1\n", []byte{0, 2, 1, 0, 2, 0, 1})
	f.Add("a😀b\nc", []byte{0, 2, 0, 3, 1, 2, 7, 1, 0, 9, 9, 1, 5})
	f.Add("", []byte{5, 5, 5, 5, 3, 7, 6, 4})
	f.Add("a\rb\nc", []byte{1, 1, 0, 1, 0, 1, 5, 1, 0, 9, 0, 9, 1, 7})
	f.Fuzz(func(t *testing.T, initial string, script []byte) {
		if !utf8.ValidString(initial) {
			return
		}
		client := strings.TrimPrefix(initial, "\uFEFF")
		doc := NewDocument("file:///f.sh", "shellscript", 1, initial)
		for edit := 0; len(script) >= 6 && edit < 32; edit++ {
			enc := Encoding(script[0] % 3)
			start := lsp.Position{Line: uint32(script[1] % 8), Character: uint32(script[2] % 12)}
			end := lsp.Position{Line: uint32(script[3] % 8), Character: uint32(script[4] % 12)}
			count := int(script[5] % 4)
			script = script[6:]
			var insert strings.Builder
			for ; count > 0 && len(script) > 0; count-- {
				insert.WriteString(fuzzInsertions[int(script[0])%len(fuzzInsertions)])
				script = script[1:]
			}
			from, to := clientOffset(client, start, enc), clientOffset(client, end, enc)
			if to < from {
				from, to = to, from
			}
			client = client[:from] + insert.String() + client[to:]
			doc.applyChanges([]lsp.TextDocumentContentChangeEvent{{Range: &lsp.Range{Start: start, End: end}, Text: insert.String()}}, enc)
			want := strings.ReplaceAll(strings.ReplaceAll(client, "\r\n", "\n"), "\r", "\n")
			if doc.Text() != want {
				t.Fatalf("after replacing %+v-%+v (%v) with %q: document %q, client %q", start, end, enc, insert.String(), doc.Text(), want)
			}
			if doc.LineCount() != len(clientLines(client)) {
				t.Fatalf("document has %d lines, client %d", doc.LineCount(), len(clientLines(client)))
			}
		}
	})
}
