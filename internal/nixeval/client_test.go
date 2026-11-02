package nixeval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

const (
	fakeMetadata = `{"path": "/nix/store/abc-source", "locked": {"narHash": "sha256-AAAA="}, "locks": {"root": "root", "nodes": {"root": {"inputs": {"nixpkgs": "nixpkgs"}}, "nixpkgs": {"locked": {"repo": "nixpkgs"}}}}}`
	fakeRef      = "path:/nix/store/abc-source?narHash=sha256-AAAA%3D"
)

type fakeNix struct {
	mu      sync.Mutex
	calls   [][]string
	replies map[string]string
	gate    chan struct{}
}

func newFakeNix(replies map[string]string) *fakeNix {
	base := map[string]string{"flake metadata": fakeMetadata, "config show": "x86_64-linux\n", "flake show": `{}`}
	for command, reply := range replies {
		base[command] = reply
	}
	return &fakeNix{replies: base}
}

func (f *fakeNix) run(ctx context.Context, _ string, args []string) ([]byte, error) {
	command := strings.TrimPrefix(commandName(args), nixProgram+" ")
	if command == "eval" {
		expr := args[slices.Index(args, "--expr")+1]
		function, _, _ := strings.Cut(strings.TrimPrefix(expr, "("+evalSource+")."), " ")
		command = "eval " + function
	}
	f.mu.Lock()
	f.calls = append(f.calls, args)
	reply, ok := f.replies[command]
	f.mu.Unlock()
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if !ok {
		return nil, errors.New("no reply for " + command)
	}
	return []byte(reply), nil
}

func (f *fakeNix) count(subcommand string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, call := range f.calls {
		if strings.Contains(commandName(call), subcommand) {
			n++
		}
	}
	return n
}

func (f *fakeNix) lastExpr() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	last := f.calls[len(f.calls)-1]
	return last[slices.Index(last, "--expr")+1]
}

func flakeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "flake.nix"), "{ outputs = _: { }; }\n")
	writeFile(t, filepath.Join(dir, "flake.lock"), "{}\n")
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func testClient(f *fakeNix) *Client {
	return newClient(f.run, testTimeout, time.Now)
}

func TestEveryEvaluatingCommandIsHardened(t *testing.T) {
	t.Parallel()

	required := [][]string{
		{"--option", "pure-eval", "true"},
		{"--option", "allow-import-from-derivation", "false"},
		{"--option", "accept-flake-config", "false"},
		{"--option", "allow-unsafe-native-code-during-evaluation", "false"},
		{"--no-update-lock-file"},
	}
	forbidden := []string{"--impure", "--no-pure-eval", "--accept-flake-config", "--no-write-lock-file", "--commit-lock-file", "--recreate-lock-file"}
	for _, args := range [][]string{metadataArgs(), showArgs(fakeRef), evalArgs("1")} {
		for _, flag := range required {
			if !containsRun(args, flag) {
				t.Errorf("%q lacks %q", args, flag)
			}
		}
		for _, flag := range forbidden {
			if slices.Contains(args, flag) {
				t.Errorf("%q passes %s", args, flag)
			}
		}
	}
}

func containsRun(args, run []string) bool {
	for i := range args {
		if i+len(run) <= len(args) && slices.Equal(args[i:i+len(run)], run) {
			return true
		}
	}
	return false
}

