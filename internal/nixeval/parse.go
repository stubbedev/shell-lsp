package nixeval

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

type lockedFlake struct {
	ref string
}

type metadataLocked struct {
	NarHash string `json:"narHash"`
	Dir     string `json:"dir"`
}

type metadata struct {
	Path   string         `json:"path"`
	Locked metadataLocked `json:"locked"`
}

func parseMetadata(data []byte) (lockedFlake, error) {
	var parsed metadata
	if err := json.Unmarshal(data, &parsed); err != nil {
		return lockedFlake{}, fmt.Errorf("parsing nix flake metadata output: %w", err)
	}
	if parsed.Path == "" || parsed.Locked.NarHash == "" {
		return lockedFlake{}, errors.New("parsing nix flake metadata output: no store path or NAR hash")
	}
	return lockedFlake{ref: lockedRef(parsed.Path, parsed.Locked.NarHash, parsed.Locked.Dir)}, nil
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

func parsePackage(data []byte) (*Package, error) {
	var raw *rawPackage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing package description: %w", err)
	}
	if raw == nil {
		return nil, nil
	}
	return &Package{Name: raw.Name, Version: raw.Version, Description: raw.Description, Declaration: parsePosition(raw.Position)}, nil
}

func parsePosition(position string) Declaration {
	file, number, found := strings.CutLast(position, ":")
	if !found {
		return Declaration{File: position}
	}
	line, err := strconv.Atoi(number)
	if err != nil {
		return Declaration{File: position}
	}
	return Declaration{File: file, Line: line}
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
