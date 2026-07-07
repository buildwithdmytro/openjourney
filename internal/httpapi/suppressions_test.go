package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type suppressionsFakeStore struct {
	fakeStore
	listCalled   bool
	createCalled bool
	deleteCalled bool
	lastChannel  string
	lastEndpoint string
	lastReason   string
}

func (f *suppressionsFakeStore) ListSuppressions(ctx context.Context, p domain.Principal) ([]domain.Suppression, error) {
	f.listCalled = true
	return []domain.Suppression{
		{
			ID:        "sup-1",
			TenantID:  p.TenantID,
			Channel:   "email",
			Endpoint:  "user@example.com",
			Reason:    "bounce",
			CreatedAt: time.Now(),
		},
	}, nil
}

func (f *suppressionsFakeStore) SuppressEndpoint(ctx context.Context, p domain.Principal, channel, endpoint, reason string) error {
	f.createCalled = true
	f.lastChannel = channel
	f.lastEndpoint = endpoint
	f.lastReason = reason
	return nil
}

func (f *suppressionsFakeStore) RemoveSuppression(ctx context.Context, p domain.Principal, channel, endpoint string) error {
	f.deleteCalled = true
	f.lastChannel = channel
	f.lastEndpoint = endpoint
	if endpoint == "notfound@example.com" {
		return errors.New("not found") // matches postgres.ErrNotFound since we have stringsContainsNotFound check
	}
	return nil
}

func TestListSuppressions(t *testing.T) {
	store := &suppressionsFakeStore{}
	store.scopes = []string{"suppressions:read"}
	server := New(store, 75)

	request := httptest.NewRequest(http.MethodGet, "/v1/suppressions", nil)
	request.Header.Set("Authorization", "Bearer test-key")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body=%s", response.Code, response.Body.String())
	}
	if !store.listCalled {
		t.Fatalf("expected ListSuppressions to be called")
	}
	if !strings.Contains(response.Body.String(), "user@example.com") {
		t.Fatalf("expected response to contain user@example.com, got %s", response.Body.String())
	}
}

func TestCreateSuppression(t *testing.T) {
	store := &suppressionsFakeStore{}
	store.scopes = []string{"suppressions:write"}
	server := New(store, 75)

	body := `{"channel":"email","endpoint":"test@example.com","reason":"unsubscribe"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/suppressions", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-key")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body=%s", response.Code, response.Body.String())
	}
	if !store.createCalled {
		t.Fatalf("expected SuppressEndpoint to be called")
	}
	if store.lastChannel != "email" || store.lastEndpoint != "test@example.com" || store.lastReason != "unsubscribe" {
		t.Fatalf("unexpected suppression arguments: %s %s %s", store.lastChannel, store.lastEndpoint, store.lastReason)
	}
}

func TestDeleteSuppression(t *testing.T) {
	store := &suppressionsFakeStore{}
	store.scopes = []string{"suppressions:write"}
	server := New(store, 75)

	request := httptest.NewRequest(http.MethodDelete, "/v1/suppressions?channel=email&endpoint=test@example.com", nil)
	request.Header.Set("Authorization", "Bearer test-key")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d. body=%s", response.Code, response.Body.String())
	}
	if !store.deleteCalled {
		t.Fatalf("expected RemoveSuppression to be called")
	}
	if store.lastChannel != "email" || store.lastEndpoint != "test@example.com" {
		t.Fatalf("unexpected delete arguments: %s %s", store.lastChannel, store.lastEndpoint)
	}
}
