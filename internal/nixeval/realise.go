package nixeval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"
)

const realiseTimeout = 30 * time.Second

var errNotInFlake = errors.New("it belongs neither to the flake nor to an input in its flake.lock")

type FileUnavailableError struct {
	File string
	Err  error
}

func (e *FileUnavailableError) Error() string {
	return fmt.Sprintf("%s is not on disk: %v", e.File, e.Err)
}

func (e *FileUnavailableError) Unwrap() error {
	return e.Err
}

func (d Declaration) OnDisk() bool {
	_, err := os.Lstat(d.File)
	return err == nil
}

func (c *Client) RealiseFile(ctx context.Context, dir, file string) error {
	if _, err := os.Lstat(file); err == nil {
		return nil
	}
	flake, err := c.inspect(ctx, dir)
	if err != nil {
		return fmt.Errorf("realising %s of flake %s: %w", file, dir, err)
	}
	key := digestOf("realise", string(flake.local[:]), file)
	e := c.realised.join(key, slotKey{}, foreground, func(ctx context.Context, work task) (computed[struct{}], error) {
		return computed[struct{}]{}, c.realise(ctx, work, flake, file)
	})
	select {
	case <-e.done:
	case <-ctx.Done():
		return &NotReadyError{Refresh: e.refresh(), Err: ctx.Err()}
	}
	if e.err != nil {
		return &FileUnavailableError{File: file, Err: e.err}
	}
	c.realised.forgetIf(key, func(struct{}) bool { return true })
	return nil
}

func (c *Client) realise(ctx context.Context, work task, flake flakeState, file string) error {
	described, err := c.withSnapshot(ctx, work, flake, func(locked lockedFlake) ([]byte, error) {
		if locked.trees != "" {
			return []byte(locked.trees), nil
		}
		return c.runBounded(ctx, work, flake.dir, archiveArgs(locked.ref))
	})
	if err != nil {
		return err
	}
	var root sourceTree
	if err := json.Unmarshal(described, &root); err != nil {
		return fmt.Errorf("parsing nix flake archive output: %w", err)
	}
	names, found := root.owner(file)
	if !found {
		return errNotInFlake
	}
	if len(names) == 0 {
		c.locked.forgetIf(flake.local, func(lockedFlake) bool { return true })
		if _, err := c.resolve(ctx, flake); err != nil {
			return err
		}
	} else if err := c.fetchInput(ctx, work, flake, names); err != nil {
		return err
	}
	if _, err := os.Lstat(file); err != nil {
		return fmt.Errorf("its source tree is in the store now, but holds no such file: %w", err)
	}
	return nil
}

func (c *Client) fetchInput(ctx context.Context, work task, flake flakeState, names []string) error {
	if flake.lock == nil {
		return fmt.Errorf("input %q has no flake.lock entry", strings.Join(names, "/"))
	}
	lock, err := parseLock(flake.lock)
	if err != nil {
		return err
	}
	locked, err := lock.lockedAt(names)
	if err != nil {
		return err
	}
	out, err := c.runBounded(ctx, work, flake.dir, evalArgs(callExpr("inputRef", stringArg("locked", string(locked)))))
	if err != nil {
		return fmt.Errorf("naming input %q: %w", strings.Join(names, "/"), err)
	}
	var ref string
	if err := json.Unmarshal(out, &ref); err != nil {
		return fmt.Errorf("parsing the reference of input %q: %w", strings.Join(names, "/"), err)
	}
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("input %q has the reference %q, which nix would read as an option", strings.Join(names, "/"), ref)
	}
	if _, err := c.runBounded(ctx, work, flake.dir, prefetchRefArgs(ref)); err != nil {
		return fmt.Errorf("fetching input %q: %w", strings.Join(names, "/"), err)
	}
	return nil
}

type sourceTree struct {
	Path   string                `json:"path"`
	Inputs map[string]sourceTree `json:"inputs"`
}

func (t sourceTree) owner(file string) ([]string, bool) {
	if t.Path != "" && (file == t.Path || strings.HasPrefix(file, t.Path+"/")) {
		return []string{}, true
	}
	for _, name := range slices.Sorted(maps.Keys(t.Inputs)) {
		if names, found := t.Inputs[name].owner(file); found {
			return append([]string{name}, names...), true
		}
	}
	return nil, false
}
