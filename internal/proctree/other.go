//go:build !unix && !windows

package proctree

import "os/exec"

func Start(cmd *exec.Cmd) (func(), error) {
	return func() {}, cmd.Start()
}