func TestClientEvaluatesTheLockedReference(t *testing.T) {
	t.Parallel()

	f := newFakeNix(map[string]string{"flake show": `{"packages": {"x": {"a": {"type": "derivation", "name": "a-1"}}}}`})
	c := testClient(f)
	got, err := c.Outputs(t.Context(), flakeDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "a-1" {
		t.Errorf("Outputs() = %+v", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Contains(f.calls[len(f.calls)-1], fakeRef) {
		t.Errorf("flake show was not given the locked reference: %q", f.calls[len(f.calls)-1])
	}
	if !slices.Contains(f.calls[0], ".") {
		t.Errorf("flake metadata did not resolve the working directory: %q", f.calls[0])
	}
}

func TestStaleAnswersAreNotServedAfterFlakeFilesChange(t *testing.T) {
	t.Parallel()

	f := newFakeNix(nil)
	c := testClient(f)
	dir := flakeDir(t)
	outputs := func() {
		t.Helper()
		if _, err := c.Outputs(t.Context(), dir); err != nil {
			t.Fatal(err)
		}
	}
	outputs()
	outputs()
	if got := f.count("show"); got != 1 {
		t.Fatalf("flake show ran %d times for an unchanged flake, want once", got)
	}
	writeFile(t, filepath.Join(dir, "flake.lock"), "{ }\n")
	outputs()
	if got := f.count("show"); got != 2 {
		t.Errorf("flake show ran %d times after flake.lock changed, want twice", got)
	}
	writeFile(t, filepath.Join(dir, "flake.nix"), "{ outputs = _: { a = 1; }; }\n")
	outputs()
	if got := f.count("show"); got != 3 {
		t.Errorf("flake show ran %d times after flake.nix changed, want three times", got)
	}
	if err := os.Remove(filepath.Join(dir, "flake.lock")); err != nil {
		t.Fatal(err)
	}
	outputs()
	if got := f.count("show"); got != 4 {
		t.Errorf("flake show ran %d times after flake.lock vanished, want four times", got)
	}
	if got := f.count("metadata"); got != 4 {
		t.Errorf("flake metadata ran %d times, want one per flake state", got)
	}
}

func TestFingerprintSeparatesAnEmptyLockFromNone(t *testing.T) {
	t.Parallel()

	nix := []byte("{ }")
	if fingerprintOf(nix, nil) == fingerprintOf(nix, []byte{}) {
		t.Error("an empty flake.lock has the fingerprint of a missing one")
	}
	if fingerprintOf([]byte("ab"), []byte("c")) == fingerprintOf([]byte("a"), []byte("bc")) {
		t.Error("the fingerprint does not separate flake.nix from flake.lock")
	}
}

func TestInvalidateForcesAFreshEvaluation(t *testing.T) {
	t.Parallel()

	f := newFakeNix(nil)
	c := testClient(f)
	dir := flakeDir(t)
	for range 2 {
		if _, err := c.Outputs(t.Context(), dir); err != nil {
			t.Fatal(err)
		}
		c.Invalidate(dir + string(filepath.Separator))
	}
	if got := f.count("show"); got != 2 {
		t.Errorf("flake show ran %d times, want one after each invalidation", got)
	}
}

func TestConcurrentIdenticalRequestsShareOneProcess(t *testing.T) {
	t.Parallel()

	dir := flakeDir(t)
	synctest.Test(t, func(t *testing.T) {
		f := newFakeNix(nil)
		f.gate = make(chan struct{})
		c := testClient(f)
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				if _, err := c.Outputs(t.Context(), dir); err != nil {
					t.Error(err)
				}
			})
		}
		synctest.Wait()
		close(f.gate)
		wg.Wait()
		if got := f.count("show"); got != 1 {
			t.Errorf("flake show ran %d times, want once", got)
		}
		if got := f.count("metadata"); got != 1 {
			t.Errorf("flake metadata ran %d times, want once", got)
		}
	})
}

func TestProcessesAreBoundedAcrossDistinctRequests(t *testing.T) {
	t.Parallel()

	dir := flakeDir(t)
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var evaluating atomic.Int32
		c := newClient(func(ctx context.Context, _ string, args []string) ([]byte, error) {
			switch commandName(args) {
			case "nix flake metadata":
				return []byte(fakeMetadata), nil
			case "nix config show":
				return []byte("x86_64-linux\n"), nil
			}
			evaluating.Add(1)
			defer evaluating.Add(-1)
			select {
			case <-release:
				return []byte(`[]`), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}, testTimeout, time.Now)
		var wg sync.WaitGroup
		for _, parent := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
			wg.Go(func() {
				if _, err := c.NixpkgsAttrs(t.Context(), dir, []string{parent}, ""); err != nil {
					t.Error(err)
				}
			})
		}
		synctest.Wait()
		if got := evaluating.Load(); got != maxConcurrent {
			t.Errorf("%d nix processes ran at once, want %d", got, maxConcurrent)
		}
		close(release)
		wg.Wait()
	})
}

func TestCallsFailWithARelativeOrMissingFlake(t *testing.T) {
	t.Parallel()

	f := newFakeNix(nil)
	c := testClient(f)
	if _, err := c.Outputs(t.Context(), "relative/dir"); err == nil || !strings.Contains(err.Error(), "relative/dir") {
		t.Errorf("relative directory error = %v", err)
	}
	empty := t.TempDir()
	_, err := c.Outputs(t.Context(), empty)
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), empty) {
		t.Errorf("missing flake.nix error = %v, want it to name %s and wrap not-exist", err, empty)
	}
	if got := f.count(""); got != 0 {
		t.Errorf("nix ran %d times without a flake", got)
	}
}

func TestFlakeFileThatIsNotRegularIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "flake.nix"), 0o700); err != nil {
		t.Fatal(err)
	}
	f := newFakeNix(nil)
	if _, err := testClient(f).Outputs(t.Context(), dir); err == nil {
		t.Error("Outputs() = nil error for a directory named flake.nix")
	}
}

