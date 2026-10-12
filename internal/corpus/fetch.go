package corpus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/shell-lsp/internal/parallel"
)

const (
	fetchTimeout = 10 * time.Minute
	fetchPrefix  = ".fetch-"
	fetchWorkers = 8
)

var cacheEntry = regexp.MustCompile(`^(?:\.fetch-.*|[a-z0-9][a-z0-9._-]*@[0-9a-f]{12})$`)

func (r Repo) Dir(cache string) string { return filepath.Join(cache, r.dirName()) }

func Fetch(ctx context.Context, repos []Repo, cache string) error {
	if err := os.MkdirAll(cache, 0o750); err != nil {
		return fmt.Errorf("creating the corpus cache %s: %w", cache, err)
	}
	if err := prune(repos, cache); err != nil {
		return err
	}
	errs := make([]error, len(repos))
	parallel.For(fetchWorkers, len(repos), func(i int) {
		errs[i] = fetchOne(ctx, repos[i], cache)
	})
	return errors.Join(errs...)
}

func Fetched(repos []Repo, cache string) error {
	var missing []string
	for _, r := range repos {
		if _, err := os.Stat(r.Dir(cache)); err != nil {
			missing = append(missing, r.Name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("corpus cache %s lacks %s; run `just corpus-fetch` first", cache, strings.Join(missing, ", "))
	}
	return nil
}

func prune(repos []Repo, cache string) error {
	wanted := map[string]bool{}
	for _, r := range repos {
		wanted[r.dirName()] = true
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		return fmt.Errorf("listing the corpus cache %s: %w", cache, err)
	}
	for _, e := range entries {
		if wanted[e.Name()] || !cacheEntry.MatchString(e.Name()) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(cache, e.Name())); err != nil {
			return fmt.Errorf("removing the stale corpus entry %s: %w", e.Name(), err)
		}
	}
	return nil
}

func fetchOne(ctx context.Context, r Repo, cache string) error {
	final := r.Dir(cache)
	if _, err := os.Stat(final); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("checking the cached %s: %w", r.Name, err)
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	tmp, err := os.MkdirTemp(cache, fetchPrefix+r.Name+"-")
	if err != nil {
		return fmt.Errorf("fetching %s: %w", r.Name, err)
	}
	defer os.RemoveAll(tmp) //nolint:errcheck
	if err := checkout(ctx, r, tmp); err != nil {
		return fmt.Errorf("fetching %s at %s from %s: %w", r.Name, r.Commit, r.URL, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("moving %s into the corpus cache: %w", r.Name, err)
	}
	return nil
}

func checkout(ctx context.Context, r Repo, dir string) error {
	if _, err := git(ctx, dir, "init", "--quiet"); err != nil {
		return err
	}
	if len(r.Paths) > 0 {
		if _, err := git(ctx, dir, "config", "core.sparseCheckout", "true"); err != nil {
			return err
		}
		var patterns strings.Builder
		for _, p := range r.Paths {
			patterns.WriteString("/" + p + "\n")
		}
		if err := os.WriteFile(filepath.Join(dir, ".git", "info", "sparse-checkout"), []byte(patterns.String()), 0o600); err != nil {
			return fmt.Errorf("writing the sparse checkout patterns: %w", err)
		}
	}
	if _, err := git(ctx, dir, "fetch", "--quiet", "--depth=1", "--filter=blob:none", "--no-tags", r.URL, r.Commit); err != nil {
		return err
	}
	if _, err := git(ctx, dir, "-c", "advice.detachedHead=false", "checkout", "--quiet", "--detach", "FETCH_HEAD"); err != nil {
		return err
	}
	head, err := git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != r.Commit {
		return fmt.Errorf("checked out %s instead", head)
	}
	if err := os.RemoveAll(filepath.Join(dir, ".git")); err != nil {
		return fmt.Errorf("removing the git metadata: %w", err)
	}
	return nil
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	argv := append([]string{"-C", dir, "-c", "core.hooksPath=/dev/null", "-c", "protocol.file.allow=never", "-c", "submodule.recurse=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...) //nolint:gosec
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(v string) bool { return strings.HasPrefix(v, "GIT_") }),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_LFS_SKIP_SMUDGE=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
