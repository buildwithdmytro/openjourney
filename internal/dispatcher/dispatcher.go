package dispatcher

import (
	"context"
	"fmt"
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
		completed, err := publishOne(ctx, store, publisher, event)
		if err != nil {
			return processed, err
		}
		if !completed {
			continue
		}
		processed++
	}
	return processed, nil
}

// publishOne isolates recovery to one claimed event so a poison event cannot
// terminate the dispatcher fleet. FailOutboxEvent retains retry/backoff/DLQ.
func publishOne(ctx context.Context, store Store, publisher ports.EventPublisher, event domain.OutboxEvent) (completed bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicErr := fmt.Errorf("publish event panic: %v", recovered)
			if failErr := store.FailOutboxEvent(ctx, event.ID, panicErr); failErr != nil {
				err = fmt.Errorf("recover publish event %s: %w (fail event: %v)", event.ID, panicErr, failErr)
				return
			}
			err = nil
			completed = false
		}
	}()

	if err := publisher.Publish(ctx, event); err != nil {
		if failErr := store.FailOutboxEvent(ctx, event.ID, err); failErr != nil {
			return false, failErr
		}
		return false, nil
	}
	if err := store.CompleteOutboxEvent(ctx, event.ID); err != nil {
		return false, err
	}
	return true, nil
}
