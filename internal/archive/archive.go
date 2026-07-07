package archive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/stream"
)

type envelope struct {
	EventID    string          `json:"event_id"`
	TenantID   string          `json:"tenant_id"`
	EventType  string          `json:"event_type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}

func Run(ctx context.Context, consumer *stream.Consumer, blobs ports.BlobStore) error {
	for {
		record, err := consumer.Poll(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		var event envelope
		if err := json.Unmarshal(record.Value, &event); err != nil {
			return fmt.Errorf("decode event: %w", err)
		}
		if event.EventType == "privacy.deleted" {
			var payload struct {
				ObjectKeys []string `json:"object_keys"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return err
			}
			for _, key := range payload.ObjectKeys {
				if err := blobs.Delete(ctx, key); err != nil {
					return err
				}
			}
			if err := consumer.Commit(ctx, record); err != nil {
				return err
			}
			continue
		}
		key := fmt.Sprintf("events/%s/%s/%s.json", event.TenantID,
			event.OccurredAt.UTC().Format("2006/01/02"), event.EventID)
		if err := blobs.Put(ctx, key, record.Value, "application/json"); err != nil {
			return err
		}
		if err := consumer.Commit(ctx, record); err != nil {
			return err
		}
	}
}
