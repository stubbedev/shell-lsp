package perf

import (
	"runtime"
	"strings"
	"unsafe"
	"weak"
)

func PinsSource(suffix string, use func(sliced string)) bool {
	source := strings.Repeat("#", 1<<20) + suffix
	probe := weak.Make(unsafe.StringData(source)) //nolint:gosec // the pointer only feeds a weak reference that observes when the bytes are collected
	use(source[len(source)-len(suffix):])
	runtime.GC()
	return probe.Value() != nil
}
