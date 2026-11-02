package nixeval

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"time"

	"github.com/stubbedev/shell-lsp/internal/regularfile"
)

const (
	refreshTimeout    = 10 * time.Minute
	maxConcurrent     = 4
	maxBackground     = 1
	MaxFlakeFile      = 16 << 20
	smallCacheBudget  = 32 << 20
	optionCacheBudget = 256 << 20
	cacheSubdirectory = "shell-lsp/nixeval"
)

var (
	evalDigest       = sha256.Sum256([]byte(evalSource))
	optionRenderings = [...]string{"all", "outside-deferred", "top-level", "nothing"}
)

type queryKind string

const (
	queryOutputs queryKind = "outputs"
	querySources queryKind = "sources"
	queryOptions queryKind = "options"
	queryAttrs   queryKind = "attrs"
	queryPackage queryKind = "package"
	queryBase    queryKind = "base"
)

type settings struct {
	run      runner
	identify func() (string, error)
	cacheDir string
	timeout  time.Duration
	now      func() time.Time
	report   func(error)
}

type Client struct {
	run      runner
	identify func() (string, error)
	disk     *store
	report   func(error)
	life     *lifetime
	slots    chan struct{}
	lowSlots chan struct{}
	system   *cache[string]
	locked   *cache[lockedFlake]
	outputs  *cache[[]Output]
	attrs    *cache[[]string]
	packages *cache[*Package]
	sources  *cache[[]OptionSource]
	options  *cache[*OptionTree]
}

func New(report func(error)) (*Client, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("finding the user cache directory: %w", err)
	}
	return newClient(settings{
		run:      execRunner(nixProgram),
		identify: nixIdentity(nixProgram),
		cacheDir: filepath.Join(root, filepath.FromSlash(cacheSubdirectory)),
		timeout:  refreshTimeout,
		now:      time.Now,
		report:   report,
	})
}

func newClient(s settings) (*Client, error) {
	if err := os.MkdirAll(s.cacheDir, cacheDirMode); err != nil {
		return nil, fmt.Errorf("creating the evaluation cache directory: %w", err)
	}
	life := newLifetime()
	return &Client{
		run:      s.run,
		identify: s.identify,
		disk:     &store{dir: s.cacheDir, budget: storeBudget, now: s.now, report: s.report},
		report:   s.report,
		life:     life,
		slots:    make(chan struct{}, maxConcurrent),
		lowSlots: make(chan struct{}, maxBackground),
		system:   newCache[string](life, smallCacheBudget, s.timeout, s.now),
		locked:   newCache[lockedFlake](life, smallCacheBudget, s.timeout, s.now),
		outputs:  newCache[[]Output](life, smallCacheBudget, s.timeout, s.now),
		attrs:    newCache[[]string](life, smallCacheBudget, s.timeout, s.now),
		packages: newCache[*Package](life, smallCacheBudget, s.timeout, s.now),
		sources:  newCache[[]OptionSource](life, smallCacheBudget, s.timeout, s.now),
		options:  newCache[*OptionTree](life, optionCacheBudget, s.timeout, s.now),
	}, nil
}

func (c *Client) Close() {
	c.life.close()
}

func (c *Client) Outputs(ctx context.Context, dir string) (Answer[[]Output], error) {
	answer, err := serveFlake(ctx, c, dir, func(flake flakeState) (query[[]Output], error) {
		return c.outputsQuery(flake), nil
	})
	if err != nil {
		return Answer[[]Output]{}, fmt.Errorf("listing the outputs of flake %s: %w", dir, err)
	}
	return mapAnswer(answer, slices.Clone[[]Output]), nil
}

func (c *Client) OptionSources(ctx context.Context, dir string) (Answer[[]OptionSource], error) {
	answer, err := serveFlake(ctx, c, dir, func(flake flakeState) (query[[]OptionSource], error) {
		return c.sourcesQuery(flake), nil
	})
	if err != nil {
		return Answer[[]OptionSource]{}, fmt.Errorf("listing the option sources of flake %s: %w", dir, err)
	}
	return mapAnswer(answer, slices.Clone[[]OptionSource]), nil
}

