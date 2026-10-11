package server

import (
	"context"
	"testing"
)

func TestYieldSlotFreesTheSlotWhileWaiting(t *testing.T) {
	t.Parallel()

	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	ctx := context.WithValue(t.Context(), requestSlotKey{}, sem)
	got, err := yieldSlot(ctx, func() (string, error) {
		select {
		case sem <- struct{}{}:
			<-sem
			return "freed", nil
		default:
			return "held", nil
		}
	})
	if err != nil || got != "freed" {
		t.Errorf("during the wait the slot was %s, want it free for other requests", got)
	}
	select {
	case sem <- struct{}{}:
		t.Error("the slot was not taken back after the wait")
	default:
	}

	if got, _ := yieldSlot(t.Context(), func() (string, error) { return "ran", nil }); got != "ran" {
		t.Errorf("without a slot the wait = %q, want it run directly", got)
	}
}
