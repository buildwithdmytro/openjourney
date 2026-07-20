package channels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// InAppAdapter implements ports.ChannelAdapter for in-app message delivery.
type InAppAdapter struct {
	store ports.Store
}

// NewInAppAdapter creates a new InAppAdapter with the given store.
func NewInAppAdapter(store ports.Store) *InAppAdapter {
	return &InAppAdapter{
		store: store,
	}
}

// Send writes an in-app message row to inapp_messages, inheriting scoping from the target profile.
// The endpoint is the profile_id; tenant/workspace/app are fetched from the profile row.
func (a *InAppAdapter) Send(ctx context.Context, msg ports.RenderedMessage) (string, error) {
	if msg.Identity.Channel != "in_app" {
		return "", &DeliveryError{Err: fmt.Errorf("invalid channel for in-app: %s", msg.Identity.Channel), Retryable: false}
	}

	// endpoint is the profile_id
	profileID := msg.Endpoint
	if profileID == "" {
		return "", &DeliveryError{Err: errors.New("empty profile endpoint for in-app"), Retryable: false}
	}

	// Get the profile's app_id (tenant/workspace come from the sending identity)
	appID, err := a.store.GetProfileAppID(ctx, msg.Identity.TenantID, msg.Identity.WorkspaceID, profileID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return "", &DeliveryError{Err: fmt.Errorf("profile not found: %s", profileID), Retryable: false}
		}
		return "", &DeliveryError{Err: fmt.Errorf("failed to get profile app_id: %w", err), Retryable: true}
	}

	// Map RenderedMessage to content JSON
	content := map[string]interface{}{
		"subject": msg.Subject,
		"title":   msg.Title,
		"html":    msg.HTML,
		"text":    msg.Text,
		"body":    msg.Body,
	}
	if len(msg.Data) > 0 {
		content["data"] = msg.Data
	}

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return "", &DeliveryError{Err: fmt.Errorf("failed to marshal content: %w", err), Retryable: false}
	}

	// Prepare the InAppMessage
	now := time.Now().UTC()
	inappMsg := domain.InAppMessage{
		MessageType:    "modal", // default type
		Content:        contentJSON,
		Rank:           0,
		Categories:     []string{},
		StartAt:        now,
		Status:         "delivered",
		DeliveredAt:    &now,
		IdempotencyKey: &msg.IdempotencyKey,
	}

	// Create the in-app message
	created, err := a.store.CreateInAppMessage(
		ctx,
		msg.Identity.TenantID,
		msg.Identity.WorkspaceID,
		appID,
		profileID,
		inappMsg,
	)
	if err != nil {
		return "", &DeliveryError{Err: fmt.Errorf("failed to create in-app message: %w", err), Retryable: false}
	}

	return created.ID, nil
}

// ValidateConfig verifies that the in-app identity is properly configured.
func (a *InAppAdapter) ValidateConfig(iden domain.SendingIdentity) error {
	if iden.Channel != "in_app" {
		return fmt.Errorf("in-app channel must be in_app, got: %s", iden.Channel)
	}

	if iden.Provider != "inapp" {
		return fmt.Errorf("in-app provider must be inapp, got: %s", iden.Provider)
	}

	return nil
}
