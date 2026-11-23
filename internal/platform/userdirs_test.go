package platform

import (
	"path/filepath"
	"testing"
)

func TestConfigDirPrefersXDGConfigHome(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "home")
	xdg := filepath.Join(t.TempDir(), "xdg")
	for _, tt := range []struct {
		dirs UserDirs
		want string
	}{
		{UserDirs{Home: home, ConfigHome: xdg}, xdg},
		{UserDirs{Home: home}, filepath.Join(home, ".config")},
		{UserDirs{ConfigHome: xdg}, xdg},
		{UserDirs{}, ""},
	} {
		if got := tt.dirs.ConfigDir(); got != tt.want {
			t.Errorf("%+v.ConfigDir() = %q, want %q", tt.dirs, got, tt.want)
		}
	}
}
