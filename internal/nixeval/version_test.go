package nixeval

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestParseReleaseReadsEveryVariant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		output    string
		variant   nixVariant
		numbers   [versionSegments]int
		supported bool
	}{
		{"nix", "nix (Nix) 2.35.2\n", variantNix, [versionSegments]int{2, 35, 2}, true},
		{"oldest nix", "nix (Nix) 2.24.0\n", variantNix, [versionSegments]int{2, 24, 0}, true},
		{"too old nix", "nix (Nix) 2.18.9\n", variantNix, [versionSegments]int{2, 18, 9}, false},
		{"minor below", "nix (Nix) 2.23.99\n", variantNix, [versionSegments]int{2, 23, 99}, false},
		{"pre-release", "nix (Nix) 2.36.0pre20260101_abc\n", variantNix, [versionSegments]int{2, 36, 0}, true},
		{"two segments", "nix (Nix) 3.0\n", variantNix, [versionSegments]int{3, 0, 0}, true},
		{
			"lix prints its configuration after the version",
			"nix (Lix, like Nix) 2.95.4\nSystem type: x86_64-linux\nFeatures: gc\n",
			variantLix,
			[versionSegments]int{2, 95, 4},
			true,
		},
		{"lix pre-release", "nix (Lix, like Nix) 2.96.0-dev-pre20260101-abc\n", variantLix, [versionSegments]int{2, 96, 0}, true},
		{"lix numbers are not nix numbers", "nix (Lix, like Nix) 2.91.1\n", variantLix, [versionSegments]int{2, 91, 1}, false},
		{"determinate is judged by its nix", "nix (Determinate Nix 3.24.1) 2.35.3\n", variantDeterminate, [versionSegments]int{2, 35, 3}, true},
		{"old determinate", "nix (Determinate Nix 1.0.0) 2.20.0\n", variantDeterminate, [versionSegments]int{2, 20, 0}, false},
		{"another product", "nix (Other) 9.9.9\n", variantUnrecognised, [versionSegments]int{9, 9, 9}, false},
		{"not a version line", "error: nope\n", variantUnrecognised, [versionSegments]int{}, false},
		{"no number", "nix (Nix) unknown\n", variantUnrecognised, [versionSegments]int{}, false},
		{"empty", "", variantUnrecognised, [versionSegments]int{}, false},
	}
	for _, tt := range tests {
		got := parseRelease([]byte(tt.output))
		if got.variant != tt.variant || got.numbers != tt.numbers {
			t.Errorf("%s: parseRelease(%q) = %+v, want variant %d and %v", tt.name, tt.output, got, tt.variant, tt.numbers)
		}
		err := got.supported()
		if (err == nil) != tt.supported {
			t.Errorf("%s: supported() = %v, want supported = %v", tt.name, err, tt.supported)
		}
	}
}

func TestUnsupportedNixErrorStatesTheMinimumAndWhatWasFound(t *testing.T) {
	t.Parallel()

	err := parseRelease([]byte("nix (Nix) 2.18.9\nSecond line\n")).supported()
	want := `shell-lsp needs Nix 2.24 or newer, Lix 2.95 or newer, or Determinate Nix based on Nix 2.24 or newer to evaluate flakes, but nix --version reports "nix (Nix) 2.18.9"`
	if diff := cmp.Diff(want, err.Error()); diff != "" {
		t.Error(diff)
	}
}

func unsupportedHarness(t *testing.T, f *fakeNix) (*testHarness, *atomic.Int32) {
	t.Helper()
	h := newHarness(t, f.run)
	asked := new(atomic.Int32)
	h.settings.version = func(context.Context) ([]byte, error) {
		asked.Add(1)
		return []byte("nix (Nix) 2.18.9\n"), nil
	}
	return h, asked
}

