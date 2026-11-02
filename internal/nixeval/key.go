package nixeval

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const systemLookupTimeout = 5 * time.Second

func digestOf(fields ...string) [32]byte {
	h := sha256.New()
	for _, field := range fields {
		writeField(h, field)
	}
	return [32]byte(h.Sum(nil))
}

type flakeState struct {
	dir      string
	identity string
	lock     []byte
	local    contentKey
}

func (f flakeState) queryKey(kind queryKind, detail string) contentKey {
	return digestOf("local", string(f.local[:]), string(kind), detail)
}

func (f flakeState) slot(kind queryKind, detail string) slotKey {
	return digestOf(f.dir, string(kind), detail)
}

func (c *Client) inspect(ctx context.Context, dir string) (flakeState, error) {
	if !filepath.IsAbs(dir) {
		return flakeState{}, fmt.Errorf("the flake directory %q is not absolute", dir)
	}
	clean := filepath.Clean(dir)
	nix, lock, err := readFlakeFiles(clean)
	if err != nil {
		return flakeState{}, err
	}
	identity, err := c.identify()
	if err != nil {
		return flakeState{}, err
	}
	tree, err := fingerprintTree(ctx, clean)
	if err != nil {
		return flakeState{}, err
	}
	lockPresent := "locked"
	if lock == nil {
		lockPresent = "unlocked"
	}
	local := digestOf(string(evalDigest[:]), identity, string(nix), lockPresent, string(lock), tree)
	return flakeState{dir: clean, identity: identity, lock: lock, local: local}, nil
}

type inputScope struct {
	input  string
	system string
	digest [32]byte
}

func (s inputScope) queryKey(kind queryKind, detail string) contentKey {
	return digestOf("input", string(s.digest[:]), string(kind), detail)
}

func (s inputScope) args(locked lockedFlake) []exprArg {
	return []exprArg{stringArg("ref", locked.ref), stringArg("input", s.input), stringArg("system", s.system)}
}

func (c *Client) inputScope(ctx context.Context, flake flakeState) (inputScope, error) {
	if flake.lock == nil {
		return inputScope{}, fmt.Errorf("%w: the flake has no flake.lock", ErrNoNixpkgsInput)
	}
	lock, err := parseLock(flake.lock)
	if err != nil {
		return inputScope{}, err
	}
	input, err := lock.nixpkgsInput()
	if err != nil {
		return inputScope{}, err
	}
	hashes, contentAddressed, err := lock.closureHashes(input)
	if err != nil {
		return inputScope{}, err
	}
	system, err := c.currentSystem(ctx)
	if err != nil {
		return inputScope{}, err
	}
	if !contentAddressed {
		return inputScope{input: input, system: system, digest: digestOf("flake", string(flake.local[:]), system, input)}, nil
	}
	return inputScope{
		input:  input,
		system: system,
		digest: digestOf("closure", string(evalDigest[:]), flake.identity, system, input, strings.Join(hashes, "\x00")),
	}, nil
}
