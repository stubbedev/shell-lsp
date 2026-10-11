//go:build !unix

package bats

import "os/exec"

func isolate(*exec.Cmd) {}
