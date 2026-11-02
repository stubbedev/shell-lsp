package nixeval

import (
	"errors"
	"fmt"
)

var (
	ErrSuperseded = errors.New("a newer state of the flake replaced this evaluation")
	ErrClosed     = errors.New("the nixeval client is closed")
)

type Basis int

const (
	BasisEarlierState Basis = iota
	BasisLockedNixpkgs
)

func (b Basis) String() string {
	return [...]string{BasisEarlierState: "an earlier state of the flake", BasisLockedNixpkgs: "the locked nixpkgs without the modules of the flake"}[b]
}

type Refresh struct {
	done <-chan struct{}
	err  *error
}

func (r *Refresh) Done() <-chan struct{} {
	return r.done
}

func (r *Refresh) Err() error {
	select {
	case <-r.done:
		return *r.err
	default:
		return nil
	}
}

type Stale struct {
	Basis   Basis
	Refresh *Refresh
}

type Answer[T any] struct {
	value T
	stale *Stale
}

func (a Answer[T]) Get() (T, *Stale) {
	return a.value, a.stale
}

func mapAnswer[T, U any](a Answer[T], convert func(T) U) Answer[U] {
	return Answer[U]{value: convert(a.value), stale: a.stale}
}

type NotReadyError struct {
	Refresh *Refresh
	Err     error
}

func (e *NotReadyError) Error() string {
	return fmt.Sprintf("the evaluation continues in the background: %v", e.Err)
}

func (e *NotReadyError) Unwrap() error {
	return e.Err
}

func NewAnswer[T any](value T, stale *Stale) Answer[T] {
	return Answer[T]{value: value, stale: stale}
}

func NewRefresh() (*Refresh, func(error)) {
	done := make(chan struct{})
	failure := new(error)
	return &Refresh{done: done, err: failure}, func(err error) {
		*failure = err
		close(done)
	}
}
