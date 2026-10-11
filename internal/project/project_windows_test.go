package project

import (
	"io/fs"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
)

func TestResolveWalksUpToDriveAndShareRoots(t *testing.T) {
	t.Parallel()

	disk := map[string]string{
		`C:\.editorconfig`:             "[*.sh]\nshell_variant = zsh\n",
		`C:\proj\.editorconfig`:        "[sub/*.sh]\nminify = true\n",
		`\\host\share\.editorconfig`:   "[*]\nshell_variant = ksh\n",
		`\\host\.editorconfig`:         "[*]\nshell_variant = dash\n",
		`\\host\share\x\.editorconfig`: "",
	}
	read := func(path string) ([]byte, error) {
		text, ok := disk[path]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return []byte(text), nil
	}
	resolver := editorconfig.NewResolver(read, time.Now, time.Second)
	tests := []struct {
		path   string
		want   dialect.Dialect
		minify bool
	}{
		{`C:\proj\sub\a.sh`, dialect.Zsh, true},
		{`\\host\share\x\a.sh`, dialect.Ksh, false},
	}
	for _, tc := range tests {
		r, err := Resolve(resolver, tc.path, nil, dialect.Input{URI: fileuri.FromPath(tc.path), Text: "echo\n"})
		if err != nil || r.Dialect != tc.want || (r.Settings.Style.Minify != nil) != tc.minify {
			t.Errorf("Resolve(%q) = %+v, %v; want %v with minify %v", tc.path, r, err, tc.want, tc.minify)
		}
	}
}
