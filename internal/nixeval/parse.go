package nixeval

import (
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strconv"
	"strings"
)

var ErrNoNixpkgsInput = errors.New("the flake locks no single nixpkgs input")

const nixpkgsName = "nixpkgs"

type lockedFlake struct {
	ref              string
	nixpkgsInputs    []string
	exactNixpkgsName bool
}

func (l lockedFlake) nixpkgsInput() (string, error) {
	if l.exactNixpkgsName {
		return nixpkgsName, nil
	}
	switch len(l.nixpkgsInputs) {
	case 0:
		return "", fmt.Errorf("%w: no root input named %s or locking the nixpkgs repository", ErrNoNixpkgsInput, nixpkgsName)
	case 1:
		return l.nixpkgsInputs[0], nil
	}
	return "", fmt.Errorf("%w: no root input is named %s and these are all nixpkgs: %s", ErrNoNixpkgsInput, nixpkgsName, strings.Join(l.nixpkgsInputs, ", "))
}

type metadataSource struct {
	Repo string `json:"repo"`
	ID   string `json:"id"`
}

type metadataNode struct {
	Inputs   map[string]json.RawMessage `json:"inputs"`
	Locked   metadataSource             `json:"locked"`
	Original metadataSource             `json:"original"`
}

type metadataLocked struct {
	NarHash string `json:"narHash"`
	Dir     string `json:"dir"`
}

type metadataLocks struct {
	Root  string                  `json:"root"`
	Nodes map[string]metadataNode `json:"nodes"`
}

type metadata struct {
	Path   string         `json:"path"`
	Locked metadataLocked `json:"locked"`
	Locks  metadataLocks  `json:"locks"`
}

func (n metadataNode) isNixpkgs() bool {
	return n.Locked.Repo == nixpkgsName || n.Original.ID == nixpkgsName
}

func parseMetadata(data []byte) (lockedFlake, error) {
	var parsed metadata
	if err := json.Unmarshal(data, &parsed); err != nil {
		return lockedFlake{}, fmt.Errorf("parsing nix flake metadata output: %w", err)
	}
	if parsed.Path == "" || parsed.Locked.NarHash == "" {
		return lockedFlake{}, errors.New("parsing nix flake metadata output: no store path or NAR hash")
	}
	root, ok := parsed.Locks.Nodes[parsed.Locks.Root]
	if !ok {
		return lockedFlake{}, fmt.Errorf("parsing nix flake metadata output: no root lock node %q", parsed.Locks.Root)
	}
	locked := lockedFlake{ref: lockedRef(parsed.Path, parsed.Locked.NarHash, parsed.Locked.Dir)}
	for _, name := range slices.Sorted(maps.Keys(root.Inputs)) {
		if name == nixpkgsName {
			locked.exactNixpkgsName = true
		}
		var nodeName string
		if err := json.Unmarshal(root.Inputs[name], &nodeName); err != nil {
			continue
		}
		if parsed.Locks.Nodes[nodeName].isNixpkgs() {
			locked.nixpkgsInputs = append(locked.nixpkgsInputs, name)
		}
	}
	return locked, nil
}

type OutputKind string

const (
	OutputDerivation   OutputKind = "derivation"
	OutputApp          OutputKind = "app"
	OutputNixOSModule  OutputKind = "nixos-module"
	OutputNixOSConfig  OutputKind = "nixos-configuration"
	OutputOverlay      OutputKind = "nixpkgs-overlay"
	OutputTemplate     OutputKind = "template"
	OutputUnknown      OutputKind = "unknown"
	OutputNotEvaluated OutputKind = ""
)

type Output struct {
	Path        []string
	Kind        OutputKind
	Name        string
	Description string
}

func parseOutputs(data []byte) ([]Output, error) {
	outputs := []Output{}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parsing nix flake show output: %w", err)
	}
	if err := collectOutputs(nil, root, &outputs); err != nil {
		return nil, fmt.Errorf("parsing nix flake show output: %w", err)
	}
	return outputs, nil
}

func collectOutputs(path []string, fields map[string]json.RawMessage, outputs *[]Output) error {
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		child := append(slices.Clip(path), name)
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(fields[name], &nested); err != nil {
			return fmt.Errorf("output %s: %w", strings.Join(child, "."), err)
		}
		kind, isLeaf := outputKind(nested)
		if !isLeaf {
			if err := collectOutputs(child, nested, outputs); err != nil {
				return err
			}
			continue
		}
		var leaf outputFields
		if err := json.Unmarshal(fields[name], &leaf); err != nil {
			return fmt.Errorf("output %s: %w", strings.Join(child, "."), err)
		}
		*outputs = append(*outputs, Output{Path: child, Kind: kind, Name: leaf.Name, Description: leaf.Description})
	}
	return nil
}

type outputFields struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func outputKind(fields map[string]json.RawMessage) (OutputKind, bool) {
	if len(fields) == 0 {
		return OutputNotEvaluated, true
	}
	var kind string
	if json.Unmarshal(fields["type"], &kind) != nil || kind == "" {
		return "", false
	}
	return OutputKind(kind), true
}

func parseSystem(data []byte) (string, error) {
	system := strings.TrimSpace(string(data))
	if system == "" {
		return "", errors.New("parsing nix config show system output: it is empty")
	}
	return system, nil
}

