package scalingtest

import (
	"math"
	"runtime"
	"testing"
	"time"
)

const (
	growth  = 16
	allowed = 64
	repeats = 7
)

func Linear(t testing.TB, what string, base int, prepare func(n int) func()) {
	t.Helper()
	runs := [...]func(){prepare(base), prepare(base * growth)}
	fastest := [len(runs)]time.Duration{time.Duration(math.MaxInt64), time.Duration(math.MaxInt64)}
	for range repeats {
		for i, run := range runs {
			runtime.GC()
			start := time.Now()
			run()
			fastest[i] = min(fastest[i], time.Since(start))
		}
	}
	if small, large := fastest[0], fastest[1]; large > allowed*small {
		t.Errorf("%s: %d took %v, %d took %v: more than %dx for %dx the input", what, base, small, base*growth, large, allowed, growth)
	}
}
