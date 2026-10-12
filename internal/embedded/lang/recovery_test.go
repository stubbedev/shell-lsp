package lang

import "testing"

func TestAttemptRecoversOnlyABailOut(t *testing.T) {
	t.Parallel()

	if !Attempt(func() {}) {
		t.Error("a parse that returns is reported as bailed out")
	}
	if Attempt(Bail) {
		t.Error("a parse that bails out is reported as finished")
	}
	stopped := false
	Run(func() {
		Attempt(Abort)
		stopped = true
	})
	if stopped {
		t.Error("Attempt swallowed an abort")
	}
}

func TestRunRecoversOnlyAnAbort(t *testing.T) {
	t.Parallel()

	Run(Abort)
	defer func() {
		if r := recover(); r != "other" {
			t.Errorf("Run let through %v, want the foreign panic", r)
		}
	}()
	Run(func() { panic("other") })
}
