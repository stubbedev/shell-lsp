//go:build !unix

package platform

import "os"

func DeviceOf(path string) (uint64, error) {
	_, err := os.Stat(path)
	return 0, err
}
