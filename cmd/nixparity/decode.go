package main

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

type normalizer struct {
	root string
}

func (n normalizer) uri(uri string) string {
	path, err := fileuri.ToPath(uri)
	if err != nil {
		return uri
	}
	rel, err := filepath.Rel(n.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

type decodeContext struct {
	norm normalizer
	doc  *docstore.Document
}

type decoder func(ctx decodeContext, raw json.RawMessage) (answer, error)

func fromItems(items []string) answer {
	if len(items) == 0 {
		return answer{outcome: outcomeEmpty}
	}
	slices.Sort(items)
	return answer{outcome: outcomeAnswered, items: slices.Compact(items)}
}

func isNull(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

func classify(spec featureSpec, ctx decodeContext, r reply) (answer, error) {
	switch r.state {
	case replyTimedOut:
		return answer{outcome: outcomeTimeout}, nil
	case replyDied:
		return answer{outcome: outcomeCrashed}, nil
	case replyResponded:
	}
	if r.rpcErr != nil {
		if r.rpcErr.Code == jsonrpc.CodeMethodNotFound {
			return answer{outcome: outcomeUnsupported}, nil
		}
		return answer{outcome: outcomeRefused, latency: r.elapsed}, nil
	}
	return decodeResult(spec, ctx, r)
}

func decodeResult(spec featureSpec, ctx decodeContext, r reply) (answer, error) {
	if isNull(r.result) {
		return answer{outcome: outcomeEmpty, latency: r.elapsed}, nil
	}
	a, err := spec.decode(ctx, r.result)
	if err != nil {
		return answer{outcome: outcomeMalformed, latency: r.elapsed}, fmt.Errorf("decoding the %s answer: %w", spec.name, err)
	}
	a.latency = r.elapsed
	return a, nil
}

func positionKey(p lsp.Position) string {
	return strconv.FormatUint(uint64(p.Line), 10) + ":" + strconv.FormatUint(uint64(p.Character), 10)
}

func rangeKey(r lsp.Range) string {
	return positionKey(r.Start) + "-" + positionKey(r.End)
}

func oneOrMany[T any](raw json.RawMessage) ([]T, error) {
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		var many []T
		err := json.Unmarshal(raw, &many)
		return many, err
	}
	var one T
	if err := json.Unmarshal(raw, &one); err != nil {
		return nil, err
	}
	return []T{one}, nil
}

type wireLocation struct {
	URI                  string    `json:"uri"`
	Range                lsp.Range `json:"range"`
	TargetURI            string    `json:"targetUri"`
	TargetSelectionRange lsp.Range `json:"targetSelectionRange"`
}

func (l wireLocation) key(n normalizer) (string, error) {
	switch {
	case l.TargetURI != "":
		return n.uri(l.TargetURI) + ":" + rangeKey(l.TargetSelectionRange), nil
	case l.URI != "":
		return n.uri(l.URI) + ":" + rangeKey(l.Range), nil
	}
	return "", errors.New("a location without a uri or targetUri")
}

func decodeLocations(ctx decodeContext, raw json.RawMessage) (answer, error) {
	locations, err := oneOrMany[wireLocation](raw)
	if err != nil {
		return answer{}, err
	}
	items := make([]string, 0, len(locations))
	for _, l := range locations {
		key, err := l.key(ctx.norm)
		if err != nil {
			return answer{}, err
		}
		items = append(items, key)
	}
	return fromItems(items), nil
}

func decodeDiagnostics(_ decodeContext, raw json.RawMessage) (answer, error) {
	var published struct {
		Diagnostics []struct {
			Range lsp.Range `json:"range"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(raw, &published); err != nil {
		return answer{}, err
	}
	items := make([]string, 0, len(published.Diagnostics))
	for _, d := range published.Diagnostics {
		items = append(items, positionKey(d.Range.Start))
	}
	return fromItems(items), nil
}

type wireSymbol struct {
	Name           string        `json:"name"`
	SelectionRange *lsp.Range    `json:"selectionRange"`
	Location       *lsp.Location `json:"location"`
	Children       []wireSymbol  `json:"children"`
}

func appendSymbols(items []string, symbols []wireSymbol) ([]string, error) {
	for _, s := range symbols {
		var at lsp.Position
		switch {
		case s.SelectionRange != nil:
			at = s.SelectionRange.Start
		case s.Location != nil:
			at = s.Location.Range.Start
		default:
			return nil, errors.New("a symbol without a selectionRange or location")
		}
		items = append(items, positionKey(at)+" "+s.Name)
		var err error
		if items, err = appendSymbols(items, s.Children); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func decodeSymbols(_ decodeContext, raw json.RawMessage) (answer, error) {
	var symbols []wireSymbol
	if err := json.Unmarshal(raw, &symbols); err != nil {
		return answer{}, err
	}
	items, err := appendSymbols(nil, symbols)
	if err != nil {
		return answer{}, err
	}
	return fromItems(items), nil
}

func decodeFolding(_ decodeContext, raw json.RawMessage) (answer, error) {
	var ranges []lsp.FoldingRange
	if err := json.Unmarshal(raw, &ranges); err != nil {
		return answer{}, err
	}
	items := make([]string, 0, len(ranges))
	for _, r := range ranges {
		items = append(items, strconv.FormatUint(uint64(r.StartLine), 10)+"-"+strconv.FormatUint(uint64(r.EndLine), 10))
	}
	return fromItems(items), nil
}

func decodeRename(ctx decodeContext, raw json.RawMessage) (answer, error) {
	var edit lsp.WorkspaceEdit
	if err := json.Unmarshal(raw, &edit); err != nil {
		return answer{}, err
	}
	var items []string
	add := func(uri string, edits []lsp.TextEdit) {
		for _, e := range edits {
			items = append(items, ctx.norm.uri(uri)+":"+rangeKey(e.Range)+"="+e.NewText)
		}
	}
	for uri, edits := range edit.Changes {
		add(uri, edits)
	}
	for _, change := range edit.DocumentChanges {
		add(change.TextDocument.URI, change.Edits)
	}
	return fromItems(items), nil
}

func contentsEmpty(raw json.RawMessage) (bool, error) {
	if isNull(raw) {
		return true, nil
	}
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		var parts []json.RawMessage
		if err := json.Unmarshal(raw, &parts); err != nil {
			return false, err
		}
		for _, part := range parts {
			empty, err := contentsEmpty(part)
			if err != nil || !empty {
				return false, err
			}
		}
		return true, nil
	}
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte(`"`)) {
		var text string
		err := json.Unmarshal(raw, &text)
		return strings.TrimSpace(text) == "", err
	}
	var markup struct {
		Value string `json:"value"`
	}
	err := json.Unmarshal(raw, &markup)
	return strings.TrimSpace(markup.Value) == "", err
}

func decodeHover(_ decodeContext, raw json.RawMessage) (answer, error) {
	var hover struct {
		Contents json.RawMessage `json:"contents"`
		Range    *lsp.Range      `json:"range"`
	}
	if err := json.Unmarshal(raw, &hover); err != nil {
		return answer{}, err
	}
	empty, err := contentsEmpty(hover.Contents)
	if err != nil {
		return answer{}, err
	}
	switch {
	case empty:
		return answer{outcome: outcomeEmpty}, nil
	case hover.Range == nil:
		return answer{outcome: outcomeAnswered}, nil
	}
	return answer{outcome: outcomeAnswered, items: []string{rangeKey(*hover.Range)}}, nil
}

func decodeCompletion(_ decodeContext, raw json.RawMessage) (answer, error) {
	type item struct {
		Label string `json:"label"`
	}
	var entries []item
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		if err := json.Unmarshal(raw, &entries); err != nil {
			return answer{}, err
		}
	} else {
		var list struct {
			Items []item `json:"items"`
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			return answer{}, err
		}
		entries = list.Items
	}
	items := make([]string, 0, len(entries))
	for _, e := range entries {
		items = append(items, e.Label)
	}
	return fromItems(items), nil
}

func applyEdits(doc *docstore.Document, edits []lsp.TextEdit) (string, error) {
	mapped := make([]textedit.Edit, 0, len(edits))
	for _, e := range edits {
		mapped = append(mapped, textedit.Edit{
			Start:   doc.PositionToOffset(e.Range.Start, docstore.EncodingUTF16),
			End:     doc.PositionToOffset(e.Range.End, docstore.EncodingUTF16),
			NewText: e.NewText,
		})
	}
	slices.SortStableFunc(mapped, func(a, b textedit.Edit) int { return cmp.Compare(a.Start, b.Start) })
	last := 0
	for _, e := range mapped {
		if e.Start < last || e.End < e.Start {
			return "", fmt.Errorf("edit of bytes %d to %d overlaps or runs backwards after byte %d", e.Start, e.End, last)
		}
		last = e.End
	}
	return textedit.Apply(doc.Text(), mapped), nil
}

func decodeFormatting(ctx decodeContext, raw json.RawMessage) (answer, error) {
	var edits []lsp.TextEdit
	if err := json.Unmarshal(raw, &edits); err != nil {
		return answer{}, err
	}
	formatted, err := applyEdits(ctx.doc, edits)
	if err != nil {
		return answer{}, err
	}
	sum := sha256.Sum256([]byte(formatted))
	return answer{outcome: outcomeAnswered, items: []string{hex.EncodeToString(sum[:])}}, nil
}
