package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

type reportHTTPStore struct {
	fakeStore
	principals []domain.Principal
	IDs        []string
}

func (s *reportHTTPStore) CampaignReport(_ context.Context, p domain.Principal, id string) (domain.CampaignReport, error) {
	s.principals = append(s.principals, p)
	s.IDs = append(s.IDs, id)
	if id == "missing" {
		return domain.CampaignReport{}, postgres.ErrNotFound
	}
	return domain.CampaignReport{
		CampaignID: id,
		Funnel: domain.ReportFunnel{
			Targeted:  domain.ReportCount{Total: 12, Unique: 10},
			Sent:      domain.ReportCount{Total: 8, Unique: 7},
			Delivered: domain.ReportCount{Total: 7, Unique: 6},
			Opened:    domain.ReportCount{Total: 5, Unique: 4},
			Clicked:   domain.ReportCount{Total: 3, Unique: 2},
			Converted: domain.ReportCount{Total: 2, Unique: 2},
		},
		Deliverability: domain.ReportDeliverability{
			Bounced:       domain.ReportCount{Total: 1, Unique: 1},
			BounceRate:    0.125,
			ComplaintRate: 0,
		},
	}, nil
}

func (s *reportHTTPStore) JourneyReport(_ context.Context, p domain.Principal, id string) (domain.JourneyReport, error) {
	s.principals = append(s.principals, p)
	s.IDs = append(s.IDs, id)
	if id == "missing" {
		return domain.JourneyReport{}, postgres.ErrNotFound
	}
	return domain.JourneyReport{
		JourneyID: id,
		Funnel: domain.ReportFunnel{
			Targeted:  domain.ReportCount{Total: 9, Unique: 8},
			Sent:      domain.ReportCount{Total: 6, Unique: 5},
			Converted: domain.ReportCount{Total: 1, Unique: 1},
		},
		Deliverability: domain.ReportDeliverability{
			Complained:    domain.ReportCount{Total: 1, Unique: 1},
			ComplaintRate: 1.0 / 6.0,
		},
	}, nil
}

func TestReportEndpointsReturnJSONAndPassScopedPrincipal(t *testing.T) {
	store := &reportHTTPStore{}
	store.scopes = []string{"reports:read"}
	server := New(store, 75)

	tests := []struct {
		path  string
		check func(*testing.T, []byte)
	}{
		{
			path: "/v1/reports/campaigns/campaign-7",
			check: func(t *testing.T, body []byte) {
				var got domain.CampaignReport
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatalf("decode campaign report: %v", err)
				}
				if got.CampaignID != "campaign-7" || got.Funnel.Targeted.Total != 12 || got.Funnel.Converted.Unique != 2 || got.Deliverability.BounceRate != 0.125 {
					t.Fatalf("unexpected campaign JSON: %+v", got)
				}
			},
		},
		{
			path: "/v1/reports/journeys/journey-9",
			check: func(t *testing.T, body []byte) {
				var got domain.JourneyReport
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatalf("decode journey report: %v", err)
				}
				if got.JourneyID != "journey-9" || got.Funnel.Sent.Unique != 5 || got.Funnel.Converted.Total != 1 || got.Deliverability.Complained.Total != 1 {
					t.Fatalf("unexpected journey JSON: %+v", got)
				}
			},
		},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		req.Header.Set("Authorization", "Bearer test-key")
		res := httptest.NewRecorder()
		server.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", tt.path, res.Code, res.Body.String())
		}
		if contentType := res.Header().Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("GET %s content-type=%q", tt.path, contentType)
		}
		tt.check(t, res.Body.Bytes())
	}

	if len(store.principals) != 2 || len(store.IDs) != 2 {
		t.Fatalf("report calls principals=%d ids=%v", len(store.principals), store.IDs)
	}
	for _, principal := range store.principals {
		if principal.TenantID != "tenant" || principal.WorkspaceID != "workspace" {
			t.Fatalf("report received unscoped principal: %+v", principal)
		}
	}
	if store.IDs[0] != "campaign-7" || store.IDs[1] != "journey-9" {
		t.Fatalf("unexpected report IDs: %v", store.IDs)
	}
}

func TestReportEndpointsRequireReportsReadScope(t *testing.T) {
	store := &reportHTTPStore{}
	store.scopes = []string{"campaigns:read", "journeys:read"}
	server := New(store, 75)

	for _, path := range []string{"/v1/reports/campaigns/campaign-7", "/v1/reports/journeys/journey-9"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer test-key")
		res := httptest.NewRecorder()
		server.ServeHTTP(res, req)
		if res.Code != http.StatusForbidden {
			t.Fatalf("GET %s status=%d body=%s", path, res.Code, res.Body.String())
		}
	}
	if len(store.principals) != 0 {
		t.Fatalf("store called without reports:read: %+v", store.principals)
	}
}

func TestReportEndpointsReturnNotFound(t *testing.T) {
	store := &reportHTTPStore{}
	store.scopes = []string{"reports:read"}
	server := New(store, 75)

	for _, path := range []string{"/v1/reports/campaigns/missing", "/v1/reports/journeys/missing"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer test-key")
		res := httptest.NewRecorder()
		server.ServeHTTP(res, req)
		if res.Code != http.StatusNotFound {
			t.Fatalf("GET %s status=%d body=%s", path, res.Code, res.Body.String())
		}
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body.Error.Code != "not_found" {
			t.Fatalf("GET %s error=%v body=%s", path, err, res.Body.String())
		}
	}
}
