package nixeval

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseLockRequiresItsRootNode(t *testing.T) {
	t.Parallel()

	for _, in := range []string{`not json`, `{}`, `{"root": "root", "nodes": {}}`} {
		if _, err := parseLock([]byte(in)); err == nil {
			t.Errorf("parseLock(%s) = nil error", in)
		}
	}
}

func TestNixpkgsInputChoosesOnlyWhatTheFlakeLocks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		nodes   string
		want    string
		wantErr string
	}{
		{
			name:  "the name wins over other nixpkgs inputs",
			nodes: `"root": {"inputs": {"nixpkgs": "a", "unstable": "b"}}, "a": {"locked": {"repo": "nixpkgs"}}, "b": {"locked": {"repo": "nixpkgs"}}`,
			want:  "nixpkgs",
		},
		{
			name:  "a followed input keeps its name",
			nodes: `"root": {"inputs": {"nixpkgs": ["other", "nixpkgs"]}}`,
			want:  "nixpkgs",
		},
		{
			name:  "a single renamed nixpkgs",
			nodes: `"root": {"inputs": {"unstable": "b", "tools": "c"}}, "b": {"locked": {"repo": "nixpkgs"}}, "c": {"locked": {"repo": "tools"}}`,
			want:  "unstable",
		},
		{
			name:  "a registry reference",
			nodes: `"root": {"inputs": {"pkgs": "b"}}, "b": {"original": {"id": "nixpkgs", "type": "indirect"}}`,
			want:  "pkgs",
		},
		{
			name:    "two renamed inputs are ambiguous",
			nodes:   `"root": {"inputs": {"stable": "b", "unstable": "c"}}, "b": {"locked": {"repo": "nixpkgs"}}, "c": {"locked": {"repo": "nixpkgs"}}`,
			wantErr: "stable, unstable",
		},
		{
			name:    "no nixpkgs at all",
			nodes:   `"root": {"inputs": {"tools": "c"}}, "c": {"locked": {"repo": "tools"}}`,
			wantErr: "no root input",
		},
	}
	for _, tt := range tests {
		lock, err := parseLock([]byte(`{"root": "root", "nodes": {` + tt.nodes + `}}`))
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		got, err := lock.nixpkgsInput()
		if tt.wantErr != "" {
			if !errors.Is(err, ErrNoNixpkgsInput) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s: error = %v, want ErrNoNixpkgsInput naming %q", tt.name, err, tt.wantErr)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("%s: nixpkgsInput() = %q, %v, want %q", tt.name, got, err, tt.want)
		}
	}
}

func TestClosureHashesFollowTheInputAndItsFollows(t *testing.T) {
	t.Parallel()

	lock, err := parseLock([]byte(`{"root": "root", "nodes": {
		"root": {"inputs": {"a": "a", "b": ["a", "dep"], "local": "local", "cycle": "x", "broken": "broken"}},
		"a": {"locked": {"narHash": "sha256-A"}, "inputs": {"dep": "dep", "back": ["b"]}},
		"dep": {"locked": {"narHash": "sha256-D"}},
		"local": {"locked": {"path": "./sub"}},
		"x": {"locked": {"narHash": "sha256-X"}, "inputs": {"y": "y"}},
		"y": {"locked": {"narHash": "sha256-Y"}, "inputs": {"x": "x"}},
		"broken": {"locked": {"narHash": "sha256-B"}, "inputs": {"gone": ["missing"]}}
	}}`))
	if err != nil {
		t.Fatal(err)
	}
	for input, want := range map[string][]string{
		"a":     {"sha256-A", "sha256-D"},
		"b":     {"sha256-D"},
		"cycle": {"sha256-X", "sha256-Y"},
	} {
		got, addressed, err := lock.closureHashes(input)
		if err != nil || !addressed || !slices.Equal(got, want) {
			t.Errorf("closureHashes(%s) = %q, %v, %v, want %q", input, got, addressed, err, want)
		}
	}
	if _, addressed, err := lock.closureHashes("local"); err != nil || addressed {
		t.Errorf("closureHashes(local) = %v, %v, want it not content addressed", addressed, err)
	}
	if _, _, err := lock.closureHashes("broken"); err == nil {
		t.Error("closureHashes(broken) = nil error for a follows of a missing input")
	}
	if _, _, err := lock.closureHashes("absent"); err == nil {
		t.Error("closureHashes(absent) = nil error")
	}
}

func TestLockedAtFollowsNamesThroughTheLock(t *testing.T) {
	t.Parallel()

	lock, err := parseLock([]byte(`{"root": "root", "version": 7, "nodes": {
  "root": {"inputs": {"dep": "dep_2", "alias": ["dep", "inner"]}},
  "dep_2": {"inputs": {"inner": "inner"}, "locked": {"type": "tarball", "url": "file:///d"}},
  "inner": {"locked": {"type": "github", "owner": "o"}}
}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		names []string
		want  string
	}{
		{[]string{"dep"}, `{"type": "tarball", "url": "file:///d"}`},
		{[]string{"dep", "inner"}, `{"type": "github", "owner": "o"}`},
		{[]string{"alias"}, `{"type": "github", "owner": "o"}`},
	} {
		got, err := lock.lockedAt(tt.names)
		if err != nil || string(got) != tt.want {
			t.Errorf("lockedAt(%q) = %s, %v, want %s", tt.names, got, err, tt.want)
		}
	}
	for _, names := range [][]string{{"absent"}, {"dep", "absent"}, {}} {
		if got, err := lock.lockedAt(names); err == nil {
			t.Errorf("lockedAt(%q) = %s, want an error", names, got)
		}
	}
}
