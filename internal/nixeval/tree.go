package nixeval

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"math"
	"slices"
	"strings"
)

const (
	treeHeaderSize = 8
	nodeFields     = 7
	nodeSize       = nodeFields * 4
	absentIndex    = math.MaxUint32

	fieldParent      = 0
	fieldFirstChild  = 1
	fieldChildCount  = 2
	fieldPlaceholder = 3
	fieldNameOffset  = 4
	fieldNameLength  = 5
	fieldOption      = 6
)

type Option struct {
	Path         []string      `json:"loc"`
	Type         string        `json:"type"`
	Description  string        `json:"description"`
	Default      string        `json:"default"`
	Example      string        `json:"example"`
	Declarations []Declaration `json:"declarations"`
}

func isPlaceholder(name string) bool {
	return name == "*" || (strings.HasPrefix(name, "<") && strings.HasSuffix(name, ">"))
}

type OptionTree struct {
	nodes     []byte
	blob      []byte
	truncated [][]string
}

type OptionNode struct {
	tree  *OptionTree
	index uint32
}

func (t *OptionTree) Root() OptionNode {
	return OptionNode{tree: t}
}

func (t *OptionTree) Truncated() [][]string {
	paths := make([][]string, len(t.truncated))
	for i, path := range t.truncated {
		paths[i] = slices.Clone(path)
	}
	return paths
}

func (t *OptionTree) Lookup(path []string) (OptionNode, bool) {
	node := t.Root()
	for _, name := range path {
		next, ok := node.child(name)
		if !ok {
			return OptionNode{}, false
		}
		node = next
	}
	return node, true
}

func (t *OptionTree) field(index uint32, field int) uint32 {
	return binary.LittleEndian.Uint32(t.nodes[int(index)*nodeSize+field*4:])
}

func (n OptionNode) field(field int) uint32 {
	return n.tree.field(n.index, field)
}

func (n OptionNode) nameBytes() []byte {
	offset := n.field(fieldNameOffset)
	return n.tree.blob[offset : offset+n.field(fieldNameLength)]
}

func (n OptionNode) Name() string {
	return string(n.nameBytes())
}

func (n OptionNode) IsPlaceholder() bool {
	return isPlaceholder(n.Name())
}

func (n OptionNode) Option() (Option, bool) {
	offset := n.field(fieldOption)
	if offset == absentIndex {
		return Option{}, false
	}
	var option Option
	reader := blobReader{data: n.tree.blob, pos: uint64(offset)}
	reader.option(&option)
	if reader.failed {
		panic("nixeval: a validated option record does not decode")
	}
	option.Path = n.path()
	return option, true
}

func (n OptionNode) path() []string {
	var path []string
	for node := n; node.index != 0; node = (OptionNode{tree: n.tree, index: node.field(fieldParent)}) {
		path = append(path, node.Name())
	}
	slices.Reverse(path)
	return path
}

func (n OptionNode) Children() iter.Seq[OptionNode] {
	return func(yield func(OptionNode) bool) {
		first := n.field(fieldFirstChild)
		for i := range n.field(fieldChildCount) {
			if !yield(OptionNode{tree: n.tree, index: first + i}) {
				return
			}
		}
	}
}

func (n OptionNode) child(name string) (OptionNode, bool) {
	first := n.field(fieldFirstChild)
	low, high := uint32(0), n.field(fieldChildCount)
	for low < high {
		middle := low + (high-low)/2
		candidate := OptionNode{tree: n.tree, index: first + middle}
		switch got := candidate.nameBytes(); {
		case string(got) == name:
			return candidate, true
		case string(got) < name:
			low = middle + 1
		default:
			high = middle
		}
	}
	placeholder := n.field(fieldPlaceholder)
	if placeholder == absentIndex {
		return OptionNode{}, false
	}
	return OptionNode{tree: n.tree, index: placeholder}, true
}

type treeBuilderNode struct {
	name     string
	option   *Option
	children []*treeBuilderNode
}

func (n *treeBuilderNode) lastChildNamed(name string) *treeBuilderNode {
	if last := len(n.children) - 1; last >= 0 && n.children[last].name == name {
		return n.children[last]
	}
	child := &treeBuilderNode{name: name}
	n.children = append(n.children, child)
	return child
}

func buildTree(sorted []*Option) *treeBuilderNode {
	root := &treeBuilderNode{}
	for _, option := range sorted {
		node := root
		for _, name := range option.Path {
			node = node.lastChildNamed(name)
		}
		if node.option == nil {
			node.option = option
		}
	}
	return root
}