func TestErrorsNameTheAttemptAndTheFlake(t *testing.T) {
	t.Parallel()

	dir := flakeDir(t)
	stderr := errors.New("nix said no")
	c := newClient(func(context.Context, string, []string) ([]byte, error) {
		return nil, &CommandError{Command: "nix flake metadata", Stderr: "details", Err: stderr}
	}, testTimeout, time.Now)
	_, err := c.Outputs(t.Context(), dir)
	if !errors.Is(err, stderr) {
		t.Errorf("error = %v, want the cause wrapped", err)
	}
	for _, want := range []string{"listing the outputs", dir, "nix flake metadata", "details"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}
}

func TestMissingNixStaysATypedErrorThroughTheClient(t *testing.T) {
	t.Parallel()

	c := newClient(execRunner("shell-lsp-no-such-nix"), testTimeout, time.Now)
	_, err := c.OptionSources(t.Context(), flakeDir(t))
	if _, ok := errors.AsType[*NixNotFoundError](err); !ok {
		t.Errorf("error = %v, want *NixNotFoundError", err)
	}
}

func TestNixpkgsAttrsFiltersOneCachedListing(t *testing.T) {
	t.Parallel()

	f := newFakeNix(map[string]string{"eval attrs": `["rich", "requests", "absl"]`})
	c := testClient(f)
	dir := flakeDir(t)
	got, err := c.NixpkgsAttrs(t.Context(), dir, []string{"python3Packages"}, "re")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"requests"}; !slices.Equal(got, want) {
		t.Errorf("NixpkgsAttrs(re) = %q, want %q", got, want)
	}
	got, err = c.NixpkgsAttrs(t.Context(), dir, []string{"python3Packages"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"absl", "requests", "rich"}; !slices.Equal(got, want) {
		t.Errorf("NixpkgsAttrs() = %q, want %q", got, want)
	}
	if count := f.count("eval"); count != 1 {
		t.Errorf("nix eval ran %d times, want once across prefixes", count)
	}
}

func TestNixpkgsWithoutALockedInputIsAPreciseError(t *testing.T) {
	t.Parallel()

	f := newFakeNix(map[string]string{"flake metadata": `{"path": "/nix/store/a", "locked": {"narHash": "sha256-A="}, "locks": {"root": "root", "nodes": {"root": {}}}}`})
	c := testClient(f)
	_, err := c.NixpkgsAttrs(t.Context(), flakeDir(t), nil, "")
	if !errors.Is(err, ErrNoNixpkgsInput) {
		t.Errorf("error = %v, want ErrNoNixpkgsInput", err)
	}
	if _, _, err := c.NixpkgsPackage(t.Context(), flakeDir(t), []string{"hello"}); !errors.Is(err, ErrNoNixpkgsInput) {
		t.Errorf("error = %v, want ErrNoNixpkgsInput", err)
	}
	if got := f.count("eval"); got != 0 {
		t.Errorf("nix eval ran %d times against no nixpkgs", got)
	}
}

func TestHostileValuesReachTheExpressionOnlyEscaped(t *testing.T) {
	t.Parallel()

	const hostile = `x"; y = ${throw "INJECTED"}; z = "`
	f := newFakeNix(map[string]string{
		"eval attrs":   `[]`,
		"eval package": `null`,
		"eval options": `[]`,
	})
	c := testClient(f)
	dir := flakeDir(t)
	calls := []func() error{
		func() error {
			_, err := c.NixpkgsAttrs(t.Context(), dir, []string{hostile}, hostile)
			return err
		},
		func() error {
			_, _, err := c.NixpkgsPackage(t.Context(), dir, []string{hostile})
			return err
		},
		func() error {
			_, err := c.Options(t.Context(), dir, OptionSource{Kind: SourceNixOS, Name: hostile})
			return err
		},
	}
	for _, call := range calls {
		if err := call(); err != nil {
			t.Fatal(err)
		}
		expr := f.lastExpr()
		if strings.Contains(expr, hostile) {
			t.Errorf("the expression embeds the raw value: %s", expr[len(evalSource):])
		}
		if !strings.Contains(expr, nixString(hostile)) {
			t.Errorf("the expression lacks the escaped value: %s", expr[len(evalSource):])
		}
	}
}

func TestOptionsAreCachedPerSource(t *testing.T) {
	t.Parallel()

	f := newFakeNix(map[string]string{"eval options": `[{"loc": ["a"], "type": "boolean"}]`})
	c := testClient(f)
	dir := flakeDir(t)
	sources := []OptionSource{
		{Kind: SourceNixOS, Name: "x"},
		{Kind: SourceHomeManager, Name: "x"},
		{Kind: SourceFlakeParts},
	}
	for range 2 {
		for _, source := range sources {
			tree, err := c.Options(t.Context(), dir, source)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := tree.Lookup([]string{"a"}); !ok {
				t.Errorf("Options(%v) lacks a", source)
			}
		}
	}
	if got := f.count("eval"); got != len(sources) {
		t.Errorf("nix eval ran %d times, want once per source", got)
	}
}
