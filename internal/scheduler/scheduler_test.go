package scheduler

import (
	"context"
	"sync"
	"testing"
)

type fakeStore struct {
	mu        sync.Mutex
	available int
	claimed   int
}

func (s *fakeStore) ClaimDueConnectorPipeline(context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.available == 0 {
		return false, nil
	}
	s.available--
	s.claimed++
	return true, nil
}

func TestDrainClaimsExactlyDuePipelines(t *testing.T) {
	store := &fakeStore{available: 2}
	got, err := Drain(context.Background(), store, 10, false)
	if err != nil || got != 2 || store.claimed != 2 {
		t.Fatalf("drain claimed %d (store %d), err=%v", got, store.claimed, err)
	}
}

func TestConcurrentDrainsDoNotDoubleClaim(t *testing.T) {
	store := &fakeStore{available: 1}
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, err := Drain(context.Background(), store, 1, false)
			if err != nil {
				t.Errorf("drain: %v", err)
			}
			results <- claimed
		}()
	}
	wg.Wait()
	close(results)
	total := 0
	for claimed := range results {
		total += claimed
	}
	if total != 1 || store.claimed != 1 {
		t.Fatalf("concurrent drains claimed %d (store %d), want one", total, store.claimed)
	}
}
