package bats

import (
	"reflect"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/platform"
)

func TestFailuresPointAtTheFailingLine(t *testing.T) {
	t.Parallel()

	report, _ := parse("1..4\n" +
		"not ok 1 in a helper\n" +
		"# (from function `check' in file /proj/test/helper.bash, line 4,\n" +
		"#  in test file test/suite.bats, line 9)\n" +
		"#   `check' failed\n" +
		"not ok 2 without frames\n" +
		"ok 3 passes\n" +
		"not ok 4 setup_file failed\n")
	lines := map[string]int{"without frames": 12}
	got := failures(platform.POSIX, report, "/proj/test/suite.bats", "/proj", func(name string) (int, bool) {
		line, ok := lines[name]
		return line, ok
	})
	want := []Failure{
		{Test: "in a helper", Line: 9, Message: "test failed: in a helper\n  `check' failed"},
		{Test: "without frames", Line: 12, Message: "test failed: without frames"},
		{Test: "setup_file failed", Line: 1, Message: "test failed: setup_file failed"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("failures = %+v\nwant       %+v", got, want)
	}
}

func TestFailuresTakeTheInnermostFrameInTheFile(t *testing.T) {
	t.Parallel()

	report, _ := parse("not ok 1 calls a local helper\n" +
		"# (from function `helper' in file suite.bats, line 3,\n" +
		"#  in test file suite.bats, line 8)\n")
	got := failures(platform.POSIX, report, "/proj/suite.bats", "/proj", func(string) (int, bool) { return 0, false })
	if len(got) != 1 || got[0].Line != 3 {
		t.Errorf("failures = %+v, want line 3", got)
	}
}

func TestFailuresResolveMSYSPathsOnWindows(t *testing.T) {
	t.Parallel()

	report, _ := parse("not ok 1 in a helper\n" +
		"# (from function `check' in file /c/Proj/test/helper.bash, line 4,\n" +
		"#  in test file /c/Proj/test/Suite.bats, line 9)\n" +
		"not ok 2 relative\n" +
		"# (in test file test/suite.bats, line 5)\n")
	got := failures(platform.Windows, report, `C:\proj\test\suite.bats`, `c:\proj`, func(string) (int, bool) { return 0, false })
	if len(got) != 2 || got[0].Line != 9 || got[1].Line != 5 {
		t.Errorf("failures = %+v, want lines 9 and 5", got)
	}
}
