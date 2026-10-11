package server

import "context"

type requestSlotKey struct{}

func yieldSlot[T any](ctx context.Context, wait func() (T, error)) (T, error) {
	sem, held := ctx.Value(requestSlotKey{}).(chan struct{})
	if !held {
		return wait()
	}
	<-sem
	defer func() { sem <- struct{}{} }()
	return wait()
}
