//go:build !windows

package manpage

var defaultManDirs = []string{"/usr/share/man", "/usr/local/share/man", "/usr/local/man", "/opt/homebrew/share/man"}
