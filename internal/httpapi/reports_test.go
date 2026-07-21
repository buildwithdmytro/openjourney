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

func (s *reportHTTPStore) CampaignReport(_ context.Context, p domain.Principal, id string, _ domain.ReportQuery) (domain.CampaignReport, error) {
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

func (s *reportHTTPStore) JourneyReport(_ context.Context, p domain.Principal, id string, _ domain.ReportQuery) (domain.JourneyReport, error) {
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

func (s *reportHTTPStore) ExperimentReport(_ context.Context, p domain.Principal, id string, _ domain.ReportQuery) (domain.ExperimentReport, error) {
	s.principals = append(s.principals, p)
	s.IDs = append(s.IDs, id)
	if id == "missing" {
		return domain.ExperimentReport{}, postgres.ErrNotFound
	}
	return domain.ExperimentReport{
		ExperimentID: id,
		Variants: []domain.ExperimentVariantReport{
			{
				Label:       "control",
				IsControl:   true,
				Sent:        100,
				Conversions: 10,
				Rate:        0.1,
				Uplift:      0,
				ZScore:      0,
				PValue:      1,
				CILow:       0,
				CIHigh:      0,
				Guardrails:  []domain.ExperimentGuardrail{},
			},
			{
				Label:       "treatment",
				IsControl:   false,
				Sent:        100,
				Conversions: 20,
				Rate:        0.2,
				Uplift:      1.0,
				ZScore:      1.980295,
				PValue:      0.047670,
				CILow:       0.002002,
				CIHigh:      0.197998,
				Guardrails:  []domain.ExperimentGuardrail{},
			},
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
		{
			path: "/v1/reports/experiments/experiment-5",
			check: func(t *testing.T, body []byte) {
				var got domain.ExperimentReport
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatalf("decode experiment report: %v", err)
				}
				if got.ExperimentID != "experiment-5" || len(got.Variants) != 2 || got.Variants[1].Label != "treatment" || got.Variants[1].PValue != 0.047670 {
					t.Fatalf("unexpected experiment JSON: %+v", got)
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

	if len(store.principals) != 3 || len(store.IDs) != 3 {
		t.Fatalf("report calls principals=%d ids=%v", len(store.principals), store.IDs)
	}
	for _, principal := range store.principals {
		if principal.TenantID != "tenant" || principal.WorkspaceID != "workspace" {
			t.Fatalf("report received unscoped principal: %+v", principal)
		}
	}
	if store.IDs[0] != "campaign-7" || store.IDs[1] != "journey-9" || store.IDs[2] != "experiment-5" {
		t.Fatalf("unexpected report IDs: %v", store.IDs)
	}
}

func TestReportEndpointsRequireReportsReadScope(t *testing.T) {
	store := &reportHTTPStore{}
	store.scopes = []string{"campaigns:read", "journeys:read"}
	server := New(store, 75)

	for _, path := range []string{"/v1/reports/campaigns/campaign-7", "/v1/reports/journeys/journey-9", "/v1/reports/experiments/experiment-5"} {
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

	for _, path := range []string{"/v1/reports/campaigns/missing", "/v1/reports/journeys/missing", "/v1/reports/experiments/missing"} {
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

func TestReportQueryEmptyQueryBackwardCompatible(t *testing.T) {
	store := &reportHTTPStore{}
	store.scopes = []string{"reports:read"}
	server := New(store, 75)

	req := httptest.NewRequest(http.MethodGet, "/v1/reports/campaigns/campaign-1", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("empty query: status=%d body=%s", res.Code, res.Body.String())
	}

	var report domain.CampaignReport
	if err := json.Unmarshal(res.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.CampaignID != "campaign-1" {
		t.Fatalf("expected campaign-1, got %s", report.CampaignID)
	}
}

func TestReportQueryValidGranularityAndTimeRange(t *testing.T) {
	store := &reportHTTPStore{}
	store.scopes = []string{"reports:read"}
	server := New(store, 75)

	req := httptest.NewRequest(http.MethodGet, "/v1/reports/campaigns/campaign-1?granularity=day&start=2024-01-01T00:00:00Z&end=2024-01-31T23:59:59Z", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("valid query: status=%d body=%s", res.Code, res.Body.String())
	}

	var report domain.CampaignReport
	if err := json.Unmarshal(res.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
}

func TestReportQueryInvalidGranularityRejected(t *testing.T) {
	store := &reportHTTPStore{}
	store.scopes = []string{"reports:read"}
	server := New(store, 75)

	req := httptest.NewRequest(http.MethodGet, "/v1/reports/campaigns/campaign-1?granularity=invalid", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid granularity: status=%d, want 422, body=%s", res.Code, res.Body.String())
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body.Error.Code != "invalid_query" {
		t.Fatalf("invalid granularity error: body=%s", res.Body.String())
	}
}

func TestReportQueryInvalidDimensionRejected(t *testing.T) {
	store := &reportHTTPStore{}
	store.scopes = []string{"reports:read"}
	server := New(store, 75)

	req := httptest.NewRequest(http.MethodGet, "/v1/reports/campaigns/campaign-1?dimensions=invalid_dimension", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid dimension: status=%d, want 422, body=%s", res.Code, res.Body.String())
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body.Error.Code != "invalid_query" {
		t.Fatalf("invalid dimension error: body=%s", res.Body.String())
	}
}

func TestReportQueryValidDimensionAccepted(t *testing.T) {
	store := &reportHTTPStore{}
	store.scopes = []string{"reports:read"}
	server := New(store, 75)

	req := httptest.NewRequest(http.MethodGet, "/v1/reports/campaigns/campaign-1?dimensions=channel,variant,node,provider", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("valid dimensions: status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestReportQueryValidFiltersAccepted(t *testing.T) {
	store := &reportHTTPStore{}
	store.scopes = []string{"reports:read"}
	server := New(store, 75)

	req := httptest.NewRequest(http.MethodGet, "/v1/reports/campaigns/campaign-1?filter_channel=email&filter_variant=control", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("valid filters: status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestReportQueryInvalidFilterRejected(t *testing.T) {
	store := &reportHTTPStore{}
	store.scopes = []string{"reports:read"}
	server := New(store, 75)

	req := httptest.NewRequest(http.MethodGet, "/v1/reports/campaigns/campaign-1?filter_invalid=value", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid filter: status=%d, want 422, body=%s", res.Code, res.Body.String())
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body.Error.Code != "invalid_query" {
		t.Fatalf("invalid filter error: body=%s", res.Body.String())
	}
}