func (c *Client) Options(ctx context.Context, dir string, source OptionSource) (Answer[*OptionTree], error) {
	answer, err := serveFlake(ctx, c, dir, func(flake flakeState) (query[*OptionTree], error) {
		return c.optionsQuery(flake, source), nil
	})
	if err != nil {
		return Answer[*OptionTree]{}, fmt.Errorf("listing the options of %s %q of flake %s: %w", source.Kind, source.Name, dir, err)
	}
	return answer, nil
}

func (c *Client) BaseOptions(ctx context.Context, dir string) (Answer[*OptionTree], error) {
	answer, err := serveFlake(ctx, c, dir, func(flake flakeState) (query[*OptionTree], error) {
		scope, err := c.inputScope(ctx, flake)
		if err != nil {
			return query[*OptionTree]{}, err
		}
		return c.baseQuery(flake, scope), nil
	})
	if err != nil {
		return Answer[*OptionTree]{}, fmt.Errorf("listing the NixOS options of the nixpkgs of flake %s: %w", dir, err)
	}
	return answer, nil
}

func (c *Client) NixpkgsAttrs(ctx context.Context, dir string, parent []string, prefix string) (Answer[[]string], error) {
	answer, err := serveFlake(ctx, c, dir, func(flake flakeState) (query[[]string], error) {
		scope, err := c.inputScope(ctx, flake)
		if err != nil {
			return query[[]string]{}, err
		}
		return c.attrsQuery(flake, scope, parent), nil
	})
	if err != nil {
		return Answer[[]string]{}, fmt.Errorf("listing nixpkgs attributes under %q of flake %s: %w", parent, dir, err)
	}
	return mapAnswer(answer, func(names []string) []string { return withPrefix(names, prefix) }), nil
}

func (c *Client) NixpkgsPackage(ctx context.Context, dir string, path []string) (Answer[*Package], error) {
	answer, err := serveFlake(ctx, c, dir, func(flake flakeState) (query[*Package], error) {
		scope, err := c.inputScope(ctx, flake)
		if err != nil {
			return query[*Package]{}, err
		}
		return c.packageQuery(flake, scope, path), nil
	})
	if err != nil {
		return Answer[*Package]{}, fmt.Errorf("describing nixpkgs attribute %q of flake %s: %w", path, dir, err)
	}
	return mapAnswer(answer, copyPackage), nil
}

func serveFlake[T any](ctx context.Context, c *Client, dir string, build func(flakeState) (query[T], error)) (Answer[T], error) {
	flake, err := c.inspect(ctx, dir)
	if err != nil {
		return Answer[T]{}, err
	}
	q, err := build(flake)
	if err != nil {
		return Answer[T]{}, err
	}
	return serve(ctx, c, q)
}

func copyPackage(pkg *Package) *Package {
	if pkg == nil {
		return nil
	}
	duplicate := *pkg
	return &duplicate
}

func (c *Client) Warm(dir string) *Refresh {
	done := make(chan struct{})
	failure := new(error)
	started := c.life.start(func() {
		defer close(done)
		*failure = c.warm(c.life.ctx, dir)
	})
	if !started {
		*failure = ErrClosed
		close(done)
	}
	return &Refresh{done: done, err: failure}
}