func encodeOptions(data []byte) ([]byte, error) {
	var raw struct {
		Options   []*Option  `json:"options"`
		Truncated [][]string `json:"truncated"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing option tree: %w", err)
	}
	if raw.Options == nil {
		return nil, errors.New("parsing option tree: it lists no options")
	}
	for _, option := range raw.Options {
		if option == nil || len(option.Path) == 0 {
			return nil, errors.New("parsing option tree: an option has no location")
		}
	}
	slices.SortStableFunc(raw.Options, func(a, b *Option) int {
		return slices.Compare(a.Path, b.Path)
	})
	slices.SortFunc(raw.Truncated, slices.Compare[[]string])
	encoder := treeEncoder{interned: map[string]uint32{}}
	return encoder.encode(buildTree(raw.Options), raw.Truncated), nil
}

type treeEncoder struct {
	blob     []byte
	interned map[string]uint32
}

func width32(n int) uint32 {
	return uint32(n) //nolint:gosec // G115: maxStdout bounds every length and count of an encoded tree far below 4 GiB
}

func (e *treeEncoder) intern(s string) uint32 {
	if offset, ok := e.interned[s]; ok {
		return offset
	}
	offset := width32(len(e.blob))
	e.blob = append(e.blob, s...)
	e.interned[s] = offset
	return offset
}

func (e *treeEncoder) text(s string) {
	e.blob = binary.AppendUvarint(e.blob, uint64(len(s)))
	e.blob = append(e.blob, s...)
}

func (e *treeEncoder) option(option *Option) uint32 {
	offsets := make([]uint32, len(option.Declarations))
	for i, declaration := range option.Declarations {
		offsets[i] = e.intern(declaration.File)
	}
	start := width32(len(e.blob))
	e.text(option.Type)
	e.text(option.Description)
	e.text(option.Default)
	e.text(option.Example)
	e.blob = binary.AppendUvarint(e.blob, uint64(len(option.Declarations)))
	for i, declaration := range option.Declarations {
		e.blob = binary.AppendUvarint(e.blob, uint64(offsets[i]))
		e.blob = binary.AppendUvarint(e.blob, uint64(len(declaration.File)))
		e.blob = binary.AppendVarint(e.blob, int64(declaration.Line))
		e.blob = binary.AppendVarint(e.blob, int64(declaration.Column))
	}
	return start
}

func (e *treeEncoder) encode(root *treeBuilderNode, truncated [][]string) []byte {
	order := []*treeBuilderNode{root}
	parents := []uint32{0}
	var nodes []byte
	for index := 0; index < len(order); index++ {
		node := order[index]
		first := width32(len(order))
		placeholder := uint32(absentIndex)
		for i, child := range node.children {
			if placeholder == absentIndex && isPlaceholder(child.name) {
				placeholder = first + width32(i)
			}
			order = append(order, child)
			parents = append(parents, width32(index))
		}
		option := uint32(absentIndex)
		if node.option != nil {
			option = e.option(node.option)
		}
		for _, value := range [nodeFields]uint32{
			fieldParent:      parents[index],
			fieldFirstChild:  first,
			fieldChildCount:  width32(len(node.children)),
			fieldPlaceholder: placeholder,
			fieldNameOffset:  e.intern(node.name),
			fieldNameLength:  width32(len(node.name)),
			fieldOption:      option,
		} {
			nodes = binary.LittleEndian.AppendUint32(nodes, value)
		}
	}
	truncatedOffset := width32(len(e.blob))
	e.blob = binary.AppendUvarint(e.blob, uint64(len(truncated)))
	for _, path := range truncated {
		e.blob = binary.AppendUvarint(e.blob, uint64(len(path)))
		for _, name := range path {
			e.text(name)
		}
	}
	out := make([]byte, 0, treeHeaderSize+len(nodes)+len(e.blob))
	out = binary.LittleEndian.AppendUint32(out, width32(len(order)))
	out = binary.LittleEndian.AppendUint32(out, truncatedOffset)
	out = append(out, nodes...)
	return append(out, e.blob...)
}

type blobReader struct {
	data   []byte
	pos    uint64
	failed bool
}

func (r *blobReader) uvarint() uint64 {
	if r.failed {
		return 0
	}
	value, n := binary.Uvarint(r.data[r.pos:])
	if n <= 0 {
		r.failed = true
		return 0
	}
	r.pos += uint64(n)
	return value
}

func (r *blobReader) varint() int64 {
	if r.failed {
		return 0
	}
	value, n := binary.Varint(r.data[r.pos:])
	if n <= 0 {
		r.failed = true
		return 0
	}
	r.pos += uint64(n)
	return value
}

func (r *blobReader) slice(offset, length uint64) []byte {
	if r.failed || offset > uint64(len(r.data)) || length > uint64(len(r.data))-offset {
		r.failed = true
		return nil
	}
	return r.data[offset : offset+length]
}

func (r *blobReader) text() []byte {
	length := r.uvarint()
	value := r.slice(r.pos, length)
	if !r.failed {
		r.pos += length
	}
	return value
}

func (r *blobReader) option(into *Option) {
	optionType, description, defaultValue, example := r.text(), r.text(), r.text(), r.text()
	count := r.uvarint()
	if count > uint64(len(r.data)) {
		r.failed = true
		return
	}
	if into != nil && !r.failed {
		into.Type, into.Description, into.Default, into.Example = string(optionType), string(description), string(defaultValue), string(example)
		into.Declarations = make([]Declaration, 0, count)
	}
	for range count {
		offset := r.uvarint()
		length := r.uvarint()
		file := r.slice(offset, length)
		line, column := r.varint(), r.varint()
		if r.failed {
			return
		}
		if into != nil {
			into.Declarations = append(into.Declarations, Declaration{File: string(file), Line: int(line), Column: int(column)})
		}
	}
}

func decodeOptions(payload []byte) (*OptionTree, error) {
	if len(payload) < treeHeaderSize {
		return nil, fmt.Errorf("decoding an option tree: only %d bytes", len(payload))
	}
	nodeCount := binary.LittleEndian.Uint32(payload)
	count := uint64(nodeCount)
	end := treeHeaderSize + count*nodeSize
	if count == 0 || end > uint64(len(payload)) {
		return nil, fmt.Errorf("decoding an option tree: %d nodes do not fit %d bytes", count, len(payload))
	}
	tree := &OptionTree{nodes: payload[treeHeaderSize:end], blob: payload[end:]}
	for index := range nodeCount {
		if err := tree.validateNode(index, nodeCount); err != nil {
			return nil, fmt.Errorf("decoding an option tree: node %d: %w", index, err)
		}
	}
	truncated, err := tree.readTruncated(binary.LittleEndian.Uint32(payload[4:]))
	if err != nil {
		return nil, fmt.Errorf("decoding an option tree: %w", err)
	}
	tree.truncated = truncated
	return tree, nil
}

func (t *OptionTree) validateNode(index, count uint32) error {
	parent, first, children := t.field(index, fieldParent), t.field(index, fieldFirstChild), t.field(index, fieldChildCount)
	if index != 0 && parent >= index {
		return fmt.Errorf("parent %d is not an earlier node", parent)
	}
	if uint64(first)+uint64(children) > uint64(count) || (children != 0 && first <= index) {
		return fmt.Errorf("children %d+%d are out of range", first, children)
	}
	placeholder := t.field(index, fieldPlaceholder)
	if placeholder != absentIndex && (placeholder < first || placeholder-first >= children) {
		return fmt.Errorf("placeholder %d is not a child", placeholder)
	}
	reader := blobReader{data: t.blob}
	reader.slice(uint64(t.field(index, fieldNameOffset)), uint64(t.field(index, fieldNameLength)))
	if reader.failed {
		return errors.New("the name is out of range")
	}
	offset := t.field(index, fieldOption)
	if offset == absentIndex {
		return nil
	}
	if uint64(offset) > uint64(len(t.blob)) {
		return fmt.Errorf("the option at %d is out of range", offset)
	}
	reader.pos = uint64(offset)
	reader.option(nil)
	if reader.failed {
		return fmt.Errorf("the option at %d does not decode", offset)
	}
	return nil
}

func (t *OptionTree) readTruncated(offset uint32) ([][]string, error) {
	if uint64(offset) > uint64(len(t.blob)) {
		return nil, fmt.Errorf("the truncated paths at %d are out of range", offset)
	}
	reader := blobReader{data: t.blob, pos: uint64(offset)}
	count := reader.uvarint()
	if count > uint64(len(t.blob)) {
		return nil, fmt.Errorf("%d truncated paths do not fit", count)
	}
	paths := make([][]string, 0, count)
	for range count {
		length := reader.uvarint()
		if length > uint64(len(t.blob)) {
			return nil, fmt.Errorf("a truncated path of %d names does not fit", length)
		}
		path := make([]string, 0, length)
		for range length {
			path = append(path, string(reader.text()))
		}
		paths = append(paths, path)
	}
	if reader.failed {
		return nil, errors.New("the truncated paths do not decode")
	}
	return paths, nil
}
