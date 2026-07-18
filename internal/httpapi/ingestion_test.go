package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type ingestionStore struct {
	ports.Store
	ext      domain.Extension
	version  domain.ExtensionVersion
	accepted []domain.Event
}

func (s *ingestionStore) ListActiveIngestionTransforms(context.Context, domain.Principal, string) ([]domain.Extension, error) {
	return []domain.Extension{s.ext}, nil
}
func (s *ingestionStore) GetExtensionVersion(context.Context, domain.Principal, string) (domain.ExtensionVersion, error) {
	return s.version, nil
}
func (s *ingestionStore) ValidateEventSchema(context.Context, domain.Principal, domain.Event) error {
	return nil
}
func (s *ingestionStore) AcceptEvents(_ context.Context, _ domain.Principal, events []domain.Event) ([]string, error) {
	s.accepted = append(s.accepted, events...)
	return []string{"event-1"}, nil
}

type ingestionInvoker struct {
	output json.RawMessage
	err    error
}

func (i ingestionInvoker) Invoke(context.Context, domain.Principal, string, string, json.RawMessage) (json.RawMessage, string, error) {
	return i.output, "activity-1", i.err
}

func TestIngestionTransformEnrichesAndPreservesEventIdentity(t *testing.T) {
	versionID := "version-1"
	store := &ingestionStore{
		ext:     domain.Extension{ID: "extension-1", CurrentVersionID: &versionID, Status: "enabled"},
		version: domain.ExtensionVersion{ID: versionID, Manifest: json.RawMessage(`{"on_error":"reject"}`)},
	}
	s := &Server{store: store, maxBatchSize: 10, extensionInvoker: ingestionInvoker{output: json.RawMessage(`{"source":"sdk","value":42}`)}}
	// Build the request body separately so this test exercises the HTTP boundary.
	req := httptest.NewRequest(http.MethodPost, "/v1/events/batch", jsonBody(t, map[string]any{"events": []domain.Event{{
		Type: "custom.event", SchemaVersion: 1, ExternalID: "external-1", IdempotencyKey: "event-1",
		OccurredAt: time.Now().UTC(), Payload: json.RawMessage(`{"value":42}`),
	}}}))
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}))
	recorder := httptest.NewRecorder()
	s.acceptEvents(recorder, req)
	if recorder.Code != http.StatusAccepted || len(store.accepted) != 1 {
		t.Fatalf("accept status=%d body=%s accepted=%d", recorder.Code, recorder.Body.String(), len(store.accepted))
	}
	if string(store.accepted[0].Payload) != `{"source":"sdk","value":42}` || store.accepted[0].ExternalID != "external-1" {
		t.Fatalf("transform changed event unexpectedly: %+v", store.accepted[0])
	}
}

func TestIngestionTransformFailureUsesConfiguredPolicy(t *testing.T) {
	versionID := "version-1"
	base := &ingestionStore{ext: domain.Extension{ID: "extension-1", CurrentVersionID: &versionID, Status: "enabled"}, version: domain.ExtensionVersion{ID: versionID}}
	makeRequest := func(t *testing.T) *http.Request {
		return httptest.NewRequest(http.MethodPost, "/v1/events/batch", jsonBody(t, map[string]any{"events": []domain.Event{{
			Type: "custom.event", SchemaVersion: 1, ExternalID: "external-1", IdempotencyKey: "event-1", OccurredAt: time.Now().UTC(), Payload: json.RawMessage(`{"value":42}`),
		}}})).WithContext(context.WithValue(context.Background(), principalKey{}, domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}))
	}
	for _, tc := range []struct {
		name, manifest string
		wantStatus     int
		wantAccepted   bool
	}{
		{name: "reject", manifest: `{"on_error":"reject"}`, wantStatus: http.StatusUnprocessableEntity},
		{name: "passthrough", manifest: `{"on_error":"passthrough"}`, wantStatus: http.StatusAccepted, wantAccepted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := *base
			store.version.Manifest = json.RawMessage(tc.manifest)
			s := &Server{store: &store, maxBatchSize: 10, extensionInvoker: ingestionInvoker{err: errors.New("trap")}}
			recorder := httptest.NewRecorder()
			s.acceptEvents(recorder, makeRequest(t))
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if (len(store.accepted) > 0) != tc.wantAccepted {
				t.Fatalf("accepted=%d want=%t", len(store.accepted), tc.wantAccepted)
			}
		})
	}
}

func TestIngestionTransformDeadlineTrapNeverStallsAndUsesPassthrough(t *testing.T) {
	versionID := "version-1"
	store := &ingestionStore{
		ext:     domain.Extension{ID: "extension-1", CurrentVersionID: &versionID, Status: "enabled"},
		version: domain.ExtensionVersion{ID: versionID, Manifest: json.RawMessage(`{"on_error":"passthrough"}`)},
	}
	s := &Server{store: store, maxBatchSize: 10, extensionInvoker: ingestionInvoker{err: context.DeadlineExceeded}}
	req := httptest.NewRequest(http.MethodPost, "/v1/events/batch", jsonBody(t, map[string]any{"events": []domain.Event{{
		Type: "custom.event", SchemaVersion: 1, ExternalID: "external-1", IdempotencyKey: "event-1", OccurredAt: time.Now().UTC(), Payload: json.RawMessage(`{"value":42}`),
	}}})).WithContext(context.WithValue(context.Background(), principalKey{}, domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}))
	recorder := httptest.NewRecorder()
	s.acceptEvents(recorder, req)
	if recorder.Code != http.StatusAccepted || len(store.accepted) != 1 {
		t.Fatalf("deadline trap stalled/rejected passthrough: status=%d body=%s accepted=%d", recorder.Code, recorder.Body.String(), len(store.accepted))
	}
	if string(store.accepted[0].Payload) != `{"value":42}` {
		t.Fatalf("passthrough changed payload: %s", store.accepted[0].Payload)
	}
}

func jsonBody(t *testing.T, value any) *bytes.Reader {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(data)
}
