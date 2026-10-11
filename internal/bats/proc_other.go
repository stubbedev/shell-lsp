//go:build !unix && !windows

package bats

import "os/exec"

func startTree(cmd *exec.Cmd) (func(), error) {
	return func() {}, cmd.Start()
}
