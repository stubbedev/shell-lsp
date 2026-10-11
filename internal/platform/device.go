package platform

import (
	"slices"
	"strings"
)

var reservedDeviceNames = []string{
	"CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$",
	"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "COM¹", "COM²", "COM³",
	"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9", "LPT¹", "LPT²", "LPT³",
}

func (f Flavour) IsDevicePath(p string) bool {
	if f == POSIX {
		return false
	}
	slashed := strings.ReplaceAll(p, `\`, "/")
	if strings.HasPrefix(slashed, "//./") {
		return true
	}
	if rest, ok := strings.CutPrefix(slashed, "//?/"); ok && !HasDrive(rest) && !strings.HasPrefix(strings.ToUpper(rest), "UNC/") {
		return true
	}
	name := slashed[strings.LastIndexByte(slashed, '/')+1:]
	if HasDrive(name) {
		name = name[2:]
	}
	if i := strings.IndexAny(name, ".:"); i >= 0 {
		name = name[:i]
	}
	return slices.Contains(reservedDeviceNames, strings.ToUpper(strings.TrimRight(name, " ")))
}
