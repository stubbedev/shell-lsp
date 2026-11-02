package nixeval

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
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
	fakeMetadata = `{"path": "/nix/store/abc-source", "locked": {"narHash": "sha256-AAAA="}}`
	fakeRef      = "path:/nix/store/abc-source?narHash=sha256-AAAA%3D"
	fakeLock     = `{"nodes": {"nixpkgs": {"locked": {"repo": "nixpkgs", "narHash": "sha256-NP="}}, "root": {"inputs": {"nixpkgs": "nixpkgs"}}}, "root": "root", "version": 7}` + "\n"
	fakeIdentity = "/nix/store/fake-nix/bin/nix"
	fakeOptions  = `{"options": [{"loc": ["a"], "type": "boolean"}], "truncated": []}`
)

type fakeNix struct {
	mu      sync.Mutex
	calls   [][]string
	replies map[string]string
	gate    chan struct{}
}

func newFakeNix(replies map[string]string) *fakeNix {
	base := map[string]string{"flake metadata": fakeMetadata, "config show": "x86_64-linux\n", "flake show": `{}`}
	maps.Copy(base, replies)
	return &fakeNix{replies: base}
}

func (f *fakeNix) setReply(command, reply string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[command] = reply
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
	gate := f.gate
	f.mu.Unlock()
	if gate != nil && command != "config show" && command != "flake metadata" {
		select {
		case <-gate:
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
	writeFile(t, filepath.Join(dir, "flake.lock"), fakeLock)
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

type testHarness struct {
	settings settings
	mu       sync.Mutex
	reports  []error
}

func newHarness(t *testing.T, run runner) *testHarness {
	t.Helper()
	h := &testHarness{}
	h.settings = settings{
		run:      run,
		identify: func() (string, error) { return fakeIdentity, nil },
		cacheDir: t.TempDir(),
		timeout:  testTimeout,
		now:      time.Now,
		report: func(err error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.reports = append(h.reports, err)
		},
	}
	return h
}

func (h *testHarness) reported() []error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.reports)
}

func (h *testHarness) client(t *testing.T) *Client {
	t.Helper()
	c, err := newClient(h.settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func testClient(t *testing.T, f *fakeNix) *Client {
	t.Helper()
	return newHarness(t, f.run).client(t)
}

func freshOf[T any](answer Answer[T], err error) (T, error) {
	value, stale := answer.Get()
	if err != nil {
		return value, err
	}
	if stale != nil {
		return value, fmt.Errorf("the answer rests on %s", stale.Basis)
	}
	return value, nil
}

func settled[T any](t *testing.T, call func() (Answer[T], error)) T {
	t.Helper()
	answer, err := call()
	if err != nil {
		t.Fatal(err)
	}
	value, stale := answer.Get()
	if stale == nil {
		return value
	}
	<-stale.Refresh.Done()
	if err := stale.Refresh.Err(); err != nil {
		t.Fatal(err)
	}
	value, err = freshOf(call())
	if err != nil {
		t.Fatal(err)
	}
	return value
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
	c := testClient(t, f)
	got, err := freshOf(c.Outputs(t.Context(), flakeDir(t)))
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

func TestAChangedFlakeIsReevaluatedWithoutBeingTold(t *testing.T) {
	t.Parallel()

	f := newFakeNix(nil)
	c := testClient(t, f)
	dir := flakeDir(t)
	if err := os.Mkdir(filepath.Join(dir, "modules"), 0o700); err != nil {
		t.Fatal(err)
	}
	module := filepath.Join(dir, "modules", "found-by-read-dir.nix")
	writeFile(t, module, "{ }\n")
	outputs := func() {
		t.Helper()
		settled(t, func() (Answer[[]Output], error) { return c.Outputs(t.Context(), dir) })
	}
	outputs()
	outputs()
	if got := f.count("flake show"); got != 1 {
		t.Fatalf("flake show ran %d times for an unchanged flake, want once", got)
	}
	changes := []func(){
		func() { writeFile(t, filepath.Join(dir, "flake.lock"), "{ }\n") },
		func() { writeFile(t, filepath.Join(dir, "flake.nix"), "{ outputs = _: { a = 1; }; }\n") },
		func() { writeFile(t, module, "{ options = { }; }\n") },
		func() { writeFile(t, filepath.Join(dir, "modules", "added.nix"), "{ }\n") },
		func() { writeFile(t, filepath.Join(dir, "hosts.json"), "{}\n") },
		func() {
			if err := os.Remove(filepath.Join(dir, "flake.lock")); err != nil {
				t.Fatal(err)
			}
		},
	}
	for i, change := range changes {
		change()
		outputs()
		if got, want := f.count("flake show"), i+2; got != want {
			t.Errorf("flake show ran %d times after change %d, want %d", got, i, want)
		}
		if got, want := f.count("metadata"), i+2; got != want {
			t.Errorf("flake metadata ran %d times after change %d, want one per flake state", got, i)
		}
	}
}

func TestKeySeparatesAnEmptyLockFromNone(t *testing.T) {
	t.Parallel()

	c := testClient(t, newFakeNix(nil))
	dir := flakeDir(t)
	key := func() contentKey {
		t.Helper()
		flake, err := c.inspect(t.Context(), dir)
		if err != nil {
			t.Fatal(err)
		}
		return flake.local
	}
	writeFile(t, filepath.Join(dir, "flake.lock"), "")
	empty := key()
	if err := os.Remove(filepath.Join(dir, "flake.lock")); err != nil {
		t.Fatal(err)
	}
	if key() == empty {
		t.Error("an empty flake.lock has the key of a missing one")
	}
	if digestOf("ab", "c") == digestOf("a", "bc") {
		t.Error("the digest does not separate its fields")
	}
}

func TestKeyFollowsTheNixBinaryAndNotTheDirectory(t *testing.T) {
	t.Parallel()

	h := newHarness(t, newFakeNix(nil).run)
	c := h.client(t)
	first, err := c.inspect(t.Context(), flakeDir(t))
	if err != nil {
		t.Fatal(err)
	}
	clone, err := c.inspect(t.Context(), flakeDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if first.local != clone.local {
		t.Error("two directories with the same content have different keys")
	}
	h.settings.identify = func() (string, error) { return "/nix/store/other-nix/bin/nix", nil }
	upgraded, err := h.client(t).inspect(t.Context(), flakeDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.local == first.local {
		t.Error("another nix binary shares the key of the first")
	}
}

func TestConcurrentIdenticalRequestsShareOneProcess(t *testing.T) {
	t.Parallel()

	dir := flakeDir(t)
	synctest.Test(t, func(t *testing.T) {
		f := newFakeNix(nil)
		f.gate = make(chan struct{})
		c := testClient(t, f)
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				if _, err := freshOf(c.Outputs(t.Context(), dir)); err != nil {
					t.Error(err)
				}
			})
		}
		synctest.Wait()
		close(f.gate)
		wg.Wait()
		if got := f.count("flake show"); got != 1 {
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
		c := newHarness(t, func(ctx context.Context, _ string, args []string) ([]byte, error) {
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
		}).client(t)
		var wg sync.WaitGroup
		for _, parent := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
			wg.Go(func() {
				if _, err := freshOf(c.NixpkgsAttrs(t.Context(), dir, []string{parent}, "")); err != nil {
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
	c := testClient(t, f)
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
	if _, err := testClient(t, newFakeNix(nil)).Outputs(t.Context(), dir); err == nil {
		t.Error("Outputs() = nil error for a directory named flake.nix")
	}
}

func TestErrorsNameTheAttemptAndTheFlake(t *testing.T) {
	t.Parallel()

	dir := flakeDir(t)
	stderr := errors.New("nix said no")
	c := newHarness(t, func(context.Context, string, []string) ([]byte, error) {
		return nil, &CommandError{Command: "nix flake metadata", Stderr: "details", Err: stderr}
	}).client(t)
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

	h := newHarness(t, execRunner("shell-lsp-no-such-nix"))
	h.settings.identify = nixIdentity("shell-lsp-no-such-nix")
	_, err := h.client(t).OptionSources(t.Context(), flakeDir(t))
	if _, ok := errors.AsType[*NixNotFoundError](err); !ok {
		t.Errorf("error = %v, want *NixNotFoundError", err)
	}
}

func TestNixpkgsAttrsFiltersOneCachedListing(t *testing.T) {
	t.Parallel()

	f := newFakeNix(map[string]string{"eval attrs": `["rich", "requests", "absl"]`})
	c := testClient(t, f)
	dir := flakeDir(t)
	got, err := freshOf(c.NixpkgsAttrs(t.Context(), dir, []string{"python3Packages"}, "re"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"requests"}; !slices.Equal(got, want) {
		t.Errorf("NixpkgsAttrs(re) = %q, want %q", got, want)
	}
	got, err = freshOf(c.NixpkgsAttrs(t.Context(), dir, []string{"python3Packages"}, ""))
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

func TestNixpkgsAnswersAreSharedByEveryFlakeLockingTheSameInput(t *testing.T) {
	t.Parallel()

	f := newFakeNix(map[string]string{"eval attrs": `["hello"]`, "eval package": `{"name": "hello-1"}`})
	c := testClient(t, f)
	first, second := flakeDir(t), flakeDir(t)
	writeFile(t, filepath.Join(second, "flake.nix"), "{ outputs = _: { other = true; }; }\n")
	for _, dir := range []string{first, second} {
		if _, err := freshOf(c.NixpkgsAttrs(t.Context(), dir, nil, "")); err != nil {
			t.Fatal(err)
		}
		pkg, err := freshOf(c.NixpkgsPackage(t.Context(), dir, []string{"hello"}))
		if err != nil || pkg == nil || pkg.Name != "hello-1" {
			t.Fatalf("NixpkgsPackage() = %+v, %v", pkg, err)
		}
	}
	writeFile(t, filepath.Join(first, "module.nix"), "{ }\n")
	if _, err := freshOf(c.NixpkgsAttrs(t.Context(), first, nil, "")); err != nil {
		t.Errorf("an edit of a local file made the nixpkgs answer stale: %v", err)
	}
	if count := f.count("eval"); count != 2 {
		t.Errorf("nix eval ran %d times, want one attrs and one package across flakes", count)
	}
}

func TestNixpkgsWithoutALockedInputIsAPreciseError(t *testing.T) {
	t.Parallel()

	f := newFakeNix(nil)
	c := testClient(t, f)
	dir := flakeDir(t)
	writeFile(t, filepath.Join(dir, "flake.lock"), `{"nodes": {"root": {}}, "root": "root", "version": 7}`)
	if _, err := c.NixpkgsAttrs(t.Context(), dir, nil, ""); !errors.Is(err, ErrNoNixpkgsInput) {
		t.Errorf("error = %v, want ErrNoNixpkgsInput", err)
	}
	if _, err := c.NixpkgsPackage(t.Context(), dir, []string{"hello"}); !errors.Is(err, ErrNoNixpkgsInput) {
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
		"eval options": fakeOptions,
	})
	c := testClient(t, f)
	dir := flakeDir(t)
	calls := []func() error{
		func() error {
			_, err := freshOf(c.NixpkgsAttrs(t.Context(), dir, []string{hostile}, hostile))
			return err
		},
		func() error {
			_, err := freshOf(c.NixpkgsPackage(t.Context(), dir, []string{hostile}))
			return err
		},
		func() error {
			_, err := freshOf(c.Options(t.Context(), dir, OptionSource{Kind: SourceNixOS, Name: hostile}))
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

	f := newFakeNix(map[string]string{"eval options": fakeOptions})
	c := testClient(t, f)
	dir := flakeDir(t)
	sources := []OptionSource{
		{Kind: SourceNixOS, Name: "x"},
		{Kind: SourceHomeManager, Name: "x"},
		{Kind: SourceFlakeParts},
	}
	for range 2 {
		for _, source := range sources {
			tree, err := freshOf(c.Options(t.Context(), dir, source))
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

func TestARestartAnswersFromDiskWithoutEvaluating(t *testing.T) {
	t.Parallel()

	f := newFakeNix(map[string]string{
		"eval options": fakeOptions,
		"eval sources": `{"nixos": ["box"], "home-manager": [], "flake-parts": false}`,
		"eval attrs":   `["hello"]`,
		"eval package": `{"name": "hello-1"}`,
	})
	h := newHarness(t, f.run)
	dir := flakeDir(t)
	source := OptionSource{Kind: SourceNixOS, Name: "box"}
	ask := func(c *Client) {
		t.Helper()
		if _, err := freshOf(c.Outputs(t.Context(), dir)); err != nil {
			t.Fatal(err)
		}
		if _, err := freshOf(c.OptionSources(t.Context(), dir)); err != nil {
			t.Fatal(err)
		}
		tree, err := freshOf(c.Options(t.Context(), dir, source))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := tree.Lookup([]string{"a"}); !ok {
			t.Error("the tree lacks a")
		}
		if _, err := freshOf(c.NixpkgsAttrs(t.Context(), dir, nil, "")); err != nil {
			t.Fatal(err)
		}
		if pkg, err := freshOf(c.NixpkgsPackage(t.Context(), dir, []string{"hello"})); err != nil || pkg == nil {
			t.Fatalf("NixpkgsPackage() = %+v, %v", pkg, err)
		}
	}
	first := h.client(t)
	ask(first)
	first.Close()
	before := f.count("eval") + f.count("flake")
	ask(h.client(t))
	if got := f.count("eval") + f.count("flake"); got != before {
		t.Errorf("the restarted client ran %d evaluating commands", got-before)
	}
	if reports := h.reported(); len(reports) != 0 {
		t.Errorf("reported %v", reports)
	}
}

func TestARequestDeadlineCannotStopTheEvaluation(t *testing.T) {
	t.Parallel()

	dir := flakeDir(t)
	synctest.Test(t, func(t *testing.T) {
		f := newFakeNix(map[string]string{"eval options": fakeOptions})
		f.gate = make(chan struct{})
		h := newHarness(t, f.run)
		c := h.client(t)
		source := OptionSource{Kind: SourceNixOS, Name: "box"}
		short, cancel := context.WithTimeout(t.Context(), time.Millisecond)
		defer cancel()
		_, err := c.Options(short, dir, source)
		pending, ok := errors.AsType[*NotReadyError](err)
		if !ok || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v, want *NotReadyError wrapping the deadline", err)
		}
		if !strings.Contains(err.Error(), dir) {
			t.Errorf("error = %v, want it to name the flake", err)
		}
		select {
		case <-pending.Refresh.Done():
			t.Error("the evaluation finished before it was released")
		default:
		}
		if err := pending.Refresh.Err(); err != nil {
			t.Errorf("Err() before Done = %v", err)
		}
		close(f.gate)
		<-pending.Refresh.Done()
		if err := pending.Refresh.Err(); err != nil {
			t.Fatal(err)
		}
		if _, err := freshOf(c.Options(t.Context(), dir, source)); err != nil {
			t.Fatal(err)
		}
		if got := f.count("eval"); got != 1 {
			t.Errorf("nix eval ran %d times, want the one that outlived the request", got)
		}
	})
}

func TestAnEarlierStateIsServedAtOnceAndMarkedStale(t *testing.T) {
	t.Parallel()

	dir := flakeDir(t)
	synctest.Test(t, func(t *testing.T) {
		f := newFakeNix(map[string]string{"eval options": fakeOptions})
		c := testClient(t, f)
		source := OptionSource{Kind: SourceHomeManager, Name: "me"}
		if _, err := freshOf(c.Options(t.Context(), dir, source)); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "module.nix"), "{ }\n")
		f.setReply("eval options", `{"options": [{"loc": ["b"], "type": "boolean"}], "truncated": []}`)
		f.mu.Lock()
		f.gate = make(chan struct{})
		gate := f.gate
		f.mu.Unlock()
		answer, err := c.Options(t.Context(), dir, source)
		if err != nil {
			t.Fatal(err)
		}
		tree, stale := answer.Get()
		if stale == nil || stale.Basis != BasisEarlierState {
			t.Fatalf("stale = %+v, want an earlier state", stale)
		}
		if _, ok := tree.Lookup([]string{"a"}); !ok {
			t.Error("the stale tree lacks the earlier option")
		}
		select {
		case <-stale.Refresh.Done():
			t.Fatal("the refresh finished before it was released")
		default:
		}
		close(gate)
		<-stale.Refresh.Done()
		if err := stale.Refresh.Err(); err != nil {
			t.Fatal(err)
		}
		tree, err = freshOf(c.Options(t.Context(), dir, source))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := tree.Lookup([]string{"b"}); !ok {
			t.Error("the refreshed tree lacks the new option")
		}
	})
}

func TestAFailedRefreshKeepsServingTheEarlierStateAndSaysWhy(t *testing.T) {
	t.Parallel()

	f := newFakeNix(map[string]string{"eval options": fakeOptions})
	h := newHarness(t, f.run)
	c := h.client(t)
	dir := flakeDir(t)
	source := OptionSource{Kind: SourceHomeManager, Name: "me"}
	if _, err := freshOf(c.Options(t.Context(), dir, source)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "module.nix"), "syntax error\n")
	f.setReply("eval options", "not json")
	answer, err := c.Options(t.Context(), dir, source)
	if err != nil {
		t.Fatal(err)
	}
	tree, stale := answer.Get()
	if stale == nil {
		t.Fatal("the answer is not marked stale")
	}
	if _, ok := tree.Lookup([]string{"a"}); !ok {
		t.Error("the stale tree lacks the earlier option")
	}
	<-stale.Refresh.Done()
	if err := stale.Refresh.Err(); err == nil {
		t.Error("the failed refresh reports no error")
	}
	again, err := c.Options(t.Context(), dir, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, stillStale := again.Get(); stillStale == nil {
		t.Error("the answer after a failed refresh is not marked stale")
	}
}

func TestWarmFillsTheCacheAndSharesTheNixpkgsTreeWithOtherFlakes(t *testing.T) {
	t.Parallel()

	f := newFakeNix(map[string]string{
		"eval sources": `{"nixos": ["box"], "home-manager": ["me"], "flake-parts": false}`,
		"eval options": fakeOptions,
		"eval attrs":   `["hello"]`,
		"eval base":    `{"options": [{"loc": ["fromBase"], "type": "boolean"}], "truncated": []}`,
	})
	h := newHarness(t, f.run)
	c := h.client(t)
	dir := flakeDir(t)
	warmed := c.Warm(dir)
	<-warmed.Done()
	if err := warmed.Err(); err != nil {
		t.Fatal(err)
	}
	counted := f.count("eval")
	box := OptionSource{Kind: SourceNixOS, Name: "box"}
	if _, err := freshOf(c.Options(t.Context(), dir, box)); err != nil {
		t.Fatal(err)
	}
	if _, err := freshOf(c.NixpkgsAttrs(t.Context(), dir, nil, "")); err != nil {
		t.Fatal(err)
	}
	if got := f.count("eval"); got != counted {
		t.Errorf("requests after Warm ran %d evaluations", got-counted)
	}

	other := flakeDir(t)
	writeFile(t, filepath.Join(other, "flake.nix"), "{ outputs = _: { other = true; }; }\n")
	f.mu.Lock()
	gate := make(chan struct{})
	f.gate = gate
	f.mu.Unlock()
	answer, err := c.Options(t.Context(), other, box)
	if err != nil {
		t.Fatal(err)
	}
	tree, stale := answer.Get()
	if stale == nil || stale.Basis != BasisLockedNixpkgs {
		t.Fatalf("stale = %+v, want the locked nixpkgs", stale)
	}
	if _, ok := tree.Lookup([]string{"fromBase"}); !ok {
		t.Error("the shared tree lacks the option of the locked nixpkgs")
	}
	close(gate)
	<-stale.Refresh.Done()
	if err := stale.Refresh.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestWarmOfAFlakeWithoutNixpkgsIsNotAFailure(t *testing.T) {
	t.Parallel()

	f := newFakeNix(map[string]string{"eval sources": `{"nixos": [], "home-manager": [], "flake-parts": false}`})
	c := testClient(t, f)
	dir := flakeDir(t)
	if err := os.Remove(filepath.Join(dir, "flake.lock")); err != nil {
		t.Fatal(err)
	}
	warmed := c.Warm(dir)
	<-warmed.Done()
	if err := warmed.Err(); err != nil {
		t.Error(err)
	}
}

func TestWarmStaysOutOfTheWayAndARequestPromotesIt(t *testing.T) {
	t.Parallel()

	dir := flakeDir(t)
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var running, peak atomic.Int32
		c := newHarness(t, func(ctx context.Context, _ string, args []string) ([]byte, error) {
			switch commandName(args) {
			case "nix flake metadata":
				return []byte(fakeMetadata), nil
			case "nix config show":
				return []byte("x86_64-linux\n"), nil
			case "nix flake show":
				return []byte(`{}`), nil
			}
			peak.Store(max(peak.Load(), running.Add(1)))
			defer running.Add(-1)
			select {
			case <-release:
				return []byte(`{"nixos": [], "home-manager": [], "flake-parts": false}`), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}).client(t)
		flake, err := c.inspect(t.Context(), dir)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Go(func() {
			if _, err := settle(t.Context(), c, c.sourcesQuery(flake)); err != nil {
				t.Error(err)
			}
		})
		c.lowSlots <- struct{}{}
		synctest.Wait()
		if got := running.Load(); got != 0 {
			t.Errorf("%d background evaluations ran with the background slot taken", got)
		}
		wg.Go(func() {
			if _, err := freshOf(c.OptionSources(t.Context(), dir)); err != nil {
				t.Error(err)
			}
		})
		synctest.Wait()
		if got := running.Load(); got != 1 {
			t.Errorf("%d evaluations ran after a request joined, want the promoted one", got)
		}
		close(release)
		wg.Wait()
		<-c.lowSlots
	})
}

func TestCorruptAndOldEntriesAreReportedAndRecomputed(t *testing.T) {
	t.Parallel()

	damages := map[string]func([]byte) []byte{
		"truncated":   func(data []byte) []byte { return data[:len(data)/2] },
		"flipped bit": func(data []byte) []byte { data[len(data)-1] ^= 1; return data },
		"old format":  func(data []byte) []byte { data[len(storeMagic)] = storeVersion + 1; return data },
		"foreign":     func([]byte) []byte { return []byte("not a cache entry") },
	}
	for name, damage := range damages {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakeNix(map[string]string{"eval options": fakeOptions})
			h := newHarness(t, f.run)
			dir := flakeDir(t)
			source := OptionSource{Kind: SourceHomeManager, Name: "me"}
			first := h.client(t)
			if _, err := freshOf(first.Options(t.Context(), dir, source)); err != nil {
				t.Fatal(err)
			}
			first.Close()
			entries, err := filepath.Glob(filepath.Join(h.settings.cacheDir, "*"+entrySuffix))
			if err != nil || len(entries) == 0 {
				t.Fatalf("Glob() = %q, %v", entries, err)
			}
			for _, entry := range entries {
				data, err := os.ReadFile(entry)
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, entry, string(damage(data)))
			}
			tree, err := freshOf(h.client(t).Options(t.Context(), dir, source))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := tree.Lookup([]string{"a"}); !ok {
				t.Error("the recomputed tree lacks a")
			}
			if got := f.count("eval"); got != 2 {
				t.Errorf("nix eval ran %d times, want a recomputation", got)
			}
			if len(h.reported()) == 0 {
				t.Error("the damaged entry was not reported")
			}
		})
	}
}

func TestAnUnwritableCacheIsReportedAndStillAnswers(t *testing.T) {
	t.Parallel()

	f := newFakeNix(nil)
	h := newHarness(t, f.run)
	c := h.client(t)
	if err := os.RemoveAll(h.settings.cacheDir); err != nil {
		t.Fatal(err)
	}
	if _, err := freshOf(c.Outputs(t.Context(), flakeDir(t))); err != nil {
		t.Fatal(err)
	}
	if len(h.reported()) == 0 {
		t.Error("the failed write was not reported")
	}
}

func TestNewPutsTheCacheUnderTheUserCacheDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	t.Setenv("HOME", root)
	c, err := New(func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if info, err := os.Stat(c.disk.dir); err != nil || !info.IsDir() {
		t.Errorf("cache directory: %v, %v", info, err)
	}
	if !strings.HasPrefix(c.disk.dir, root) {
		t.Errorf("cache directory %s is outside %s", c.disk.dir, root)
	}
}

func TestClosedClientRefusesWork(t *testing.T) {
	t.Parallel()

	c := testClient(t, newFakeNix(nil))
	c.Close()
	if _, err := c.Outputs(t.Context(), flakeDir(t)); !errors.Is(err, ErrClosed) {
		t.Errorf("Outputs() error = %v, want closed", err)
	}
	warmed := c.Warm(flakeDir(t))
	<-warmed.Done()
	if !errors.Is(warmed.Err(), ErrClosed) {
		t.Errorf("Warm() error = %v, want closed", warmed.Err())
	}
}

func TestACancelledRequestStartsNoWork(t *testing.T) {
	t.Parallel()

	f := newFakeNix(nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := testClient(t, f).Outputs(ctx, flakeDir(t)); !errors.Is(err, context.Canceled) {
		t.Errorf("Outputs() error = %v, want cancelled", err)
	}
	if got := f.count(""); got != 0 {
		t.Errorf("nix ran %d times for a cancelled request", got)
	}
}

func evalExpr(args []string) string {
	i := slices.Index(args, "--expr")
	if i < 0 {
		return ""
	}
	return args[i+1]
}

func exitFailure(t *testing.T) error {
	t.Helper()
	err := exec.CommandContext(t.Context(), "sh", "-c", "exit 1").Run()
	if err == nil {
		t.Fatal("the failing command succeeded")
	}
	return &CommandError{Command: "nix eval", Stderr: "error: attribute 'pkgs' missing", Err: err}
}

func TestOptionsSurviveAValueThatStopsTheEvaluator(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		failures int
		rendered string
	}{
		{"everything renders", 0, "all"},
		{"a deferred module cannot render", 1, "outside-deferred"},
		{"a submodule cannot render", 2, "top-level"},
		{"a top-level option cannot render", 3, "nothing"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			failure := exitFailure(t)
			f := newFakeNix(map[string]string{"eval sources": `{"nixos": ["box"], "home-manager": [], "flake-parts": false}`, "eval options": fakeOptions})
			var mu sync.Mutex
			var rendered []string
			h := newHarness(t, func(ctx context.Context, dir string, args []string) ([]byte, error) {
				if expr := evalExpr(args); strings.Contains(expr, ").options {") {
					mu.Lock()
					defer mu.Unlock()
					if len(rendered) < tt.failures {
						rendered = append(rendered, expr)
						return nil, failure
					}
					rendered = append(rendered, expr)
				}
				return f.run(ctx, dir, args)
			})
			c := h.client(t)
			dir := flakeDir(t)
			tree := settled(t, func() (Answer[*OptionTree], error) {
				return c.Options(t.Context(), dir, OptionSource{Kind: SourceNixOS, Name: "box"})
			})
			if _, ok := tree.Lookup([]string{"a"}); !ok {
				t.Error("the option tree lost its option")
			}
			mu.Lock()
			defer mu.Unlock()
			if len(rendered) != tt.failures+1 || !strings.Contains(rendered[tt.failures], `rendering = "`+tt.rendered+`"`) {
				t.Errorf("%d attempts, the last being %q, want %d ending with rendering %q", len(rendered), rendered[len(rendered)-1], tt.failures+1, tt.rendered)
			}
			if got := len(h.reported()); (got > 0) != (tt.failures > 0) {
				t.Errorf("reports = %v after %d failures", h.reported(), tt.failures)
			}
		})
	}
}

func TestOptionsReportTheLastFailureWhenNothingRenders(t *testing.T) {
	t.Parallel()

	failure := exitFailure(t)
	f := newFakeNix(nil)
	attempts := atomic.Int32{}
	h := newHarness(t, func(ctx context.Context, dir string, args []string) ([]byte, error) {
		if strings.Contains(evalExpr(args), ").options {") {
			attempts.Add(1)
			return nil, failure
		}
		return f.run(ctx, dir, args)
	})
	c := h.client(t)
	_, err := c.Options(t.Context(), flakeDir(t), OptionSource{Kind: SourceNixOS, Name: "box"})
	if err == nil || !strings.Contains(err.Error(), "attribute 'pkgs' missing") {
		t.Errorf("Options() error = %v", err)
	}
	if got := attempts.Load(); got != int32(len(optionRenderings)) {
		t.Errorf("%d attempts, want one per rendering", got)
	}
}

func TestOptionsDoNotRetryWhenNixCannotRun(t *testing.T) {
	t.Parallel()

	f := newFakeNix(nil)
	attempts := atomic.Int32{}
	h := newHarness(t, func(ctx context.Context, dir string, args []string) ([]byte, error) {
		if strings.Contains(evalExpr(args), ").options {") {
			attempts.Add(1)
			return nil, &NixNotFoundError{Err: exec.ErrNotFound}
		}
		return f.run(ctx, dir, args)
	})
	c := h.client(t)
	if _, err := c.Options(t.Context(), flakeDir(t), OptionSource{Kind: SourceNixOS, Name: "box"}); err == nil {
		t.Error("Options() succeeded without nix")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("%d attempts, want 1", got)
	}
}
