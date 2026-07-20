// Package export drains the event-export outbox topic into configured sink
// connectors. It intentionally has a topic-specific claim path so the normal
// event dispatcher cannot consume export deliveries.
package export

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/connector"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/extension"
)

type Store interface {
	ClaimExportOutboxEvent(context.Context) (domain.OutboxEvent, bool, error)
	CompleteOutboxEvent(context.Context, string) error
	FailOutboxEvent(context.Context, string, error) error
	GetConnectorPipeline(context.Context, domain.Principal, string) (domain.ConnectorPipeline, error)
	GetConnectorPipelineVersion(context.Context, domain.Principal, string) (domain.ConnectorPipelineVersion, error)
	GetExtensionConfig(context.Context, domain.Principal, string) (domain.ExtensionConfig, error)
}

// Drain delivers at most maxItems claimed outbox records. A missing deadline
// is replaced with a bounded one for each sink call; failed records remain
// pending through the standard outbox retry path.
func Drain(ctx context.Context, store Store, maxItems int, watch bool) (int, error) {
	return DrainWithRegistry(ctx, store, connector.DefaultRegistry(), maxItems, watch)
}

func DrainWithRegistry(ctx context.Context, store Store, registry *connector.Registry, maxItems int, watch bool) (int, error) {
	if store == nil || registry == nil {
		return 0, errors.New("export relay requires store and registry")
	}
	processed := 0
	for processed < maxItems {
		event, found, err := store.ClaimExportOutboxEvent(ctx)
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
		if err := deliver(ctx, store, registry, event); err != nil {
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

func deliver(ctx context.Context, store Store, registry *connector.Registry, event domain.OutboxEvent) error {
	var envelope struct {
		PipelineIDs []string        `json:"export_pipeline_ids"`
		EventID     string          `json:"event_id"`
		TenantID    string          `json:"tenant_id"`
		WorkspaceID string          `json:"workspace_id"`
		AppID       string          `json:"app_id"`
		Payload     json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(event.Payload, &envelope); err != nil {
		return fmt.Errorf("decode export envelope: %w", err)
	}
	if envelope.EventID == "" {
		envelope.EventID = event.EventID
	}
	if len(envelope.PipelineIDs) == 0 {
		return errors.New("export envelope has no pipelines")
	}
	for _, pipelineID := range envelope.PipelineIDs {
		p := domain.Principal{TenantID: event.TenantID, WorkspaceID: envelope.WorkspaceID, AppID: envelope.AppID, ActorType: "connector"}
		pipeline, err := store.GetConnectorPipeline(ctx, p, pipelineID)
		if err != nil {
			return err
		}
		if pipeline.CurrentVersionID == nil {
			return fmt.Errorf("export pipeline %s has no published version", pipelineID)
		}
		version, err := store.GetConnectorPipelineVersion(ctx, p, *pipeline.CurrentVersionID)
		if err != nil {
			return err
		}
		cfg, err := store.GetExtensionConfig(ctx, p, pipeline.ConnectorExtensionID)
		if err != nil {
			return err
		}
		resolved, err := extension.ResolveConfigMap(cfg.Config)
		if err != nil {
			return err
		}
		resolved["pipeline_id"] = pipelineID
		rows := []connector.Row{{"event_id": envelope.EventID, "payload": json.RawMessage(envelope.Payload)}}
		if mapping := version.Mapping; len(mapping) > 0 {
			resolved["mapping"] = mapping
		}
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err = registry.For(stringValue(resolved, "driver")).Write(callCtx, resolved, rows)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

func stringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}
