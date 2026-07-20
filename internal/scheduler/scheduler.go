// Package scheduler drains due connector pipelines into the leased operation
// queue. Claiming and enqueueing are one database transaction in the store.
package scheduler

import (
	"context"
	"time"
)

type Store interface {
	ClaimDueConnectorPipeline(context.Context) (bool, error)
}

const defaultPollInterval = 500 * time.Millisecond

// Drain claims at most maxItems due pipelines. In watch mode it keeps polling
// until the context is cancelled, like the operations worker.
func Drain(ctx context.Context, store Store, maxItems int, watch bool) (int, error) {
	return drain(ctx, store, maxItems, watch, defaultPollInterval)
}

func drain(ctx context.Context, store Store, maxItems int, watch bool, pollInterval time.Duration) (int, error) {
	if maxItems < 1 {
		return 0, nil
	}
	claimed := 0
	for claimed < maxItems {
		found, err := store.ClaimDueConnectorPipeline(ctx)
		if err != nil {
			return claimed, err
		}
		if found {
			claimed++
			continue
		}
		if !watch {
			return claimed, nil
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return claimed, nil
		case <-timer.C:
		}
	}
	return claimed, nil
}
