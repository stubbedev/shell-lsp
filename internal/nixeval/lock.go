package nixeval

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

var ErrNoNixpkgsInput = errors.New("the flake locks no single nixpkgs input")

const nixpkgsName = "nixpkgs"

type lockSource struct {
	Repo    string `json:"repo"`
	ID      string `json:"id"`
	NarHash string `json:"narHash"`
}

type lockNode struct {
	Inputs   map[string]json.RawMessage `json:"inputs"`
	Locked   lockSource                 `json:"locked"`
	Original lockSource                 `json:"original"`
}

func (n lockNode) isNixpkgs() bool {
	return n.Locked.Repo == nixpkgsName || n.Original.ID == nixpkgsName
}

type lockFile struct {
	Root  string              `json:"root"`
	Nodes map[string]lockNode `json:"nodes"`
}

func parseLock(data []byte) (lockFile, error) {
	var parsed lockFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return lockFile{}, fmt.Errorf("parsing flake.lock: %w", err)
	}
	if _, ok := parsed.Nodes[parsed.Root]; !ok {
		return lockFile{}, fmt.Errorf("parsing flake.lock: no root node %q", parsed.Root)
	}
	return parsed, nil
}

func (l lockFile) nixpkgsInput() (string, error) {
	inputs := l.Nodes[l.Root].Inputs
	if _, exact := inputs[nixpkgsName]; exact {
		return nixpkgsName, nil
	}
	var candidates []string
	for _, name := range slices.Sorted(maps.Keys(inputs)) {
		var node string
		if json.Unmarshal(inputs[name], &node) == nil && l.Nodes[node].isNixpkgs() {
			candidates = append(candidates, name)
		}
	}
	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("%w: no root input named %s or locking the nixpkgs repository", ErrNoNixpkgsInput, nixpkgsName)
	case 1:
		return candidates[0], nil
	}
	return "", fmt.Errorf("%w: no root input is named %s and these are all nixpkgs: %s", ErrNoNixpkgsInput, nixpkgsName, strings.Join(candidates, ", "))
}

func (l lockFile) resolve(reference json.RawMessage, depth int) (string, error) {
	if depth > len(l.Nodes) {
		return "", errors.New("the follows of flake.lock form a cycle")
	}
	var node string
	if json.Unmarshal(reference, &node) == nil {
		return node, nil
	}
	var path []string
	if err := json.Unmarshal(reference, &path); err != nil {
		return "", fmt.Errorf("parsing a flake.lock input: %w", err)
	}
	current := l.Root
	for _, name := range path {
		next, ok := l.Nodes[current].Inputs[name]
		if !ok {
			return "", fmt.Errorf("flake.lock follows the missing input %q of node %q", name, current)
		}
		resolved, err := l.resolve(next, depth+1)
		if err != nil {
			return "", err
		}
		current = resolved
	}
	return current, nil
}

func (l lockFile) closureHashes(input string) ([]string, bool, error) {
	reference, ok := l.Nodes[l.Root].Inputs[input]
	if !ok {
		return nil, false, fmt.Errorf("flake.lock has no root input %q", input)
	}
	start, err := l.resolve(reference, 0)
	if err != nil {
		return nil, false, err
	}
	seen := map[string]bool{}
	pending := []string{start}
	var hashes []string
	for len(pending) > 0 {
		name := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[name] {
			continue
		}
		seen[name] = true
		node := l.Nodes[name]
		if node.Locked.NarHash == "" {
			return nil, false, nil
		}
		hashes = append(hashes, node.Locked.NarHash)
		for _, child := range slices.Sorted(maps.Keys(node.Inputs)) {
			resolved, err := l.resolve(node.Inputs[child], 0)
			if err != nil {
				return nil, false, err
			}
			pending = append(pending, resolved)
		}
	}
	slices.Sort(hashes)
	return hashes, true, nil
}
