package dispatcher

import (
	"context"
	"errors"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type fakeStore struct {
	event     domain.OutboxEvent
	found     bool
	completed int
	failed    int
}

func (f *fakeStore) ClaimOutboxEvent(context.Context) (domain.OutboxEvent, bool, error) {
	if !f.found {
		return domain.OutboxEvent{}, false, nil
	}
	f.found = false
	return f.event, true, nil
}
func (f *fakeStore) CompleteOutboxEvent(context.Context, string) error    { f.completed++; return nil }
func (f *fakeStore) FailOutboxEvent(context.Context, string, error) error { f.failed++; return nil }

type fakePublisher struct {
	err       error
	published int
}

type panickingPublisher struct{}

func (panickingPublisher) Publish(context.Context, domain.OutboxEvent) error {
	panic("poison event")
}
func (panickingPublisher) Close() {}

func (f *fakePublisher) Publish(context.Context, domain.OutboxEvent) error {
	f.published++
	return f.err
}
func (f *fakePublisher) Close() {}

func TestDrainCompletesPublishedEvent(t *testing.T) {
	store := &fakeStore{event: domain.OutboxEvent{ID: "1"}, found: true}
	publisher := &fakePublisher{}
	count, err := Drain(context.Background(), store, publisher, 1, false)
	if err != nil || count != 1 || store.completed != 1 || publisher.published != 1 {
		t.Fatalf("count=%d completed=%d published=%d err=%v", count, store.completed, publisher.published, err)
	}
}

func TestDrainRecordsPublishFailure(t *testing.T) {
	store := &fakeStore{event: domain.OutboxEvent{ID: "1"}, found: true}
	publisher := &fakePublisher{err: errors.New("unavailable")}
	count, err := Drain(context.Background(), store, publisher, 1, false)
	if err != nil || count != 0 || store.failed != 1 {
		t.Fatalf("count=%d failed=%d err=%v", count, store.failed, err)
	}
}

func TestDrainRecoversPanickingPublisherAndDeadLettersEvent(t *testing.T) {
	store := &fakeStore{event: domain.OutboxEvent{ID: "poison"}, found: true}
	count, err := Drain(context.Background(), store, panickingPublisher{}, 1, false)
	if err != nil {
		t.Fatalf("Drain returned panic instead of dead-lettering: %v", err)
	}
	if count != 0 || store.failed != 1 {
		t.Fatalf("count=%d failed=%d, want one failed poison event", count, store.failed)
	}
}