func (c *Client) warm(ctx context.Context, dir string) error {
	flake, err := c.inspect(ctx, dir)
	if err != nil {
		return fmt.Errorf("warming flake %s: %w", dir, err)
	}
	var problems []error
	if _, err := settle(ctx, c, c.outputsQuery(flake)); err != nil {
		problems = append(problems, fmt.Errorf("warming the outputs of flake %s: %w", dir, err))
	}
	sources, err := settle(ctx, c, c.sourcesQuery(flake))
	if err != nil {
		problems = append(problems, fmt.Errorf("warming the option sources of flake %s: %w", dir, err))
	}
	for _, source := range sources {
		if _, err := settle(ctx, c, c.optionsQuery(flake, source)); err != nil {
			problems = append(problems, fmt.Errorf("warming the options of %s %q of flake %s: %w", source.Kind, source.Name, dir, err))
		}
	}
	scope, err := c.inputScope(ctx, flake)
	if errors.Is(err, ErrNoNixpkgsInput) {
		return errors.Join(problems...)
	}
	if err != nil {
		return errors.Join(append(problems, fmt.Errorf("warming nixpkgs of flake %s: %w", dir, err))...)
	}
	if _, err := settle(ctx, c, c.attrsQuery(flake, scope, nil)); err != nil {
		problems = append(problems, fmt.Errorf("warming the nixpkgs attributes of flake %s: %w", dir, err))
	}
	if slices.ContainsFunc(sources, func(source OptionSource) bool { return source.Kind == SourceNixOS }) {
		if _, err := settle(ctx, c, c.baseQuery(flake, scope)); err != nil {
			problems = append(problems, fmt.Errorf("warming the NixOS options of the nixpkgs of flake %s: %w", dir, err))
		}
	}
	return errors.Join(problems...)
}

type query[T any] struct {
	results  *cache[T]
	key      contentKey
	slot     slotKey
	base     func() (contentKey, bool)
	evaluate func(context.Context, task) ([]byte, error)
	decode   func([]byte) (T, error)
}

func start[T any](c *Client, q query[T], level priority) *entry[T] {
	return q.results.join(q.key, q.slot, level, func(ctx context.Context, work task) (computed[T], error) {
		if payload, ok := c.disk.read(q.key); ok {
			value, err := q.decode(payload)
			if err == nil {
				c.disk.setLatest(q.slot, q.key)
				return computed[T]{value: value, cost: len(payload)}, nil
			}
			c.report(fmt.Errorf("discarding the evaluation cache entry %x: %w", q.key, err))
		}
		work.miss()
		payload, err := q.evaluate(ctx, work)
		if err != nil {
			return computed[T]{}, err
		}
		value, err := q.decode(payload)
		if err != nil {
			return computed[T]{}, err
		}
		c.disk.write(q.key, payload)
		c.disk.setLatest(q.slot, q.key)
		return computed[T]{value: value, cost: len(payload)}, nil
	})
}

func serve[T any](ctx context.Context, c *Client, q query[T]) (Answer[T], error) {
	if err := ctx.Err(); err != nil {
		return Answer[T]{}, err
	}
	e := start(c, q, foreground)
	select {
	case <-e.done:
	case <-e.missed:
	case <-ctx.Done():
		return Answer[T]{}, &NotReadyError{Refresh: e.refresh(), Err: ctx.Err()}
	}
	if e.succeeded() {
		return Answer[T]{value: e.value}, nil
	}
	if value, basis, ok := fallback(c, q); ok {
		return Answer[T]{value: value, stale: &Stale{Basis: basis, Refresh: e.refresh()}}, nil
	}
	select {
	case <-e.done:
	case <-ctx.Done():
		return Answer[T]{}, &NotReadyError{Refresh: e.refresh(), Err: ctx.Err()}
	}
	if e.err != nil {
		return Answer[T]{}, e.err
	}
	return Answer[T]{value: e.value}, nil
}

func settle[T any](ctx context.Context, c *Client, q query[T]) (T, error) {
	var zero T
	e := start(c, q, background)
	select {
	case <-e.done:
		return e.value, e.err
	case <-ctx.Done():
		return zero, context.Cause(ctx)
	}
}

