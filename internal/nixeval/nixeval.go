package nixeval

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"time"

	"github.com/stubbedev/shell-lsp/internal/regularfile"
)

const (
	evalTimeout       = 2 * time.Minute
	maxConcurrent     = 4
	maxFlakeFile      = 16 << 20
	smallCacheBudget  = 32 << 20
	optionCacheBudget = 256 << 20
)

type Client struct {
	run     runner
	slots   chan struct{}
	system  *cache[string]
	locked  *cache[lockedFlake]
	outputs *cache[[]Output]
	attrs   *cache[[]string]
	pkgs    *cache[packageResult]
	sources *cache[[]OptionSource]
	options *cache[*OptionTree]
}

func New() *Client {
	return newClient(execRunner(nixProgram), evalTimeout, time.Now)
}

func newClient(run runner, timeout time.Duration, now func() time.Time) *Client {
	return &Client{
		run:     run,
		slots:   make(chan struct{}, maxConcurrent),
		system:  newCache[string](smallCacheBudget, timeout, now),
		locked:  newCache[lockedFlake](smallCacheBudget, timeout, now),
		outputs: newCache[[]Output](smallCacheBudget, timeout, now),
		attrs:   newCache[[]string](smallCacheBudget, timeout, now),
		pkgs:    newCache[packageResult](smallCacheBudget, timeout, now),
		sources: newCache[[]OptionSource](smallCacheBudget, timeout, now),
		options: newCache[*OptionTree](optionCacheBudget, timeout, now),
	}
}

func (c *Client) Outputs(ctx context.Context, dir string) ([]Output, error) {
	outputs, err := load(ctx, c, c.outputs, dir, "", func(ctx context.Context, flake lockedFlake) (computed[[]Output], error) {
		return runParsed(ctx, c, dir, showArgs(flake.ref), parseOutputs)
	})
	if err != nil {
		return nil, fmt.Errorf("listing the outputs of flake %s: %w", dir, err)
	}
	return slices.Clone(outputs), nil
}

func (c *Client) NixpkgsAttrs(ctx context.Context, dir string, parent []string, prefix string) ([]string, error) {
	names, err := load(ctx, c, c.attrs, dir, pathDetail(parent), func(ctx context.Context, flake lockedFlake) (computed[[]string], error) {
		args, err := c.packageArgs(ctx, flake, parent)
		if err != nil {
			return computed[[]string]{}, err
		}
		return runParsed(ctx, c, dir, evalArgs(callExpr("attrs", args...)), parseNames)
	})
	if err != nil {
		return nil, fmt.Errorf("listing nixpkgs attributes under %q of flake %s: %w", parent, dir, err)
	}
	return withPrefix(names, prefix), nil
}

func (c *Client) NixpkgsPackage(ctx context.Context, dir string, path []string) (Package, bool, error) {
	result, err := load(ctx, c, c.pkgs, dir, pathDetail(path), func(ctx context.Context, flake lockedFlake) (computed[packageResult], error) {
		args, err := c.packageArgs(ctx, flake, path)
		if err != nil {
			return computed[packageResult]{}, err
		}
		return runParsed(ctx, c, dir, evalArgs(callExpr("package", args...)), parsePackage)
	})
	if err != nil {
		return Package{}, false, fmt.Errorf("describing nixpkgs attribute %q of flake %s: %w", path, dir, err)
	}
	return result.pkg, result.found, nil
}

func (c *Client) OptionSources(ctx context.Context, dir string) ([]OptionSource, error) {
	sources, err := load(ctx, c, c.sources, dir, "", func(ctx context.Context, flake lockedFlake) (computed[[]OptionSource], error) {
		return runParsed(ctx, c, dir, evalArgs(callExpr("sources", stringArg("ref", flake.ref))), parseSources)
	})
	if err != nil {
		return nil, fmt.Errorf("listing the option sources of flake %s: %w", dir, err)
	}
	return slices.Clone(sources), nil
}

func (c *Client) Options(ctx context.Context, dir string, source OptionSource) (*OptionTree, error) {
	detail := fmt.Sprintf("%s %q", source.Kind.evalName(), source.Name)
	tree, err := load(ctx, c, c.options, dir, detail, func(ctx context.Context, flake lockedFlake) (computed[*OptionTree], error) {
		expr := callExpr("options", stringArg("ref", flake.ref), stringArg("kind", source.Kind.evalName()), stringArg("name", source.Name))
		return runParsed(ctx, c, dir, evalArgs(expr), parseOptions)
	})
	if err != nil {
		return nil, fmt.Errorf("listing the options of %s %q of flake %s: %w", source.Kind, source.Name, dir, err)
	}
	return tree, nil
}

