package filecache

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"testing"
	"time"
)

func parseNumber(path string, data []byte) (int, error) {
	n, err := strconv.Atoi(string(data))
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	return n, nil
}

func TestCacheKeepsEachOutcomeUntilRecheckOrInvalidate(t *testing.T) {
	t.Parallel()

	now := time.Now()
	files := map[string]string{"/a": "1", "/broken": "x"}
	reads := 0
	read := func(path string) ([]byte, error) {
		reads++
		text, ok := files[path]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return []byte(text), nil
	}
	c := New(read, parseNumber, func() time.Time { return now }, time.Second)
	for range 2 {
		if n, present, err := c.Load("/a"); n != 1 || !present || err != nil {
			t.Errorf("/a = %d, %v, %v", n, present, err)
		}
		if _, present, err := c.Load("/missing"); present || err != nil {
			t.Errorf("/missing = %v, %v, want absent", present, err)
		}
		if _, present, err := c.Load("/broken"); present || err == nil {
			t.Errorf("/broken = %v, %v, want a parse error", present, err)
		}
	}
	if reads != 3 {
		t.Errorf("%d reads within the recheck window, want one per path", reads)
	}
	files["/a"] = "2"
	c.Invalidate("/a")
	if n, _, _ := c.Load("/a"); n != 2 {
		t.Errorf("after Invalidate /a = %d", n)
	}
	files["/broken"] = "3"
	now = now.Add(time.Second)
	if n, present, err := c.Load("/broken"); n != 3 || !present || err != nil {
		t.Errorf("after the recheck window /broken = %d, %v, %v", n, present, err)
	}
}

func TestCacheReportsUnreadableFiles(t *testing.T) {
	t.Parallel()

	c := New(func(string) ([]byte, error) { return nil, fs.ErrPermission }, parseNumber, time.Now, time.Second)
	if _, _, err := c.Load("/a"); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("error = %v, want the permission failure", err)
	}
}

func TestCacheForgetsStalePaths(t *testing.T) {
	t.Parallel()

	now := time.Now()
	c := New(func(string) ([]byte, error) { return nil, fs.ErrNotExist }, parseNumber, func() time.Time { return now }, time.Second)
	for i := range 10 * minSweep {
		if _, _, err := c.Load(fmt.Sprintf("/tmp/session%d/a", i)); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	if len(c.entries) > 2*minSweep {
		t.Errorf("cache holds %d entries after visiting paths one recheck interval apart", len(c.entries))
	}
}
