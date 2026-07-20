package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestIdentityCommandsEmitEventsAndGateMergeAndUnmerge(t *testing.T) {
	var events []domain.Event
	store := &fakeStore{}
	store.AcceptEventsFunc = func(_ context.Context, _ domain.Principal, input []domain.Event) ([]string, error) {
		events = append(events, input...)
		return []string{"event-1"}, nil
	}
	server := &Server{store: store}

	identify := withPrincipal(httptest.NewRequest(http.MethodPost, "/v1/identity/identify", strings.NewReader(`{"external_id":"customer-1","namespace":"email","value":"a@example.test"}`)), domain.Principal{ActorType: "user", UserID: "u-1"})
	rec := httptest.NewRecorder()
	server.identifyIdentity(rec, identify)
	if rec.Code != http.StatusAccepted || len(events) != 1 || events[0].Type != "identity.alias" {
		t.Fatalf("identify did not emit an alias event: status=%d events=%+v", rec.Code, events)
	}

	merge := withPrincipal(httptest.NewRequest(http.MethodPost, "/v1/identity/merge", strings.NewReader(`{"external_id":"winner","source_external_id":"loser"}`)), domain.Principal{ActorType: "api_key", UserID: ""})
	rec = httptest.NewRecorder()
	server.mergeIdentity(rec, merge)
	if rec.Code != http.StatusForbidden || len(events) != 1 {
		t.Fatalf("non-human merge bypassed gate: status=%d events=%d", rec.Code, len(events))
	}

	merge = withPrincipal(httptest.NewRequest(http.MethodPost, "/v1/identity/merge", strings.NewReader(`{"external_id":"winner","source_external_id":"loser"}`)), domain.Principal{ActorType: "user", UserID: "u-1"})
	rec = httptest.NewRecorder()
	server.mergeIdentity(rec, merge)
	if rec.Code != http.StatusAccepted || len(events) != 2 || events[1].Type != "identity.merge" {
		t.Fatalf("human merge did not emit an event: status=%d events=%+v", rec.Code, events)
	}

	unmerge := withPrincipal(httptest.NewRequest(http.MethodPost, "/v1/identity/unmerge", strings.NewReader(`{"source_profile_id":"loser-profile"}`)), domain.Principal{ActorType: "user", UserID: "u-1"})
	rec = httptest.NewRecorder()
	server.unmergeIdentity(rec, unmerge)
	if rec.Code != http.StatusAccepted || len(events) != 3 || events[2].Type != "identity.unmerge" {
		t.Fatalf("human unmerge did not emit an event: status=%d events=%+v", rec.Code, events)
	}
}
