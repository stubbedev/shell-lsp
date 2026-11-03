package nix

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
)

func loadedIncludes(includes []model.Include) []model.Include {
	return slices.DeleteFunc(slices.Clone(includes), func(inc model.Include) bool { return inc.Probe || inc.Dynamic })
}

func semanticIncludes(src string) []model.Include {
	parsed := Parse(src)
	ix := &model.Index{}
	newSymbolTable(&file{src: src, tree: parsed.tree, table: scope.Resolve(parsed.tree)}, ix).collect()
	return ix.Includes
}

func TestIncludesOfPlainPathsNeedNoScopes(t *testing.T) {
	t.Parallel()

	for _, src := range []string{"{ }\n", "{ a = import ./a.nix; b = ./b.nix; imports = [ ./c.nix ]; }\n", "{ a = ./a.nix\n"} {
		parsed := Parse(src)
		got := parsed.Includes()
		if parsed.table != nil {
			t.Errorf("reading the includes of %q resolved its scopes", src)
		}
		if want := semanticIncludes(src); !reflect.DeepEqual(loadedIncludes(got), loadedIncludes(want)) {
			t.Errorf("includes of %q = %+v, want %+v", src, got, want)
		}
	}
}

func TestIncludesOfOtherPathsFollowHowTheyAreUsed(t *testing.T) {
	t.Parallel()

	got := loadedIncludes(Parse("let dir = ./dir; in { a = import ./pkg; b = import dir; c = ./fix.patch; }\n").Includes())
	var paths []string
	for _, inc := range got {
		paths = append(paths, inc.Path)
	}
	if want := []string{"./dir", "./pkg"}; !slices.Equal(paths, want) {
		t.Errorf("loaded paths = %q, want %q", paths, want)
	}
}

func TestIncludesAgreeWithTheSymbolTableOnTheCorpus(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..", "corpus", "nix")
	files := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, nixExtension) {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		src := string(raw)
		if got, want := loadedIncludes(Parse(src).Includes()), loadedIncludes(semanticIncludes(src)); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: includes = %+v, want %+v", path, got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatalf("no Nix file under %s", root)
	}
}
