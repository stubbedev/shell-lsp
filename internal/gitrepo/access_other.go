//go:build !unix

package gitrepo

import "os"

func searchable(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
