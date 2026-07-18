package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type extensionInvoker interface {
	Invoke(context.Context, domain.Principal, string, string, json.RawMessage) (json.RawMessage, string, error)
}

var errTransformRejected = errors.New("ingestion transform rejected the event")

// applyIngestionTransforms runs subscribed transforms after schema validation and before
// acceptance. A transform can add or change only payload annotations: every input key and
// value must remain unchanged in the returned object.
func (s *Server) applyIngestionTransforms(ctx context.Context, p domain.Principal, events []domain.Event) error {
	for i := range events {
		exts, err := s.store.ListActiveIngestionTransforms(ctx, p, events[i].Type)
		if err != nil {
			return fmt.Errorf("list ingestion transforms: %w", err)
		}
		for _, ext := range exts {
			version, err := s.store.GetExtensionVersion(ctx, p, valueOrEmpty(ext.CurrentVersionID))
			if err != nil {
				return fmt.Errorf("resolve ingestion transform %s: %w", ext.ID, err)
			}
			output, _, invokeErr := s.extensionInvoker.Invoke(ctx, p, ext.ID, "transform", events[i].Payload)
			if invokeErr != nil {
				if transformOnError(version.Manifest) == "passthrough" {
					continue
				}
				return fmt.Errorf("%w: %s", errTransformRejected, invokeErr)
			}
			payload, err := enrichmentPayload(events[i].Payload, output)
			if err != nil {
				if transformOnError(version.Manifest) == "passthrough" {
					continue
				}
				return fmt.Errorf("%w: %s", errTransformRejected, err)
			}
			events[i].Payload = payload
		}
	}
	return nil
}

func transformOnError(manifest json.RawMessage) string {
	var cfg struct {
		OnError string `json:"on_error"`
	}
	if json.Unmarshal(manifest, &cfg) == nil && cfg.OnError == "passthrough" {
		return "passthrough"
	}
	return "reject"
}

func enrichmentPayload(input, output json.RawMessage) (json.RawMessage, error) {
	var before, after map[string]any
	if err := json.Unmarshal(input, &before); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(output, &after); err != nil {
		return nil, fmt.Errorf("output must be a JSON object: %w", err)
	}
	for key, value := range before {
		if next, ok := after[key]; !ok || !reflect.DeepEqual(value, next) {
			return nil, fmt.Errorf("transform attempted to modify payload field %q", key)
		}
	}
	return json.Marshal(after)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
