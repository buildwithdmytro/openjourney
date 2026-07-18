package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	formdefinition "github.com/buildwithdmytro/openjourney/internal/forms"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type publicSubmitStore struct {
	*fakeStore
	form         domain.Form
	version      domain.FormVersion
	events       []domain.Event
	submissions  int
	acceptedKeys map[string]string
}

func (s *publicSubmitStore) GetPublishedForm(context.Context, string) (domain.Form, domain.FormVersion, error) {
	return s.form, s.version, nil
}

func (s *publicSubmitStore) GetFirstAppID(context.Context, string, string) (string, error) {
	return "app-1", nil
}

func (s *publicSubmitStore) RecordFormSubmission(context.Context, domain.Principal, string, int, json.RawMessage, json.RawMessage, string) error {
	s.submissions++
	return nil
}

func (s *publicSubmitStore) AcceptEvents(_ context.Context, _ domain.Principal, events []domain.Event) ([]string, error) {
	ids := make([]string, len(events))
	for i, event := range events {
		if _, exists := s.acceptedKeys[event.IdempotencyKey]; !exists {
			s.acceptedKeys[event.IdempotencyKey] = "event-" + event.IdempotencyKey
			s.events = append(s.events, event)
		}
		ids[i] = s.acceptedKeys[event.IdempotencyKey]
	}
	return ids, nil
}

func newPublicSubmitHandler(t *testing.T, limiter *IPRateLimiter) (*publicSubmitStore, http.Handler, string) {
	t.Helper()
	draft := json.RawMessage(`{"fields":[{"key":"email","type":"email","required":true,"maps_to":"email"},{"key":"consent","type":"boolean","consent":true}]}`)
	definition, err := formdefinition.CanonicalizeDraft(draft)
	if err != nil {
		t.Fatal(err)
	}
	store := &publicSubmitStore{
		fakeStore:    &fakeStore{Store: ports.Store(nil)},
		form:         domain.Form{ID: "form-1", TenantID: "tenant-1", WorkspaceID: "workspace-1", Status: "published"},
		version:      domain.FormVersion{FormID: "form-1", Version: 1, Definition: definition},
		acceptedKeys: make(map[string]string),
	}
	handler := NewWithSessionTTL(store, 20, nil, "*", time.Hour, WithPublicGuard(limiter, nil, false))
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	token, err := SignFormToken("form-1", 1, expires, []byte("change-me-in-production"))
	if err != nil {
		t.Fatal(err)
	}
	return store, handler, token
}

func TestPublicFormSubmitDefensesAndIdempotency(t *testing.T) {
	store, handler, token := newPublicSubmitHandler(t, NewIPRateLimiter(1, 10))
	body := `{"form_token":"` + token + `","values":{"email":"person@example.com","consent":true},"utm":{"source":"newsletter"}}`
	request := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/f/form-1", strings.NewReader(body))
		r.RemoteAddr = "198.51.100.10:1234"
		r.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		return recorder
	}
	if got := request(body).Code; got != http.StatusAccepted {
		t.Fatalf("valid submission status = %d", got)
	}
	if got := request(body).Code; got != http.StatusAccepted {
		t.Fatalf("idempotent retry status = %d", got)
	}
	if len(store.events) != 3 || store.submissions != 2 {
		t.Fatalf("events=%d submissions=%d, want 3 unique events and two harmless persistence retries", len(store.events), store.submissions)
	}
	var consent domain.Event
	for _, event := range store.events {
		if event.Type == "consent.changed" {
			consent = event
		}
	}
	var consentPayload struct {
		Evidence map[string]any `json:"evidence"`
	}
	if json.Unmarshal(consent.Payload, &consentPayload) != nil || consentPayload.Evidence["form_id"] != "form-1" || consentPayload.Evidence["ip"] != "198.51.100.10" {
		t.Fatalf("consent evidence = %s", consent.Payload)
	}

	_, invalidHandler, invalidToken := newPublicSubmitHandler(t, NewIPRateLimiter(1, 10))
	invalidBody := `{"form_token":"` + invalidToken + `","values":{"email":"not-an-email"}}`
	r := httptest.NewRequest(http.MethodPost, "/f/form-1", strings.NewReader(invalidBody))
	r.RemoteAddr = "198.51.100.14:1234"
	w := httptest.NewRecorder()
	invalidHandler.ServeHTTP(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid schema status = %d", w.Code)
	}
	_, expiredHandler, _ := newPublicSubmitHandler(t, NewIPRateLimiter(1, 10))
	expiredToken, err := SignFormToken("form-1", 1, time.Now().Add(-time.Minute), []byte("change-me-in-production"))
	if err != nil {
		t.Fatal(err)
	}
	expiredBody := `{"form_token":"` + expiredToken + `","values":{"email":"person@example.com"}}`
	r = httptest.NewRequest(http.MethodPost, "/f/form-1", strings.NewReader(expiredBody))
	r.RemoteAddr = "198.51.100.15:1234"
	w = httptest.NewRecorder()
	expiredHandler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expired token status = %d", w.Code)
	}

	_, honeypotHandler, validToken := newPublicSubmitHandler(t, NewIPRateLimiter(1, 10))
	honeypot := `{"form_token":"` + validToken + `","honeypot":"bot","values":{"email":"bot@example.com"}}`
	if got := func() int {
		r := httptest.NewRequest(http.MethodPost, "/f/form-1", strings.NewReader(honeypot))
		r.RemoteAddr = "198.51.100.11:1234"
		w := httptest.NewRecorder()
		honeypotHandler.ServeHTTP(w, r)
		return w.Code
	}(); got != http.StatusOK {
		t.Fatalf("honeypot status = %d", got)
	}

	_, badTokenHandler, _ := newPublicSubmitHandler(t, NewIPRateLimiter(1, 10))
	bad := `{"form_token":"tampered","values":{"email":"person@example.com"}}`
	r = httptest.NewRequest(http.MethodPost, "/f/form-1", strings.NewReader(bad))
	r.RemoteAddr = "198.51.100.12:1234"
	w = httptest.NewRecorder()
	badTokenHandler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("bad token status = %d", w.Code)
	}

	_, limitedHandler, limitedToken := newPublicSubmitHandler(t, NewIPRateLimiter(0, 1))
	limitedBody := `{"form_token":"` + limitedToken + `","values":{"email":"person@example.com"}}`
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest(http.MethodPost, "/f/form-1", strings.NewReader(limitedBody))
		r.RemoteAddr = "198.51.100.13:1234"
		w := httptest.NewRecorder()
		limitedHandler.ServeHTTP(w, r)
		if i == 1 && w.Code != http.StatusTooManyRequests {
			t.Fatalf("over-limit status = %d", w.Code)
		}
	}
}
