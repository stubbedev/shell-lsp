package platform

import "path/filepath"

type UserDirs struct {
	Home       string
	ConfigHome string
}

func (d UserDirs) ConfigDir() string {
	if d.ConfigHome != "" {
		return d.ConfigHome
	}
	if d.Home != "" {
		return filepath.Join(d.Home, ".config")
	}
	return ""
}
