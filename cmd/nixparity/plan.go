package main

import (
	"cmp"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/regularfile"
	"github.com/stubbedev/shell-lsp/internal/scan"
)

const (
	nixExtension = ".nix"
	maxFileBytes = 4 << 20
	languageID   = "nix"
)

type settings struct {
	seed      uint64
	positions int
	fileLimit int
}

type document struct {
	rel string
	doc *docstore.Document
}

type query struct {
	feature  feature
	file     int
	position lsp.Position
}

type plan struct {
	root     string
	settings settings
	docs     []document
	skipped  []string
	queries  []query
}

func buildPlan(root string, s settings) (plan, error) {
	rels, err := listNixFiles(root)
	if err != nil {
		return plan{}, err
	}
	if len(rels) == 0 {
		return plan{}, fmt.Errorf("no %s files under %s", nixExtension, root)
	}
	p := plan{root: root, settings: s}
	for _, rel := range sampleFiles(s.seed, rels, s.fileLimit) {
		doc, usable, err := readDocument(root, rel)
		if err != nil {
			return plan{}, err
		}
		if !usable {
			p.skipped = append(p.skipped, rel)
			continue
		}
		p.docs = append(p.docs, document{rel: rel, doc: doc})
	}
	if len(p.docs) == 0 {
		return plan{}, fmt.Errorf("every sampled %s file under %s is over %d bytes or not UTF-8", nixExtension, root, maxFileBytes)
	}
	p.queries = planQueries(p.docs, s)
	return p, nil
}

func listNixFiles(root string) ([]string, error) {
	var rels []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("listing %s: %w", path, err)
		}
		if entry.IsDir() {
			if path != root && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || filepath.Ext(path) != nixExtension {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("naming %s within %s: %w", path, root, err)
		}
		rels = append(rels, filepath.ToSlash(rel))
		return nil
	})
	slices.Sort(rels)
	return rels, err
}

func sampleFiles(seed uint64, rels []string, limit int) []string {
	if len(rels) <= limit {
		return rels
	}
	rng := rand.New(rand.NewPCG(seed, 0)) //nolint:gosec
	picked := make([]string, 0, limit)
	for _, i := range rng.Perm(len(rels))[:limit] {
		picked = append(picked, rels[i])
	}
	slices.Sort(picked)
	return picked
}

func readDocument(root, rel string) (*docstore.Document, bool, error) {
	path := filepath.Join(root, filepath.FromSlash(rel))
	raw, _, err := regularfile.Read(path, maxFileBytes)
	switch {
	case errors.Is(err, regularfile.ErrTooLarge):
		return nil, false, nil
	case err != nil:
		return nil, false, fmt.Errorf("reading %s: %w", path, err)
	case !utf8.Valid(raw):
		return nil, false, nil
	}
	return docstore.NewDocument(fileuri.FromPath(path), languageID, 1, string(raw)), true, nil
}

func planQueries(docs []document, s settings) []query {
	var queries []query
	features := allFeatures()
	for file, d := range docs {
		picks := sampleTokens(s.seed, d.rel, nixTokens(d.doc.Text()), s.positions)
		for _, f := range features {
			if !f.spec().positional {
				queries = append(queries, query{feature: f, file: file})
				continue
			}
			for _, t := range picks {
				queries = append(queries, query{
					feature:  f,
					file:     file,
					position: d.doc.OffsetToPosition(f.probeOffset(t), docstore.EncodingUTF16),
				})
			}
		}
	}
	return queries
}

type token struct {
	start, end int
}

func sampleTokens(seed uint64, rel string, tokens []token, n int) []token {
	if len(tokens) <= n {
		return tokens
	}
	stream := fnv.New64a()
	_, _ = stream.Write([]byte(rel))
	rng := rand.New(rand.NewPCG(seed, stream.Sum64())) //nolint:gosec
	picked := make([]token, 0, n)
	for _, i := range rng.Perm(len(tokens))[:n] {
		picked = append(picked, tokens[i])
	}
	slices.SortFunc(picked, func(a, b token) int { return cmp.Compare(a.start, b.start) })
	return picked
}

func nixNameChar(c byte) bool { return scan.NameChar(c) || c == '\'' || c == '-' }

func nixKeyword(word string) bool {
	switch word {
	case "assert", "else", "if", "in", "inherit", "let", "or", "rec", "then", "with":
		return true
	}
	return false
}

func nixTokens(text string) []token {
	var tokens []token
	for i := 0; i < len(text); {
		switch c := text[i]; {
		case c == '#' && (i == 0 || text[i-1] == '\n' || scan.Blank(text[i-1])):
			i = lineEnd(text, i)
		case strings.HasPrefix(text[i:], "/*"):
			i = blockCommentEnd(text, i)
		case scan.NameStart(c) && (i == 0 || !scan.NameChar(text[i-1])):
			end := nameEnd(text, i)
			if !nixKeyword(text[i:end]) {
				tokens = append(tokens, token{start: i, end: end})
			}
			i = end
		default:
			i++
		}
	}
	return tokens
}

func nameEnd(text string, start int) int {
	end := start + scan.While(text[start:], nixNameChar)
	if closing := strings.Index(text[start:end], "''"); closing >= 0 {
		return start + closing
	}
	return end
}

func lineEnd(text string, from int) int {
	if n := strings.IndexByte(text[from:], '\n'); n >= 0 {
		return from + n
	}
	return len(text)
}

func blockCommentEnd(text string, from int) int {
	const opener, closer = "/*", "*/"
	if n := strings.Index(text[from+len(opener):], closer); n >= 0 {
		return from + len(opener) + n + len(closer)
	}
	return from + len(opener)
}