func parseNames(data []byte) ([]string, error) {
	names := []string{}
	if err := json.Unmarshal(data, &names); err != nil {
		return nil, fmt.Errorf("parsing attribute names: %w", err)
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}

func withPrefix(sorted []string, prefix string) []string {
	start, _ := slices.BinarySearch(sorted, prefix)
	end := start
	for end < len(sorted) && strings.HasPrefix(sorted[end], prefix) {
		end++
	}
	return slices.Clone(sorted[start:end])
}

type Declaration struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type Package struct {
	Name        string
	Version     string
	Description string
	Declaration Declaration
}

type rawPackage struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Position    string `json:"position"`
}

func parsePackage(data []byte) (packageResult, error) {
	var raw *rawPackage
	if err := json.Unmarshal(data, &raw); err != nil {
		return packageResult{}, fmt.Errorf("parsing package description: %w", err)
	}
	if raw == nil {
		return packageResult{}, nil
	}
	return packageResult{
		found: true,
		pkg:   Package{Name: raw.Name, Version: raw.Version, Description: raw.Description, Declaration: parsePosition(raw.Position)},
	}, nil
}

type packageResult struct {
	pkg   Package
	found bool
}

func parsePosition(position string) Declaration {
	i := strings.LastIndexByte(position, ':')
	if i < 0 {
		return Declaration{File: position}
	}
	line, err := strconv.Atoi(position[i+1:])
	if err != nil {
		return Declaration{File: position}
	}
	return Declaration{File: position[:i], Line: line}
}

type SourceKind int

const (
	SourceNixOS SourceKind = iota
	SourceHomeManager
	SourceFlakeParts
)

func (k SourceKind) String() string {
	return [...]string{SourceNixOS: "NixOS configuration", SourceHomeManager: "home-manager configuration", SourceFlakeParts: "flake-parts evaluation"}[k]
}

func (k SourceKind) evalName() string {
	return [...]string{SourceNixOS: "nixos", SourceHomeManager: "home-manager", SourceFlakeParts: "flake-parts"}[k]
}

type OptionSource struct {
	Kind SourceKind
	Name string
}

func parseSources(data []byte) ([]OptionSource, error) {
	var raw struct {
		NixOS       []string `json:"nixos"`
		HomeManager []string `json:"home-manager"`
		FlakeParts  bool     `json:"flake-parts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing option sources: %w", err)
	}
	slices.Sort(raw.NixOS)
	slices.Sort(raw.HomeManager)
	sources := make([]OptionSource, 0, len(raw.NixOS)+len(raw.HomeManager)+1)
	for _, name := range raw.NixOS {
		sources = append(sources, OptionSource{Kind: SourceNixOS, Name: name})
	}
	for _, name := range raw.HomeManager {
		sources = append(sources, OptionSource{Kind: SourceHomeManager, Name: name})
	}
	if raw.FlakeParts {
		sources = append(sources, OptionSource{Kind: SourceFlakeParts})
	}
	return sources, nil
}

type Option struct {
	Path         []string      `json:"loc"`
	Type         string        `json:"type"`
	Description  string        `json:"description"`
	Default      string        `json:"default"`
	Example      string        `json:"example"`
	Declarations []Declaration `json:"declarations"`
}

type OptionNode struct {
	name        string
	option      *Option
	children    []*OptionNode
	placeholder *OptionNode
}

func isPlaceholder(name string) bool {
	return name == "*" || (strings.HasPrefix(name, "<") && strings.HasSuffix(name, ">"))
}

func (n *OptionNode) Name() string {
	return n.name
}

func (n *OptionNode) IsPlaceholder() bool {
	return isPlaceholder(n.name)
}

func (n *OptionNode) Option() (Option, bool) {
	if n.option == nil {
		return Option{}, false
	}
	option := *n.option
	option.Path = slices.Clone(option.Path)
	option.Declarations = slices.Clone(option.Declarations)
	return option, true
}

func (n *OptionNode) Children() iter.Seq[*OptionNode] {
	return slices.Values(n.children)
}

func (n *OptionNode) child(name string) (*OptionNode, bool) {
	i, found := slices.BinarySearchFunc(n.children, name, func(child *OptionNode, target string) int {
		return strings.Compare(child.name, target)
	})
	if found {
		return n.children[i], true
	}
	return n.placeholder, n.placeholder != nil
}

type OptionTree struct {
	root *OptionNode
}

func (t *OptionTree) Root() *OptionNode {
	return t.root
}

func (t *OptionTree) Lookup(path []string) (*OptionNode, bool) {
	node := t.root
	for _, name := range path {
		next, ok := node.child(name)
		if !ok {
			return nil, false
		}
		node = next
	}
	return node, true
}

func parseOptions(data []byte) (*OptionTree, error) {
	var options []*Option
	if err := json.Unmarshal(data, &options); err != nil {
		return nil, fmt.Errorf("parsing option tree: %w", err)
	}
	for _, option := range options {
		if option == nil || len(option.Path) == 0 {
			return nil, errors.New("parsing option tree: an option has no location")
		}
	}
	slices.SortStableFunc(options, func(a, b *Option) int {
		return slices.Compare(a.Path, b.Path)
	})
	return &OptionTree{root: buildTree(options)}, nil
}

func buildTree(sorted []*Option) *OptionNode {
	root := &OptionNode{}
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

func (n *OptionNode) lastChildNamed(name string) *OptionNode {
	if last := len(n.children) - 1; last >= 0 && n.children[last].name == name {
		return n.children[last]
	}
	child := &OptionNode{name: name}
	n.children = append(n.children, child)
	if n.placeholder == nil && isPlaceholder(name) {
		n.placeholder = child
	}
	return child
}
