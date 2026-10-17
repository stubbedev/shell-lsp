package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host"
)

func TestLoadAsParsesAFileInTheGivenLanguage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "rules.inc")
	if err := os.WriteFile(path, []byte("CFLAGS := -O2\nbuild:\n\techo $$HOME\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := New(func(string) (*File, bool) { return nil, false }, Detect)
	plain, err := w.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Native != nil {
		t.Fatalf("rules.inc loaded by name is %v, want a shell script", plain.Native.Language)
	}
	made, err := w.LoadAs(path, host.Make)
	if err != nil {
		t.Fatal(err)
	}
	if made.Native == nil || made.Native.Language != host.Make || len(made.Native.Index.Symbols) != 2 {
		t.Fatalf("rules.inc loaded as make = %+v", made.Native)
	}
	again, err := w.LoadAs(path, host.Make)
	if err != nil || again != made {
		t.Errorf("loading it as make again = %p, %v, want the cached %p", again, err, made)
	}
	if _, err := w.LoadAs(filepath.Join(dir, "missing.mk"), host.Make); err == nil {
		t.Error("LoadAs of a missing file succeeded")
	}
}
