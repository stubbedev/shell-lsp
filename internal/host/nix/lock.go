package nix

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
)

const (
	lockFile        = "flake.lock"
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
	var path []string
	if json.Unmarshal(reference, &path) != nil {
		return lockNode{}, false
	}
	at, known := l.Nodes[l.Root], true
	for _, name := range path {
		next, listed := at.Inputs[name]
		if !listed {
			return lockNode{}, false
		}
		if at, known = l.nodeOf(next, depth+1); !known {
			return lockNode{}, false
		}
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
	return filepath.Join(store, node.Locked.Dir), true
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
