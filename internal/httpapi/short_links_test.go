package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type shortLinkTestStore struct {
	ports.Store
	link   domain.ShortLink
	events []domain.Event
}

func (s *shortLinkTestStore) GetShortLinkBySlug(context.Context, string) (domain.ShortLink, error) {
	return s.link, nil
}

func (s *shortLinkTestStore) GetFirstAppID(context.Context, string, string) (string, error) {
	return "app-1", nil
}

func (s *shortLinkTestStore) AcceptEvents(_ context.Context, _ domain.Principal, events []domain.Event) ([]string, error) {
	s.events = append(s.events, events...)
	return []string{"event-1"}, nil
}

func TestRedirectShortLinkAppendsUTMAndRecordsClick(t *testing.T) {
	store := &shortLinkTestStore{link: domain.ShortLink{
		ID: "link-1", TenantID: "tenant-1", WorkspaceID: "workspace-1", Slug: "launch",
		DestinationURL: "https://example.com/landing?existing=1",
		UTM:            json.RawMessage(`{"source":"newsletter","campaign":"spring"}`),
	}}
	h := New(store, 10)
	req := httptest.NewRequest(http.MethodGet, "/s/launch", nil)
	req.Header.Set("User-Agent", "test-agent")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusFound)
	}
	if got, want := res.Header().Get("Location"), "https://example.com/landing?existing=1&utm_campaign=spring&utm_source=newsletter"; got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}
	if len(store.events) != 1 || store.events[0].Type != "link.clicked" {
		t.Fatalf("events = %#v, want one link.clicked event", store.events)
	}
	var payload map[string]any
	if err := json.Unmarshal(store.events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["short_link_id"] != "link-1" || payload["utm"] == nil {
		t.Fatalf("payload = %#v, want short link id and UTM", payload)
	}
}
