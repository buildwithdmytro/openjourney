package eval

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type memoryStore struct {
	version domain.PromptVersion
	cases   []domain.EvalCase
	runs    []domain.EvalRun
	status  string
}

func (s *memoryStore) GetPromptVersion(context.Context, domain.Principal, string) (domain.PromptVersion, error) {
	return s.version, nil
}
func (s *memoryStore) ListEvalCases(context.Context, domain.Principal, string) ([]domain.EvalCase, error) {
	return s.cases, nil
}
func (s *memoryStore) CreateEvalRun(_ context.Context, _ domain.Principal, run domain.EvalRun) (domain.EvalRun, error) {
	run.ID = "run-1"
	s.runs = append(s.runs, run)
	return run, nil
}
func (s *memoryStore) SetPromptVersionEvalStatus(_ context.Context, _ domain.Principal, _ string, status string) error {
	s.status = status
	return nil
}

func testVersion() domain.PromptVersion {
	return domain.PromptVersion{
		ID: "version-1", Provider: "fake", Model: "fake-content-draft-v1", Template: "Draft content.",
		InputSchema:  json.RawMessage(`{"type":"object","required":["brief"],"properties":{"brief":{"type":"string"}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["subject"],"properties":{"subject":{"type":"string"}}}`),
	}
}

func TestRunnerFakeProviderPassesAndPersistsEvalGate(t *testing.T) {
	store := &memoryStore{version: testVersion(), cases: []domain.EvalCase{{ID: "case-1", Input: json.RawMessage(`{"brief":"welcome"}`), Expectations: json.RawMessage(`{"must_pass_schema":true,"required_fields":["subject"],"max_cost_cents":1}`)}}}
	runner := NewRunner(store, ai.DeterministicFakeProvider{})
	result, cases, err := runner.Run(context.Background(), domain.Principal{TenantID: "tenant", WorkspaceID: "workspace"}, "version-1", "dataset-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Verdict != "passed" || result.Passed != 1 || len(cases) != 1 || !cases[0].Passed {
		t.Fatalf("unexpected successful evaluation: result=%+v cases=%+v", result, cases)
	}
	if store.status != "passed" || len(store.runs) != 1 {
		t.Fatalf("eval gate was not persisted: status=%q runs=%d", store.status, len(store.runs))
	}
}

func TestRunnerFakeProviderFailsForbiddenAndValidatorChecks(t *testing.T) {
	store := &memoryStore{version: testVersion(), cases: []domain.EvalCase{{ID: "case-1", Input: json.RawMessage(`{"brief":"welcome"}`), Expectations: json.RawMessage(`{"forbidden_fields":["subject"]}`)}}}
	runner := NewRunner(store, ai.DeterministicFakeProvider{})
	runner.SetValidator("version-1", func(output []byte) error {
		if !strings.Contains(string(output), "subject") {
			return nil
		}
		return nil
	})
	result, cases, err := runner.Run(context.Background(), domain.Principal{TenantID: "tenant", WorkspaceID: "workspace"}, "version-1", "dataset-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Verdict != "failed" || result.Failed != 1 || cases[0].Passed || store.status != "failed" {
		t.Fatalf("failed validator did not close eval gate: result=%+v cases=%+v status=%q", result, cases, store.status)
	}
	if !strings.Contains(cases[0].Error, "forbidden field") {
		t.Fatalf("unexpected failure: %q", cases[0].Error)
	}
}