func TestUnsupportedNixAnswersEveryCallFromOneVersionCheckAndRunsNothing(t *testing.T) {
	t.Parallel()

	f := newFakeNix(nil)
	h, asked := unsupportedHarness(t, f)
	c := h.client(t)
	dir := flakeDir(t)
	warmed := c.Warm(dir)
	<-warmed.Done()
	calls := map[string]error{"Warm": warmed.Err()}
	_, calls["Outputs"] = c.Outputs(t.Context(), dir)
	_, calls["OptionSources"] = c.OptionSources(t.Context(), dir)
	_, calls["Options"] = c.Options(t.Context(), dir, OptionSource{Kind: SourceNixOS, Name: "box"})
	_, calls["BaseOptions"] = c.BaseOptions(t.Context(), dir)
	_, calls["NixpkgsAttrs"] = c.NixpkgsAttrs(t.Context(), dir, nil, "")
	_, calls["NixpkgsPackage"] = c.NixpkgsPackage(t.Context(), dir, []string{"hello"})
	calls["RealiseFile"] = c.RealiseFile(t.Context(), dir, "/nix/store/absent-source/x.nix")
	for name, err := range calls {
		if _, unsupported := errors.AsType[*UnsupportedNixError](err); !unsupported {
			t.Errorf("%s error = %v, want *UnsupportedNixError", name, err)
		}
	}
	if got := asked.Load(); got != 1 {
		t.Errorf("nix --version ran %d times, want once", got)
	}
	if len(f.calls) != 0 {
		t.Errorf("processes started: %q", f.calls)
	}
}

func TestVersionIsAskedAgainOnlyForAnotherBinary(t *testing.T) {
	t.Parallel()

	f := newFakeNix(nil)
	h, asked := unsupportedHarness(t, f)
	binary := atomic.Value{}
	binary.Store("/nix/store/old-nix/bin/nix")
	h.settings.identify = func() (string, error) { return binary.Load().(string), nil } //nolint:forcetypeassert // the test stores only strings
	c := h.client(t)
	dir := flakeDir(t)
	for range 3 {
		if _, err := c.Outputs(t.Context(), dir); err == nil {
			t.Fatal("the old nix answered")
		}
	}
	binary.Store("/nix/store/new-nix/bin/nix")
	if _, err := c.Outputs(t.Context(), dir); err == nil {
		t.Fatal("the unsupported nix answered")
	}
	if got := asked.Load(); got != 2 {
		t.Errorf("nix --version ran %d times, want once per binary", got)
	}
}

func TestFailedVersionCheckIsAPreciseErrorWrappingTheCause(t *testing.T) {
	t.Parallel()

	f := newFakeNix(nil)
	h := newHarness(t, f.run)
	cause := errors.New("exec format error")
	h.settings.version = func(context.Context) ([]byte, error) { return nil, cause }
	_, err := h.client(t).Outputs(t.Context(), flakeDir(t))
	if !errors.Is(err, cause) {
		t.Errorf("error = %v, want it to wrap %v", err, cause)
	}
}

func TestASlowVersionCheckDoesNotBlockARequestAndIsNotRepeated(t *testing.T) {
	t.Parallel()

	dir := flakeDir(t)
	synctest.Test(t, func(t *testing.T) {
		f := newFakeNix(nil)
		h := newHarness(t, f.run)
		release := make(chan struct{})
		var asked atomic.Int32
		h.settings.version = func(context.Context) ([]byte, error) {
			asked.Add(1)
			<-release
			return []byte(fakeVersion), nil
		}
		c := h.client(t)
		ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
		defer cancel()
		_, err := c.Outputs(ctx, dir)
		notReady, ok := errors.AsType[*NotReadyError](err)
		if !ok {
			t.Fatalf("error = %v, want *NotReadyError", err)
		}
		close(release)
		<-notReady.Refresh.Done()
		if _, err := freshOf(c.Outputs(t.Context(), dir)); err != nil {
			t.Fatal(err)
		}
		if got := asked.Load(); got != 1 {
			t.Errorf("nix --version ran %d times, want once", got)
		}
	})
}
