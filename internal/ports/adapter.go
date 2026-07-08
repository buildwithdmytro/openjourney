package ports

import (
	"context"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

// RenderedMessage represents a message rendered and ready for transmission.
type RenderedMessage struct {
	Channel  string                 `json:"channel"`
	Endpoint string                 `json:"endpoint"`
	Subject  string                 `json:"subject,omitempty"`
	HTML     string                 `json:"html,omitempty"`
	Text     string                 `json:"text,omitempty"`
	Body     string                 `json:"body,omitempty"`
	Identity       domain.SendingIdentity `json:"identity"`
	IdempotencyKey string                 `json:"idempotency_key,omitempty"`
}

// ChannelAdapter is the interface that message delivery channel integrations (like SES, Webhooks) must implement.
type ChannelAdapter interface {
	Send(ctx context.Context, msg RenderedMessage) (providerID string, err error)
	ValidateConfig(iden domain.SendingIdentity) error
}