func fallback[T any](c *Client, q query[T]) (T, Basis, bool) {
	if key, ok := c.disk.latest(q.slot); ok && key != q.key {
		if value, ok := loadFinished(c, q, key); ok {
			return value, BasisEarlierState, true
		}
	}
	if q.base != nil {
		if key, ok := q.base(); ok {
			if value, ok := loadFinished(c, q, key); ok {
				return value, BasisLockedNixpkgs, true
			}
		}
	}
	var zero T
	return zero, BasisEarlierState, false
}

func loadFinished[T any](c *Client, q query[T], key contentKey) (T, bool) {
	if value, ok := q.results.succeeded(key); ok {
		return value, true
	}
	var zero T
	payload, ok := c.disk.read(key)
	if !ok {
		return zero, false
	}
	value, err := q.decode(payload)
	if err != nil {
		c.report(fmt.Errorf("discarding the evaluation cache entry %x: %w", key, err))
		return zero, false
	}
	q.results.insert(key, computed[T]{value: value, cost: len(payload)})
	return value, true
}

func (c *Client) evaluating(flake flakeState, args func(lockedFlake) []string) func(context.Context, task) ([]byte, error) {
	return func(ctx context.Context, work task) ([]byte, error) {
		locked, err := c.resolve(ctx, flake)
		if err != nil {
			return nil, err
		}
		return c.runBounded(ctx, work, flake.dir, args(locked))
	}
}

func isEvaluationFailure(err error) bool {
	_, exited := errors.AsType[*exec.ExitError](err)
	return exited
}

func (c *Client) renderedOptions(flake flakeState, what string, args func(locked lockedFlake, rendering exprArg) []string) func(context.Context, task) ([]byte, error) {
	return func(ctx context.Context, work task) ([]byte, error) {
		var failure error
		for i, rendering := range optionRenderings {
			out, err := c.evaluating(flake, func(locked lockedFlake) []string {
				return args(locked, stringArg("rendering", rendering))
			})(ctx, work)
			if err == nil {
				if i > 0 {
					c.report(fmt.Errorf("some option values of %s cannot be evaluated, so only the %q rendering is cached: %w", what, rendering, failure))
				}
				return encodeOptions(out)
			}
			if !isEvaluationFailure(err) {
				return nil, err
			}
			failure = err
		}
		return nil, failure
	}
}

func (c *Client) outputsQuery(flake flakeState) query[[]Output] {
	return query[[]Output]{
		results: c.outputs,
		key:     flake.queryKey(queryOutputs, ""),
		slot:    flake.slot(queryOutputs, ""),
		decode:  parseOutputs,
		evaluate: c.evaluating(flake, func(locked lockedFlake) []string {
			return showArgs(locked.ref)
		}),
	}
}

func (c *Client) sourcesQuery(flake flakeState) query[[]OptionSource] {
	return query[[]OptionSource]{
		results: c.sources,
		key:     flake.queryKey(querySources, ""),
		slot:    flake.slot(querySources, ""),
		decode:  parseSources,
		evaluate: c.evaluating(flake, func(locked lockedFlake) []string {
			return evalArgs(callExpr("sources", stringArg("ref", locked.ref)))
		}),
	}
}

func (c *Client) optionsQuery(flake flakeState, source OptionSource) query[*OptionTree] {
	detail := fmt.Sprintf("%s %q", source.Kind.evalName(), source.Name)
	q := query[*OptionTree]{
		results: c.options,
		key:     flake.queryKey(queryOptions, detail),
		slot:    flake.slot(queryOptions, detail),
		decode:  decodeOptions,
		evaluate: c.renderedOptions(flake, detail, func(locked lockedFlake, rendering exprArg) []string {
			return evalArgs(callExpr("options", stringArg("ref", locked.ref), stringArg("kind", source.Kind.evalName()), stringArg("name", source.Name), rendering))
		}),
	}
	if source.Kind == SourceNixOS {
		q.base = func() (contentKey, bool) { return c.baseKey(flake) }
	}
	return q
}

