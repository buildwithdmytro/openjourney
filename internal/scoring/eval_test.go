package scoring

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type mockScoringStore struct {
	ports.Store
	version    domain.ScoringModelVersion
	model      domain.ScoringModel
	cases      []domain.EvalCase
	evalStatus string
}

func (m *mockScoringStore) GetScoringModelVersion(ctx context.Context, p domain.Principal, id string) (domain.ScoringModelVersion, error) {
	return m.version, nil
}

func (m *mockScoringStore) GetScoringModel(ctx context.Context, p domain.Principal, id string) (domain.ScoringModel, error) {
	return m.model, nil
}

func (m *mockScoringStore) ListEvalCases(ctx context.Context, p domain.Principal, datasetID string) ([]domain.EvalCase, error) {
	return m.cases, nil
}

func (m *mockScoringStore) SetScoringModelVersionEvalStatus(ctx context.Context, p domain.Principal, id string, evalStatus string) error {
	m.evalStatus = evalStatus
	return nil
}

func (m *mockScoringStore) GetScoringModelVersionByNumber(ctx context.Context, p domain.Principal, modelID string, version int) (domain.ScoringModelVersion, error) {
	return m.version, nil
}

func TestEvaluateExpressionModel_AllPassed(t *testing.T) {
	store := &mockScoringStore{
		model: domain.ScoringModel{
			ID:   "model-1",
			Kind: "expression",
		},
		version: domain.ScoringModelVersion{
			ID:             "version-1",
			ScoringModelID: "model-1",
			Definition:     json.RawMessage(`{"expr": "profile.age > 18"}`),
			OutputMin:      0.0,
			OutputMax:      1.0,
		},
		cases: []domain.EvalCase{
			{
				ID:           "case-1",
				Input:        json.RawMessage(`{"profile": {"age": 20}}`),
				Expectations: json.RawMessage(`{"expected_score": 1.0}`),
			},
			{
				ID:           "case-2",
				Input:        json.RawMessage(`{"profile": {"age": 16}}`),
				Expectations: json.RawMessage(`{"expected_value": 0.0}`),
			},
		},
	}

	p := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}
	res, caseResults, err := EvaluateExpressionModel(context.Background(), store, p, "version-1", "dataset-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Verdict != "passed" {
		t.Errorf("expected verdict passed, got %s", res.Verdict)
	}
	if res.Passed != 2 || res.Failed != 0 {
		t.Errorf("expected 2 passed 0 failed, got passed=%d failed=%d", res.Passed, res.Failed)
	}
	if store.evalStatus != "passed" {
		t.Errorf("expected store eval status passed, got %s", store.evalStatus)
	}

	if len(caseResults) != 2 {
		t.Fatalf("expected 2 case results, got %d", len(caseResults))
	}
	for _, cr := range caseResults {
		if !cr.Passed {
			t.Errorf("case %s failed with error: %s", cr.CaseID, cr.Error)
		}
	}
}

func TestEvaluateExpressionModel_SomeFailed(t *testing.T) {
	store := &mockScoringStore{
		model: domain.ScoringModel{
			ID:   "model-1",
			Kind: "expression",
		},
		version: domain.ScoringModelVersion{
			ID:             "version-1",
			ScoringModelID: "model-1",
			Definition:     json.RawMessage(`{"expr": "profile.income * 0.1"}`),
			OutputMin:      0.0,
			OutputMax:      100.0,
		},
		cases: []domain.EvalCase{
			{
				ID:           "case-1",
				Input:        json.RawMessage(`{"profile": {"income": 500}}`), // 50.0
				Expectations: json.RawMessage(`{"expected_score": 50.0}`),
			},
			{
				ID:           "case-2",
				Input:        json.RawMessage(`{"profile": {"income": 2000}}`), // 200.0 clamped to 100.0
				Expectations: json.RawMessage(`{"expected_score": 200.0}`),     // should fail because it gets clamped to 100.0
			},
		},
	}

	p := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}
	res, caseResults, err := EvaluateExpressionModel(context.Background(), store, p, "version-1", "dataset-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Verdict != "failed" {
		t.Errorf("expected verdict failed, got %s", res.Verdict)
	}
	if res.Passed != 1 || res.Failed != 1 {
		t.Errorf("expected 1 passed 1 failed, got passed=%d failed=%d", res.Passed, res.Failed)
	}
	if store.evalStatus != "failed" {
		t.Errorf("expected store eval status failed, got %s", store.evalStatus)
	}

	if len(caseResults) != 2 {
		t.Fatalf("expected 2 case results, got %d", len(caseResults))
	}
	if !caseResults[0].Passed {
		t.Errorf("case-1 should pass")
	}
	if caseResults[1].Passed {
		t.Errorf("case-2 should fail")
	}
	if !strings.Contains(caseResults[1].Error, "expected score 200, got 100") {
		t.Errorf("unexpected case-2 error: %s", caseResults[1].Error)
	}
}

func TestEvaluateExpressionModel_InvalidKind(t *testing.T) {
	store := &mockScoringStore{
		model: domain.ScoringModel{
			ID:   "model-1",
			Kind: "llm", // not expression
		},
		version: domain.ScoringModelVersion{
			ID:             "version-1",
			ScoringModelID: "model-1",
		},
	}

	p := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}
	_, _, err := EvaluateExpressionModel(context.Background(), store, p, "version-1", "dataset-1")
	if err == nil {
		t.Fatal("expected error evaluating non-expression model kind")
	}
	if !strings.Contains(err.Error(), "only supports expression scoring models") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGetUsableScoringModelVersion_EvalGate(t *testing.T) {
	// Verify that GetUsableScoringModelVersion enforces passed eval_status
	store := &mockScoringStore{
		version: domain.ScoringModelVersion{
			ID:         "version-1",
			Status:     "active",
			EvalStatus: "pending",
		},
	}

	p := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}
	_, err := GetUsableScoringModelVersion(context.Background(), store, p, "model-1", 1)
	if err == nil {
		t.Fatal("expected error getting usable version with pending eval")
	}
	if !strings.Contains(err.Error(), "is not usable") {
		t.Errorf("unexpected error: %v", err)
	}

	store.version.EvalStatus = "failed"
	_, err = GetUsableScoringModelVersion(context.Background(), store, p, "model-1", 1)
	if err == nil {
		t.Fatal("expected error getting usable version with failed eval")
	}

	store.version.EvalStatus = "passed"
	usable, err := GetUsableScoringModelVersion(context.Background(), store, p, "model-1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usable.ID != "version-1" {
		t.Errorf("expected version-1, got %s", usable.ID)
	}
}