func (c *Client) Invalidate(dir string) {
	clean := filepath.Clean(dir)
	c.locked.invalidate(clean)
	c.outputs.invalidate(clean)
	c.attrs.invalidate(clean)
	c.pkgs.invalidate(clean)
	c.sources.invalidate(clean)
	c.options.invalidate(clean)
}

func (c *Client) packageArgs(ctx context.Context, flake lockedFlake, path []string) ([]exprArg, error) {
	input, err := flake.nixpkgsInput()
	if err != nil {
		return nil, err
	}
	system, err := c.currentSystem(ctx)
	if err != nil {
		return nil, err
	}
	return []exprArg{stringArg("ref", flake.ref), stringArg("input", input), stringArg("system", system), listArg("path", path)}, nil
}

func (c *Client) currentSystem(ctx context.Context) (string, error) {
	return c.system.get(ctx, request{}, func(ctx context.Context) (computed[string], error) {
		out, err := c.runBounded(ctx, "", systemArgs())
		if err != nil {
			return computed[string]{}, fmt.Errorf("finding the current system: %w", err)
		}
		system, err := parseSystem(out)
		if err != nil {
			return computed[string]{}, err
		}
		return computed[string]{value: system, cost: len(out)}, nil
	})
}

func (c *Client) runBounded(ctx context.Context, dir string, args []string) ([]byte, error) {
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for a free nix process slot: %w", ctx.Err())
	}
	return c.run(ctx, dir, args)
}

func runParsed[T any](ctx context.Context, c *Client, dir string, args []string, parse func([]byte) (T, error)) (computed[T], error) {
	out, err := c.runBounded(ctx, dir, args)
	if err != nil {
		return computed[T]{}, err
	}
	value, err := parse(out)
	if err != nil {
		return computed[T]{}, err
	}
	return computed[T]{value: value, cost: len(out)}, nil
}

func load[T any](ctx context.Context, c *Client, results *cache[T], dir, detail string, compute func(context.Context, lockedFlake) (computed[T], error)) (T, error) {
	var zero T
	clean, identity, err := openFlake(dir)
	if err != nil {
		return zero, err
	}
	return results.get(ctx, request{dir: clean, fingerprint: identity, detail: detail}, func(ctx context.Context) (computed[T], error) {
		flake, err := c.locked.get(ctx, request{dir: clean, fingerprint: identity}, func(ctx context.Context) (computed[lockedFlake], error) {
			return runParsed(ctx, c, clean, metadataArgs(), parseMetadata)
		})
		if err != nil {
			return computed[T]{}, fmt.Errorf("resolving the flake: %w", err)
		}
		return compute(ctx, flake)
	})
}

func pathDetail(path []string) string {
	return fmt.Sprintf("%q", path)
}

func openFlake(dir string) (string, fingerprint, error) {
	if !filepath.IsAbs(dir) {
		return "", fingerprint{}, fmt.Errorf("the flake directory %q is not absolute", dir)
	}
	clean := filepath.Clean(dir)
	nix, _, err := regularfile.Read(filepath.Join(clean, "flake.nix"), maxFlakeFile)
	if err != nil {
		return "", fingerprint{}, fmt.Errorf("reading flake.nix: %w", err)
	}
	lock, _, err := regularfile.Read(filepath.Join(clean, "flake.lock"), maxFlakeFile)
	if errors.Is(err, fs.ErrNotExist) {
		return clean, fingerprintOf(nix, nil), nil
	}
	if err != nil {
		return "", fingerprint{}, fmt.Errorf("reading flake.lock: %w", err)
	}
	return clean, fingerprintOf(nix, lock), nil
}

func fingerprintOf(nix, lock []byte) fingerprint {
	nixSum := sha256.Sum256(nix)
	lockSum := sha256.Sum256(lock)
	lockPresent := byte(0)
	if lock != nil {
		lockPresent = 1
	}
	return sha256.Sum256(slices.Concat(nixSum[:], lockSum[:], []byte{lockPresent}))
}
