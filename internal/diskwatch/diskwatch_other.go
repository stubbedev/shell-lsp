//go:build !linux

package diskwatch

import "errors"

var errUnsupported = errors.New("watching files needs inotify, which only Linux has")

func New() (*Watcher, error) { return nil, errUnsupported }

func isAbsent(error) bool { return false }
