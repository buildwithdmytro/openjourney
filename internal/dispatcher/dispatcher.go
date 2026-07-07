package dispatcher

import (
	"context"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type Store interface {
	ClaimOutboxEvent(context.Context) (domain.OutboxEvent, bool, error)
	CompleteOutboxEvent(context.Context, string) error
	FailOutboxEvent(context.Context, string, error) error
}

func Drain(ctx context.Context, store Store, publisher ports.EventPublisher, maxItems int, watch bool) (int, error) {
	processed := 0
	for processed < maxItems {
		event, found, err := store.ClaimOutboxEvent(ctx)
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
		if err := publisher.Publish(ctx, event); err != nil {
			if failErr := store.FailOutboxEvent(ctx, event.ID, err); failErr != nil {
				return processed, failErr
			}
			continue
		}
		if err := store.CompleteOutboxEvent(ctx, event.ID); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}
