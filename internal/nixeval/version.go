package nixeval

import (
	"cmp"
	"context"
	"fmt"
	"strconv"
	"strings"
)

const (
	versionSegments = 3
	supportedNix    = "Nix 2.24 or newer, Lix 2.95 or newer, or Determinate Nix based on Nix 2.24 or newer"
)

type nixVariant int

const (
	variantUnrecognised nixVariant = iota
	variantNix
	variantLix
	variantDeterminate
)

type nixRelease struct {
	variant  nixVariant
	numbers  [versionSegments]int
	reported string
}

var minimumRelease = map[nixVariant][versionSegments]int{
	variantNix:         {2, 24, 0},
	variantLix:         {2, 95, 0},
	variantDeterminate: {2, 24, 0},
}

func parseRelease(output []byte) nixRelease {
	first, _, _ := strings.Cut(string(output), "\n")
	line := strings.TrimSpace(first)
	release := nixRelease{reported: line}
	inner, found := strings.CutPrefix(line, nixProgram+" (")
	if !found {
		return release
	}
	product, version, found := strings.Cut(inner, ") ")
	if !found {
		return release
	}
	numbers, ok := parseVersionNumbers(version)
	if !ok {
		return release
	}
	release.numbers = numbers
	switch {
	case product == "Nix":
		release.variant = variantNix
	case strings.HasPrefix(product, "Lix"):
		release.variant = variantLix
	case strings.HasPrefix(product, "Determinate Nix"):
		release.variant = variantDeterminate
	}
	return release
}

func parseVersionNumbers(version string) ([versionSegments]int, bool) {
	var numbers [versionSegments]int
	segments := strings.SplitN(version, ".", versionSegments)
	if len(segments) < versionSegments-1 {
		return numbers, false
	}
	for i, segment := range segments {
		digits := segment
		if end := strings.IndexFunc(segment, func(r rune) bool { return r < '0' || r > '9' }); end >= 0 {
			digits = segment[:end]
		}
		number, err := strconv.Atoi(digits)
		if err != nil {
			return numbers, false
		}
		numbers[i] = number
	}
	return numbers, true
}

func (r nixRelease) supported() error {
	minimum, known := minimumRelease[r.variant]
	if !known {
		return &UnsupportedNixError{Reported: r.reported}
	}
	for i, number := range r.numbers {
		if order := cmp.Compare(number, minimum[i]); order != 0 {
			if order < 0 {
				return &UnsupportedNixError{Reported: r.reported}
			}
			return nil
		}
	}
	return nil
}

type UnsupportedNixError struct {
	Reported string
}

func (e *UnsupportedNixError) Error() string {
	return fmt.Sprintf("shell-lsp needs %s to evaluate flakes, but nix --version reports %q", supportedNix, e.Reported)
}

func (c *Client) nixRelease(ctx context.Context, identity string) (nixRelease, error) {
	e := c.releases.join(digestOf("release", identity), slotKey{}, foreground, func(ctx context.Context, _ task) (computed[nixRelease], error) {
		out, err := c.version(ctx)
		if err != nil {
			return computed[nixRelease]{}, fmt.Errorf("finding the version of %s: %w", identity, err)
		}
		return computed[nixRelease]{value: parseRelease(out), cost: len(out)}, nil
	})
	select {
	case <-e.done:
	case <-ctx.Done():
		return nixRelease{}, &NotReadyError{Refresh: e.refresh(), Err: ctx.Err()}
	}
	if e.err != nil {
		return nixRelease{}, e.err
	}
	return e.value, e.value.supported()
}
