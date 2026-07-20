package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type connectorPipelineHTTPStore struct {
	ports.Store
	published domain.ConnectorPipelineVersion
}

func (s *connectorPipelineHTTPStore) PublishConnectorPipeline(_ context.Context, _ domain.Principal, id, publisher, manifest string, definition json.RawMessage, definitionSHA string) (domain.ConnectorPipelineVersion, error) {
	sum := sha256.Sum256(definition)
	if definitionSHA != fmt.Sprintf("%x", sum) {
		return domain.ConnectorPipelineVersion{}, errors.New("definition hash mismatch")
	}
	s.published = domain.ConnectorPipelineVersion{PipelineID: id, MappingKey: manifest, Mapping: definition, DefinitionSHA: definitionSHA, CreatedByUserID: &publisher}
	return s.published, nil
}

func TestPublishConnectorPipelineFreezesCanonicalDefinitionAndRequiresHuman(t *testing.T) {
	store := &connectorPipelineHTTPStore{}
	blobs := &fakeBlobStore{}
	server := &Server{store: store, blobStore: blobs}

	apiKeyReq := withPrincipal(httptest.NewRequest(http.MethodPost, "/v1/connectors/pipelines/p-1/publish", strings.NewReader(`{"mapping":{"b":2,"a":1}}`)), domain.Principal{ActorType: "api_key", TenantID: "tenant-1"})
	apiKeyReq.SetPathValue("id", "p-1")
	rec := httptest.NewRecorder()
	server.publishConnectorPipeline(rec, apiKeyReq)
	if rec.Code != http.StatusForbidden || store.published.PipelineID != "" || len(blobs.objects) != 0 {
		t.Fatalf("non-human publish was not blocked: status=%d body=%s", rec.Code, rec.Body.String())
	}
	enableReq := withPrincipal(httptest.NewRequest(http.MethodPut, "/v1/connectors/pipelines/p-1", strings.NewReader(`{"status":"enabled"}`)), domain.Principal{ActorType: "api_key", TenantID: "tenant-1"})
	enableReq.SetPathValue("id", "p-1")
	rec = httptest.NewRecorder()
	server.updateConnectorPipeline(rec, enableReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-human enable status=%d body=%s", rec.Code, rec.Body.String())
	}

	userReq := withPrincipal(httptest.NewRequest(http.MethodPost, "/v1/connectors/pipelines/p-1/publish", strings.NewReader(`{"mapping":{"b":2,"a":1}}`)), domain.Principal{ActorType: "user", UserID: "user-1", TenantID: "tenant-1"})
	userReq.SetPathValue("id", "p-1")
	rec = httptest.NewRecorder()
	server.publishConnectorPipeline(rec, userReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("human publish status=%d body=%s", rec.Code, rec.Body.String())
	}
	canonical := []byte(`{"a":1,"b":2}`)
	if string(store.published.Mapping) != string(canonical) {
		t.Fatalf("definition was not canonicalized: %s", store.published.Mapping)
	}
	if len(blobs.objects) != 1 {
		t.Fatalf("expected one frozen blob, got %d", len(blobs.objects))
	}
	sum := sha256.Sum256(canonical)
	if store.published.DefinitionSHA != fmt.Sprintf("%x", sum) {
		t.Fatalf("definition sha is unstable: %s", store.published.DefinitionSHA)
	}
}