func (c *Client) baseKey(flake flakeState) (contentKey, bool) {
	ctx, cancel := context.WithTimeout(c.life.ctx, systemLookupTimeout)
	defer cancel()
	scope, err := c.inputScope(ctx, flake)
	if err != nil {
		return contentKey{}, false
	}
	return scope.queryKey(queryBase, ""), true
}

func (c *Client) baseQuery(flake flakeState, scope inputScope) query[*OptionTree] {
	return query[*OptionTree]{
		results: c.options,
		key:     scope.queryKey(queryBase, ""),
		slot:    flake.slot(queryBase, ""),
		decode:  decodeOptions,
		evaluate: c.renderedOptions(flake, "the NixOS options of the locked nixpkgs", func(locked lockedFlake, rendering exprArg) []string {
			return evalArgs(callExpr("base", append(scope.args(locked), rendering)...))
		}),
	}
}

func (c *Client) attrsQuery(flake flakeState, scope inputScope, parent []string) query[[]string] {
	detail := pathDetail(parent)
	return query[[]string]{
		results: c.attrs,
		key:     scope.queryKey(queryAttrs, detail),
		slot:    flake.slot(queryAttrs, detail),
		decode:  parseNames,
		evaluate: c.evaluating(flake, func(locked lockedFlake) []string {
			return evalArgs(callExpr("attrs", append(scope.args(locked), listArg("path", parent))...))
		}),
	}
}

func (c *Client) packageQuery(flake flakeState, scope inputScope, path []string) query[*Package] {
	detail := pathDetail(path)
	return query[*Package]{
		results: c.packages,
		key:     scope.queryKey(queryPackage, detail),
		slot:    flake.slot(queryPackage, detail),
		decode:  parsePackage,
		evaluate: c.evaluating(flake, func(locked lockedFlake) []string {
			return evalArgs(callExpr("package", append(scope.args(locked), listArg("path", path))...))
		}),
	}
}

func (c *Client) resolve(ctx context.Context, flake flakeState) (lockedFlake, error) {
	locked, err := c.locked.await(ctx, flake.local, func(ctx context.Context, work task) (computed[lockedFlake], error) {
		out, err := c.runBounded(ctx, work, flake.dir, metadataArgs())
		if err != nil {
			return computed[lockedFlake]{}, err
		}
		value, err := parseMetadata(out)
		if err != nil {
			return computed[lockedFlake]{}, err
		}
		return computed[lockedFlake]{value: value, cost: len(out)}, nil
	})
	if err != nil {
		return lockedFlake{}, fmt.Errorf("resolving the flake: %w", err)
	}
	return locked, nil
}

func (c *Client) currentSystem(ctx context.Context) (string, error) {
	return c.system.await(ctx, contentKey{}, func(ctx context.Context, _ task) (computed[string], error) {
		out, err := c.run(ctx, "", systemArgs())
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

func (c *Client) runBounded(ctx context.Context, work task, dir string, args []string) ([]byte, error) {
	if work.priority == background {
		select {
		case c.lowSlots <- struct{}{}:
			defer func() { <-c.lowSlots }()
		case <-work.promoted:
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for a free background nix process slot: %w", ctx.Err())
		}
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for a free nix process slot: %w", ctx.Err())
	}
	return c.run(ctx, dir, args)
}

func pathDetail(path []string) string {
	return fmt.Sprintf("%q", path)
}

func readFlakeFiles(dir string) (nix, lock []byte, err error) {
	nix, _, err = regularfile.Read(filepath.Join(dir, "flake.nix"), MaxFlakeFile)
	if err != nil {
		return nil, nil, fmt.Errorf("reading flake.nix: %w", err)
	}
	lock, _, err = regularfile.Read(filepath.Join(dir, "flake.lock"), MaxFlakeFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nix, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading flake.lock: %w", err)
	}
	return nix, lock, nil
}
