package server

import "runtime"

type urgency uint8

const (
	background urgency = iota
	interactive
)

func (u urgency) workers() int {
	if u == background {
		return 1
	}
	return runtime.GOMAXPROCS(0)
}
