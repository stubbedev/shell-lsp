package platform

import "testing"

func TestIsDevicePath(t *testing.T) {
	t.Parallel()

	devices := []string{
		"NUL", "nul", `C:\proj\NUL`, `C:\proj\nul.txt`, "CON", "con:", `sub\AUX `, "PRN.sh", "COM1", "lpt9.log", "COM²",
		"CONIN$", "conout$", `C:NUL`,
		`\\.\pipe\shell-lsp`, `//./pipe/x`, `\\.\C:`, `\\.\PhysicalDrive0`, `\\?\pipe\x`, `\\?\GLOBALROOT\Device\Null`,
	}
	for _, p := range devices {
		if !Windows.IsDevicePath(p) {
			t.Errorf("Windows.IsDevicePath(%q) = false, want true", p)
		}
		if POSIX.IsDevicePath(p) {
			t.Errorf("POSIX.IsDevicePath(%q) = true, want false", p)
		}
	}
	files := []string{
		`C:\proj\a.sh`, `C:\proj\console.sh`, `C:\proj\COM10`, `C:\proj\nullable`, `C:\NUL\a.sh`, `\\?\C:\proj\a.sh`,
		`\\?\UNC\host\share\a.sh`, `\\host\share\a.sh`, "a.sh", "",
	}
	for _, p := range files {
		if Windows.IsDevicePath(p) {
			t.Errorf("Windows.IsDevicePath(%q) = true, want false", p)
		}
	}
}
