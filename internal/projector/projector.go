package projector

import (
	"context"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type Store interface {
	ClaimProjectionJob(context.Context) (domain.AcceptedEvent, bool, error)
	ProjectEvent(context.Context, domain.AcceptedEvent) error
	FailProjectionJob(context.Context, string, error) error
}

type Options struct {
	AfterClaimDelay time.Duration
}

func Drain(ctx context.Context, store Store, maxItems int, watch bool) (int, error) {
	return DrainWithOptions(ctx, store, maxItems, watch, Options{})
}

func DrainWithOptions(ctx context.Context, store Store, maxItems int, watch bool, opts Options) (int, error) {
	processed := 0
	for processed < maxItems {
		event, found, err := store.ClaimProjectionJob(ctx)
		if err != nil {
			return processed, err
		}
		if !found {
			if !watch {
				return processed, nil
			}
			select {
			case <-ctx.Done():
				return processed, nil
			case <-time.After(500 * time.Millisecond):
				continue
			}
		}
		if opts.AfterClaimDelay > 0 {
			select {
			case <-ctx.Done():
				return processed, nil
			case <-time.After(opts.AfterClaimDelay):
			}
		}
		if err := store.ProjectEvent(ctx, event); err != nil {
			if failErr := store.FailProjectionJob(ctx, event.ID, err); failErr != nil {
				return processed, failErr
			}
			continue
		}
		processed++
	}
	return processed, nil
}
