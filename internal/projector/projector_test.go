package projector

import (
	"context"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type panicProjectorStore struct {
	event   domain.AcceptedEvent
	claimed bool
	failed  int
	dead    bool
}

func (s *panicProjectorStore) ClaimProjectionJob(context.Context) (domain.AcceptedEvent, bool, error) {
	if s.claimed {
		return domain.AcceptedEvent{}, false, nil
	}
	s.claimed = true
	return s.event, true, nil
}

func (s *panicProjectorStore) ProjectEvent(context.Context, domain.AcceptedEvent) error {
	panic("poison projection")
}

func (s *panicProjectorStore) FailProjectionJob(_ context.Context, _ string, _ error) error {
	s.failed++
	s.dead = true
	return nil
}

func TestDrainRecoversPanickingProjectorAndDeadLettersEvent(t *testing.T) {
	store := &panicProjectorStore{event: domain.AcceptedEvent{ID: "poison"}}
	count, err := Drain(context.Background(), store, 1, false)
	if err != nil {
		t.Fatalf("Drain returned panic instead of dead-lettering: %v", err)
	}
	if count != 0 || store.failed != 1 || !store.dead {
		t.Fatalf("count=%d failed=%d dead=%v, want dead-lettered poison event", count, store.failed, store.dead)
	}
}
