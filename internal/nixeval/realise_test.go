package nixeval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type realiseFixture struct {
	dir      string
	store    string
	input    string
	file     string
	f        *fakeNix
	realises func() error
}

func newRealiseFixture(t *testing.T) *realiseFixture {
	t.Helper()
	store := t.TempDir()
	r := &realiseFixture{
		dir:   flakeDir(t),
		store: store,
		input: filepath.Join(store, "input-source"),
		realises: func() error {
			return nil
		},
	}
	r.file = filepath.Join(r.input, "mod.nix")
	writeFile(t, filepath.Join(r.dir, "flake.lock"), `{"root": "root", "version": 7, "nodes": {
  "root": {"inputs": {"dep": "dep_2", "shared": ["dep", "inner"]}},
  "dep_2": {"inputs": {"inner": "inner"}, "locked": {"type": "tarball", "url": "file:///dep.tar", "narHash": "sha256-D="}},
  "inner": {"locked": {"type": "github", "owner": "o", "repo": "r", "rev": "abc", "narHash": "sha256-I="}}
}}`+"\n")
	tree, err := json.Marshal(sourceTree{
		Path: filepath.Join(store, "own-source"),
		Inputs: map[string]sourceTree{
			"dep": {
				Path:   filepath.Join(store, "dep-source"),
				Inputs: map[string]sourceTree{"inner": {Path: r.input}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.f = newFakeNix(map[string]string{"flake archive": string(tree), "eval inputRef": `"github:o/r/abc"`})
	return r
}

func (r *realiseFixture) run(ctx context.Context, dir string, args []string) ([]byte, error) {
	out, err := r.f.run(ctx, dir, args)
	if err == nil && slices.Contains(args, "github:o/r/abc") {
		if realised := r.realises(); realised != nil {
			return nil, realised
		}
	}
	return out, err
}

func TestAFileOnDiskIsNotRealisedAndStartsNoProcess(t *testing.T) {
	t.Parallel()

	r := newRealiseFixture(t)
	c := newHarness(t, r.run).client(t)
	present := filepath.Join(t.TempDir(), "present.nix")
	writeFile(t, present, "{ }\n")
	if err := c.RealiseFile(t.Context(), r.dir, present); err != nil {
		t.Fatal(err)
	}
	if len(r.f.calls) != 0 {
		t.Errorf("processes started: %q", r.f.calls)
	}
}

func TestOnlyTheInputHoldingTheFileIsFetched(t *testing.T) {
	t.Parallel()

	r := newRealiseFixture(t)
	r.realises = func() error {
		if err := os.Mkdir(r.input, 0o700); err != nil {
			return err
		}
		return os.WriteFile(r.file, []byte("{ }\n"), 0o600)
	}
	c := newHarness(t, r.run).client(t)
	if err := c.RealiseFile(t.Context(), r.dir, r.file); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.file); err != nil {
		t.Error(err)
	}
	r.f.mu.Lock()
	defer r.f.mu.Unlock()
	var fetched []string
	for _, call := range r.f.calls {
		if commandName(call) == "nix flake prefetch" {
			fetched = append(fetched, call[2])
		}
		if commandName(call) == "nix eval" && !strings.Contains(call[slices.Index(call, "--expr")+1], `sha256-I=`) {
			t.Errorf("the reference was built from another input: %q", call)
		}
	}
	if want := []string{".", "github:o/r/abc"}; !slices.Equal(fetched, want) {
		t.Errorf("prefetched %q, want the flake and the one input %q", fetched, want)
	}
}

func TestAFileInTheFlakeItselfIsAFreshSnapshot(t *testing.T) {
	t.Parallel()

	r := newRealiseFixture(t)
	own := filepath.Join(r.store, "own-source")
	file := filepath.Join(own, "flake.nix")
	prefetches := 0
	c := newHarness(t, func(ctx context.Context, dir string, args []string) ([]byte, error) {
		if commandName(args) == "nix flake prefetch" {
			prefetches++
			if prefetches == 2 {
				if err := os.Mkdir(own, 0o700); err != nil {
					return nil, err
				}
				if err := os.WriteFile(file, []byte("{ }\n"), 0o600); err != nil {
					return nil, err
				}
			}
		}
		return r.f.run(ctx, dir, args)
	}).client(t)
	if err := c.RealiseFile(t.Context(), r.dir, file); err != nil {
		t.Fatal(err)
	}
	if prefetches != 2 {
		t.Errorf("the flake was prefetched %d times, want again after finding its snapshot gone", prefetches)
	}
}

func TestAFileNoInputHoldsIsAPreciseAnswerFromOneAttempt(t *testing.T) {
	t.Parallel()

	r := newRealiseFixture(t)
	c := newHarness(t, r.run).client(t)
	const nowhere = "/nix/store/elsewhere-source/x.nix"
	for range 3 {
		err := c.RealiseFile(t.Context(), r.dir, nowhere)
		unavailable, ok := errors.AsType[*FileUnavailableError](err)
		if !ok || !errors.Is(err, errNotInFlake) || unavailable.File != nowhere || !strings.Contains(err.Error(), nowhere) {
			t.Errorf("error = %v, want *FileUnavailableError naming %s", err, nowhere)
		}
	}
	if got := r.f.count("flake archive"); got != 1 {
		t.Errorf("flake archive ran %d times, want one attempt", got)
	}
}

func TestAFailedFetchIsAPreciseAnswerAndIsNotRetriedAtOnce(t *testing.T) {
	t.Parallel()

	r := newRealiseFixture(t)
	failure := errors.New("the network is down")
	r.realises = func() error { return failure }
	now := time.Now()
	h := newHarness(t, r.run)
	h.settings.now = func() time.Time { return now }
	c := h.client(t)
	for range 3 {
		err := c.RealiseFile(t.Context(), r.dir, r.file)
		if _, ok := errors.AsType[*FileUnavailableError](err); !ok || !errors.Is(err, failure) {
			t.Errorf("error = %v, want *FileUnavailableError wrapping %v", err, failure)
		}
	}
	if got := fetchesOf(r.f, "github:o/r/abc"); got != 1 {
		t.Errorf("the input was fetched %d times, want one attempt", got)
	}
}

func fetchesOf(f *fakeNix, ref string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, call := range f.calls {
		if commandName(call) == "nix flake prefetch" && slices.Contains(call, ref) {
			n++
		}
	}
	return n
}

func TestAFetchThatLeavesNoFileIsNotReportedAsSuccess(t *testing.T) {
	t.Parallel()

	r := newRealiseFixture(t)
	err := newHarness(t, r.run).client(t).RealiseFile(t.Context(), r.dir, r.file)
	if _, ok := errors.AsType[*FileUnavailableError](err); !ok || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error = %v, want *FileUnavailableError wrapping the missing file", err)
	}
}

func TestAHungFetchIsOneShortAttempt(t *testing.T) {
	t.Parallel()

	r := newRealiseFixture(t)
	synctest.Test(t, func(t *testing.T) {
		c := newHarness(t, func(ctx context.Context, dir string, args []string) ([]byte, error) {
			if slices.Contains(args, "github:o/r/abc") {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return r.f.run(ctx, dir, args)
		}).client(t)
		started := time.Now()
		err := c.RealiseFile(t.Context(), r.dir, r.file)
		if _, ok := errors.AsType[*FileUnavailableError](err); !ok || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("error = %v, want *FileUnavailableError wrapping the deadline", err)
		}
		if took := time.Since(started); took != realiseTimeout {
			t.Errorf("the attempt took %v, want %v", took, realiseTimeout)
		}
	})
}

func TestARequestThatStopsWaitingLeavesTheFetchRunning(t *testing.T) {
	t.Parallel()

	r := newRealiseFixture(t)
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		r.realises = func() error {
			<-release
			if err := os.Mkdir(r.input, 0o700); err != nil {
				return err
			}
			return os.WriteFile(r.file, []byte("{ }\n"), 0o600)
		}
		c := newHarness(t, r.run).client(t)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		err := c.RealiseFile(ctx, r.dir, r.file)
		notReady, ok := errors.AsType[*NotReadyError](err)
		if !ok {
			t.Fatalf("error = %v, want *NotReadyError", err)
		}
		close(release)
		<-notReady.Refresh.Done()
		if err := notReady.Refresh.Err(); err != nil {
			t.Fatal(err)
		}
		if err := c.RealiseFile(t.Context(), r.dir, r.file); err != nil {
			t.Error(err)
		}
	})
}

func TestDeclarationOnDiskReportsThePresentFile(t *testing.T) {
	t.Parallel()

	present := filepath.Join(t.TempDir(), "a.nix")
	writeFile(t, present, "{ }\n")
	if !(Declaration{File: present}).OnDisk() {
		t.Errorf("%s is not reported on disk", present)
	}
	if (Declaration{File: present + ".missing"}).OnDisk() {
		t.Error("a missing file is reported on disk")
	}
}

func TestOwnerNamesTheInputPathOfAFile(t *testing.T) {
	t.Parallel()

	tree := sourceTree{Path: "/s/root", Inputs: map[string]sourceTree{
		"a": {Path: "/s/a", Inputs: map[string]sourceTree{"c": {Path: "/s/c"}}},
		"b": {Path: "/s/ab"},
		"r": {Inputs: map[string]sourceTree{}},
	}}
	tests := []struct {
		file  string
		want  []string
		found bool
	}{
		{"/s/root/x.nix", []string{}, true},
		{"/s/a/x.nix", []string{"a"}, true},
		{"/s/ab/x.nix", []string{"b"}, true},
		{"/s/c", []string{"a", "c"}, true},
		{"/s/rootless/x.nix", nil, false},
		{"", nil, false},
	}
	for _, tt := range tests {
		got, found := tree.owner(tt.file)
		if found != tt.found || !slices.Equal(got, tt.want) {
			t.Errorf("owner(%q) = %q, %v, want %q, %v", tt.file, got, found, tt.want, tt.found)
		}
	}
}

func TestSnapshotCollectedWhileCachedIsResolvedAgainAndRetriedOnce(t *testing.T) {
	t.Parallel()

	failure := exitFailure(t)
	shows, prefetches, inspected := 0, 0, 0
	f := newFakeNix(nil)
	c := newHarness(t, func(ctx context.Context, dir string, args []string) ([]byte, error) {
		switch commandName(args) {
		case "nix flake show":
			shows++
			if prefetches < 2 {
				return nil, &CommandError{Command: "nix flake show", Stderr: "gone", Err: failure}
			}
		case "nix flake prefetch":
			prefetches++
		case "nix path-info":
			inspected++
			return nil, &CommandError{Command: "nix path-info", Err: failure}
		}
		return f.run(ctx, dir, args)
	}).client(t)
	if _, err := freshOf(c.Outputs(t.Context(), flakeDir(t))); err != nil {
		t.Fatal(err)
	}
	if shows != 2 || prefetches != 2 || inspected != 1 {
		t.Errorf("flake show ran %d, prefetch %d and path-info %d times, want a single retry after a single fresh resolve", shows, prefetches, inspected)
	}
}

func TestSnapshotThatStaysMissingIsTheFailureOfTheRetryAndNoLoop(t *testing.T) {
	t.Parallel()

	failure := exitFailure(t)
	shows, prefetches := 0, 0
	f := newFakeNix(nil)
	c := newHarness(t, func(ctx context.Context, dir string, args []string) ([]byte, error) {
		switch commandName(args) {
		case "nix flake show":
			shows++
			return nil, &CommandError{Command: "nix flake show", Stderr: fmt.Sprintf("attempt %d", shows), Err: failure}
		case "nix flake prefetch":
			prefetches++
		case "nix path-info":
			return nil, &CommandError{Command: "nix path-info", Err: failure}
		}
		return f.run(ctx, dir, args)
	}).client(t)
	_, err := freshOf(c.Outputs(t.Context(), flakeDir(t)))
	if err == nil || !strings.Contains(err.Error(), "attempt 2") {
		t.Errorf("error = %v, want the failure of the retry", err)
	}
	if shows != 2 || prefetches != 2 {
		t.Errorf("flake show ran %d and prefetch %d times, want two of each", shows, prefetches)
	}
}

func TestAFailedEvaluationWithItsSnapshotInPlaceKeepsItsErrorAndTheSnapshot(t *testing.T) {
	t.Parallel()

	failure := exitFailure(t)
	shows, prefetches := 0, 0
	f := newFakeNix(nil)
	c := newHarness(t, func(ctx context.Context, dir string, args []string) ([]byte, error) {
		switch commandName(args) {
		case "nix flake show":
			shows++
			return nil, &CommandError{Command: "nix flake show", Stderr: "a throw of the flake", Err: failure}
		case "nix flake prefetch":
			prefetches++
		}
		return f.run(ctx, dir, args)
	}).client(t)
	_, err := freshOf(c.Outputs(t.Context(), flakeDir(t)))
	if err == nil || !strings.Contains(err.Error(), "a throw of the flake") {
		t.Errorf("error = %v, want the failure of the evaluation", err)
	}
	if shows != 1 || prefetches != 1 {
		t.Errorf("flake show ran %d and prefetch %d times, want no retry for a store path still there", shows, prefetches)
	}
}

func TestAFailedFreshResolveExplainsWhatWasCollected(t *testing.T) {
	t.Parallel()

	failure := exitFailure(t)
	prefetches := 0
	f := newFakeNix(nil)
	c := newHarness(t, func(ctx context.Context, dir string, args []string) ([]byte, error) {
		switch commandName(args) {
		case "nix flake show", "nix path-info":
			return nil, &CommandError{Command: commandName(args), Err: failure}
		case "nix flake prefetch":
			prefetches++
			if prefetches == 2 {
				return nil, &CommandError{Command: "nix flake prefetch", Stderr: "the store is read-only", Err: failure}
			}
		}
		return f.run(ctx, dir, args)
	}).client(t)
	_, err := freshOf(c.Outputs(t.Context(), flakeDir(t)))
	for _, want := range []string{"garbage collection", "/nix/store/abc-source", "the store is read-only"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}
}

func TestDeterminateEvaluationIsGivenTheSourceTreesToNameFilesBy(t *testing.T) {
	t.Parallel()

	const trees = `{"path": "/nix/store/own-source", "inputs": {}}`
	f := newFakeNix(map[string]string{"flake archive": trees, "eval options": fakeOptions})
	h := newHarness(t, f.run)
	h.settings.version = func(context.Context) ([]byte, error) { return []byte("nix (Determinate Nix 3.24.1) 2.35.3\n"), nil }
	if _, err := freshOf(h.client(t).Options(t.Context(), flakeDir(t), OptionSource{Kind: SourceNixOS, Name: "box"})); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.ContainsFunc(f.calls, func(call []string) bool { return strings.Contains(evalExpr(call), nixString(trees)) }) {
		t.Error("the evaluation was not told the source trees")
	}
}

func TestOtherNixEvaluationNeverAsksForTheSourceTrees(t *testing.T) {
	t.Parallel()

	f := newFakeNix(map[string]string{"eval options": fakeOptions})
	if _, err := freshOf(testClient(t, f).Options(t.Context(), flakeDir(t), OptionSource{Kind: SourceNixOS, Name: "box"})); err != nil {
		t.Fatal(err)
	}
	if got := f.count("flake archive"); got != 0 {
		t.Errorf("flake archive ran %d times while evaluating", got)
	}
}

func TestAnInputReferenceThatLooksLikeAnOptionIsNeverPassedToNix(t *testing.T) {
	t.Parallel()

	r := newRealiseFixture(t)
	r.f.setReply("eval inputRef", `"--impure"`)
	c := newHarness(t, r.run).client(t)
	if err := c.RealiseFile(t.Context(), r.dir, r.file); err == nil || !strings.Contains(err.Error(), "--impure") {
		t.Errorf("error = %v, want one naming the reference", err)
	}
	if got := fetchesOf(r.f, "--impure"); got != 0 {
		t.Errorf("nix was started with the reference %d times", got)
	}
}
