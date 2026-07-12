package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type experimentHTTPStore struct {
	fakeStore
	created domain.Experiment
}

func (s *experimentHTTPStore) CreateExperiment(_ context.Context, _ domain.Principal, input domain.Experiment) (domain.Experiment, error) {
	input.ID = "experiment-2"
	s.created = input
	return input, nil
}

func (s *experimentHTTPStore) ListExperiments(context.Context, domain.Principal) ([]domain.Experiment, error) {
	return []domain.Experiment{{ID: "experiment-1", Name: "Subject lines", SubjectType: "campaign", Status: "draft", Method: "frequentist", Seed: "seed-1"}}, nil
}

func TestExperimentCreateAndList(t *testing.T) {
	store := &experimentHTTPStore{}
	store.scopes = []string{"experiments:read", "experiments:write"}
	server := New(store, 75)

	create := httptest.NewRequest(http.MethodPost, "/v1/experiments", strings.NewReader(`{"name":"CTA test","subject_type":"campaign","seed":"fixed-seed","variants":[{"label":"control","weight":50,"is_control":true},{"label":"b","weight":50}]}`))
	create.Header.Set("Authorization", "Bearer test-key")
	created := httptest.NewRecorder()
	server.ServeHTTP(created, create)
	if created.Code != http.StatusCreated || store.created.Name != "CTA test" || len(store.created.Variants) != 2 {
		t.Fatalf("create status=%d stored=%+v body=%s", created.Code, store.created, created.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/v1/experiments", nil)
	list.Header.Set("Authorization", "Bearer test-key")
	listed := httptest.NewRecorder()
	server.ServeHTTP(listed, list)
	var got []domain.Experiment
	if err := json.Unmarshal(listed.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if listed.Code != http.StatusOK || len(got) != 1 || got[0].ID != "experiment-1" {
		t.Fatalf("list status=%d experiments=%+v", listed.Code, got)
	}
}
