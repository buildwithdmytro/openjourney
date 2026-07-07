package channels

import (
	"context"
	"errors"
	"sync"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// FakeAdapter is a thread-safe ChannelAdapter mock for unit tests and local sandboxed runs.
type FakeAdapter struct {
	mu    sync.Mutex
	Sends []ports.RenderedMessage
}

// NewFakeAdapter creates an initialized FakeAdapter.
func NewFakeAdapter() *FakeAdapter {
	return &FakeAdapter{
		Sends: make([]ports.RenderedMessage, 0),
	}
}

// Send records the rendered message in memory and returns a mock provider ID.
func (f *FakeAdapter) Send(ctx context.Context, msg ports.RenderedMessage) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Sends = append(f.Sends, msg)
	return "fake-msg-id-" + msg.Endpoint, nil
}

// ValidateConfig verifies that the SendingIdentity is logically complete.
func (f *FakeAdapter) ValidateConfig(iden domain.SendingIdentity) error {
	if iden.Channel == "" {
		return errors.New("channel is required")
	}
	return nil
}

// Reset clears all captured sends.
func (f *FakeAdapter) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Sends = f.Sends[:0]
}

// GetSends returns a copy of all recorded sends.
func (f *FakeAdapter) GetSends() []ports.RenderedMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := make([]ports.RenderedMessage, len(f.Sends))
	copy(copied, f.Sends)
	return copied
}
