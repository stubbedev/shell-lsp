package cli

import (
	"fmt"
	"io/fs"
	"math"
	"time"

	"github.com/stubbedev/shell-lsp/internal/filecache"
	"github.com/stubbedev/shell-lsp/internal/platform"
	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
)

type rcFinder struct {
	choice   rcChoice
	resolver *shellcheckrc.Resolver
}

func newRCFinder(choice rcChoice, read filecache.ReadFile, dirs platform.UserDirs) *rcFinder {
	never := time.Duration(math.MaxInt64)
	return &rcFinder{choice: choice, resolver: shellcheckrc.NewResolver(read, time.Now, never, shellcheckrc.Fallback(dirs))}
}

func (f *rcFinder) forDir(dir string) (shellcheckrc.File, error) {
	switch f.choice.mode {
	case rcNone:
		return shellcheckrc.File{}, nil
	case rcExplicit:
		rc, found, err := f.resolver.Load(f.choice.path)
		if err == nil && !found {
			err = fmt.Errorf("reading --rcfile %s: %w", f.choice.path, fs.ErrNotExist)
		}
		return rc, err
	case rcSearch:
	}
	rc, _, err := f.resolver.Nearest(dir)
	return rc, err
}
