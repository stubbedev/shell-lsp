//go:build !linux

package main

func sampleTreeRSS(int) (int64, error) { return 0, errRSSUnsupported }
