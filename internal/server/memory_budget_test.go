package server

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/regularfile"
)

func nixModule(i, bytes int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "{ lib, pkgs, ... }:\nrec {\n")
	for line := 0; sb.Len() < bytes; line++ {
		fmt.Fprintf(&sb, "  attr%d_%d = lib.mkIf (pkgs.enable or false) { value = %d; };\n", i, line, line)
	}
	sb.WriteString("}\n")
	return sb.String()
}

func TestAMakefileGraphReadsNoNixFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFiles(t, root, map[string]string{"Makefile": "all:\n\techo hi\n", "a.nix": "{ }\n", "dir/b.nix": "{ }\n"})
	var nixReads atomic.Int64
	c, s := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {}}`, func(s *Server) {
		s.workspace.UseRead(func(path string, info fs.FileInfo, limit int64) ([]byte, fs.FileInfo, error) {
			if strings.HasSuffix(path, ".nix") {
				nixReads.Add(1)
			}
			return regularfile.ReadStat(path, info, limit)
		})
	})
	defer c.close()
	files, err := s.hostFiles(s.docs.View(), host.Make)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != filepath.Join(root, "Makefile") {
		t.Errorf("the graph of a Makefile sees %d files, want the Makefile", len(files))
	}
	if got := nixReads.Load(); got != 0 {
		t.Errorf("the graph of a Makefile read %d Nix files", got)
	}
}

func TestRenamingAFileHoldsOneBatchOfParsedFiles(t *testing.T) {
	root := t.TempDir()
	const files, size = 1200, 8 << 10
	tree := make(map[string]string, files)
	for i := range files {
		tree[fmt.Sprintf("m%04d.nix", i)] = nixModule(i, size)
	}
	writeFiles(t, root, tree)
	var reads atomic.Int64
	var highest atomic.Uint64
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {}}`, func(s *Server) {
		s.workspace.UseRead(func(path string, info fs.FileInfo, limit int64) ([]byte, fs.FileInfo, error) {
			if reads.Add(1)%100 == 0 {
				runtime.GC()
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				for seen := highest.Load(); m.HeapAlloc > seen && !highest.CompareAndSwap(seen, m.HeapAlloc); seen = highest.Load() {
				}
			}
			return regularfile.ReadStat(path, info, limit)
		})
	})
	defer c.close()
	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	c.call("workspace/willRenameFiles", `{"files": [{"oldUri": "`+fileuri.FromPath(filepath.Join(root, "gone.nix"))+`", "newUri": "`+fileuri.FromPath(filepath.Join(root, "new.nix"))+`"}]}`)
	if got := reads.Load(); got < files {
		t.Fatalf("the rename read %d of %d files", got, files)
	}
	const budget = 192 << 20
	if grew := highest.Load() - min(before.HeapAlloc, highest.Load()); grew > budget {
		t.Errorf("a rename over %d MiB of source held %d MiB, the budget is %d", files*size>>20, grew>>20, budget>>20)
	}
}

func TestWorkspaceDiagnosticsLeaveNoProjectOfAClosedFile(t *testing.T) {
	root := t.TempDir()
	const imported = 3 << 20
	large := "# " + strings.Repeat("x", imported) + "\n{ }\n"
	writeFiles(t, root, map[string]string{"a.nix": "{ wrap }: { x = wrap \"echo hi\"; y = import ./b.nix; z = import ./c.nix; }\n", "b.nix": large, "c.nix": large})
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}}}}`)
	defer c.close()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	c.call("workspace/diagnostic", `{"previousResultIds": []}`)
	runtime.GC()
	runtime.ReadMemStats(&after)
	if grew := after.HeapAlloc - min(before.HeapAlloc, after.HeapAlloc); grew > imported {
		t.Errorf("after diagnosing closed files, %d MiB stay held", grew>>20)
	}
}
