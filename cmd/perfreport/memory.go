package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/stubbedev/shell-lsp/internal/corpus"
)

type parityServer struct {
	Server       tool   `json:"server"`
	Restarts     int    `json:"restarts"`
	PeakRSSBytes *int64 `json:"peakRssBytes"`
	IdleRSSBytes *int64 `json:"idleRssBytes"`
}

type parityReport struct {
	Counts struct {
		Scope struct {
			Files int `json:"files"`
		} `json:"scope"`
	} `json:"counts"`
	Ours   parityServer `json:"ours"`
	Theirs parityServer `json:"theirs"`
}

func measureMemory(ctx context.Context, bins binaries, repos []corpus.Repo, cache, nixpkgsDir string, references []string, tmp string) ([]memoryRun, error) {
	runs := make([]memoryRun, 0, len(repos)*len(references))
	for _, repo := range repos {
		for _, reference := range references {
			run, err := compareMemory(ctx, bins, repo, repo.Dir(cache), nixpkgsDir, reference, tmp)
			if err != nil {
				return nil, fmt.Errorf("comparing the memory of %s with %s on %s: %w", oursName, reference, repo.Name, err)
			}
			runs = append(runs, run)
		}
	}
	return runs, nil
}

func compareMemory(ctx context.Context, bins binaries, repo corpus.Repo, dir, nixpkgsDir, reference, tmp string) (memoryRun, error) {
	prefix := filepath.Join(tmp, repo.Name+"-"+reference)
	cmd := exec.CommandContext(ctx, bins.nixparity, "compare", "-ours", bins.ours, "-theirs", reference, "-out", prefix, dir) //nolint:gosec // G204: no shell is involved; the programs were built by this command and the arguments are paths and a server name
	cmd.Env = append(os.Environ(), "NIX_PATH=nixpkgs="+nixpkgsDir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return memoryRun{}, fmt.Errorf("running %s: %w", nixparityName, err)
	}
	raw, err := os.ReadFile(prefix + ".json") //nolint:gosec // G304: a file nixparity wrote under a directory from os.MkdirTemp
	if err != nil {
		return memoryRun{}, fmt.Errorf("reading the %s report: %w", nixparityName, err)
	}
	var parity parityReport
	if err := json.Unmarshal(raw, &parity); err != nil {
		return memoryRun{}, fmt.Errorf("decoding the %s report %s.json: %w", nixparityName, prefix, err)
	}
	ours, err := rssOf(parity.Ours)
	if err != nil {
		return memoryRun{}, err
	}
	theirs, err := rssOf(parity.Theirs)
	if err != nil {
		return memoryRun{}, err
	}
	return memoryRun{Project: projectOf(repo), Files: parity.Counts.Scope.Files, Ours: ours, Theirs: theirs}, nil
}

func rssOf(s parityServer) (rss, error) {
	if s.PeakRSSBytes == nil || s.IdleRSSBytes == nil {
		return rss{}, fmt.Errorf("%s reports no memory for %s; it is measured only on Linux", nixparityName, s.Server.Name)
	}
	return rss{Server: s.Server, PeakBytes: *s.PeakRSSBytes, IdleBytes: *s.IdleRSSBytes, Restarts: s.Restarts}, nil
}
