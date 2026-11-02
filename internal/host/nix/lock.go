package nix

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"path"
	"path/filepath"
	"strings"
)

const (
	LockFileName    = "flake.lock"
	storeDirectory  = "/nix/store"
	lockedName      = "source"
	narHashPrefix   = "sha256-"
	storeHashLength = 20
	storeAlphabet   = "0123456789abcdfghijklmnpqrsvwxyz"
)

type flakeLock struct {
	Root  string              `json:"root"`
	Nodes map[string]lockNode `json:"nodes"`
}

type lockNode struct {
	Inputs map[string]json.RawMessage `json:"inputs"`
	Locked struct {
		NarHash string `json:"narHash"`
		Dir     string `json:"dir"`
	} `json:"locked"`
}

func parseFlakeLock(data []byte) (flakeLock, bool) {
	var lock flakeLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return flakeLock{}, false
	}
	_, known := lock.Nodes[lock.Root]
	return lock, known
}

func (l flakeLock) nodeOf(reference json.RawMessage, depth int) (lockNode, bool) {
	if depth > len(l.Nodes) {
		return lockNode{}, false
	}
	var direct string
	if json.Unmarshal(reference, &direct) == nil {
		node, known := l.Nodes[direct]
		return node, known
	}
	var route []string
	if json.Unmarshal(reference, &route) != nil {
		return lockNode{}, false
	}
	at := l.Nodes[l.Root]
	for _, name := range route {
		next, listed := at.Inputs[name]
		if !listed {
			return lockNode{}, false
		}
		node, known := l.nodeOf(next, depth+1)
		if !known {
			return lockNode{}, false
		}
		at = node
	}
	return at, true
}

func (l flakeLock) sourceOf(input string) (string, bool) {
	reference, listed := l.Nodes[l.Root].Inputs[input]
	if !listed {
		return "", false
	}
	node, known := l.nodeOf(reference, 0)
	if !known {
		return "", false
	}
	store, ok := storePath(node.Locked.NarHash)
	if !ok {
		return "", false
	}
	return path.Join(store, node.Locked.Dir), true
}

func storePath(narHash string) (string, bool) {
	encoded, tagged := strings.CutPrefix(narHash, narHashPrefix)
	if !tagged {
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", false
	}
	fingerprint := "source:sha256:" + hex.EncodeToString(raw) + ":" + storeDirectory + ":" + lockedName
	digest := sha256.Sum256([]byte(fingerprint))
	var folded [storeHashLength]byte
	for i, b := range digest {
		folded[i%storeHashLength] ^= b
	}
	return storeDirectory + "/" + storeBase32(folded) + "-" + lockedName, true
}

func storeBase32(hash [storeHashLength]byte) string {
	const bits = 5
	length := (len(hash)*8-1)/bits + 1
	var out strings.Builder
	out.Grow(length)
	for n := length - 1; n >= 0; n-- {
		bit := n * bits
		i, shift := bit/8, uint(bit%8)
		v := uint(hash[i]) >> shift
		if i+1 < len(hash) {
			v |= uint(hash[i+1]) << (8 - shift)
		}
		out.WriteByte(storeAlphabet[v&31])
	}
	return out.String()
}

func dirOf(file string) string {
	if InStore(file) {
		return path.Dir(file)
	}
	return filepath.Dir(file)
}

func joinedTo(dir, rest string) string {
	if InStore(dir) {
		return path.Join(dir, rest)
	}
	return filepath.Join(dir, rest)
}

func absolute(file string) bool {
	return filepath.IsAbs(file) || strings.HasPrefix(filepath.ToSlash(file), "/")
}

func Within(dir, file string) string {
	switch {
	case InStore(file):
		return path.Clean(file)
	case absolute(file):
		return filepath.Clean(file)
	}
	return joinedTo(dir, file)
}
