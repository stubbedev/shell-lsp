package cli

import (
	"strings"
	"testing"
)

func TestCheckNamesAFileOnceWhateverItsCase(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"proj/a.sh": unquoted})
	got := run(t, dir, "", "check", "-f", "gcc", `proj\a.sh`, `PROJ\A.SH`, "proj")
	if n := strings.Count(strings.ToLower(got.stdout), "a.sh:2:6"); n != 1 || got.status != checkStatusFindings {
		t.Errorf("a file named in two cases was checked %d times (status %d):\n%s", n, got.status, got.stdout)
	}
}
