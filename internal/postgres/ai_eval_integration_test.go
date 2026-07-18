package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestAIEvalStoreCRUD_11_12_1(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, "TRUNCATE tenants CASCADE"); err != nil {
		t.Fatal(err)
	}

	if err := store.EnsureDevelopmentTenant(ctx, "eval-store-test"); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, "eval-store-test")
	if err != nil {
		t.Fatal(err)
	}

	dataset, err := store.CreateEvalDataset(ctx, p, domain.EvalDataset{TaskType: "content_draft", Name: "golden-content"})
	if err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	datasets, err := store.ListEvalDatasets(ctx, p)
	if err != nil || len(datasets) != 1 {
		t.Fatalf("list datasets: count=%d err=%v", len(datasets), err)
	}
	dataset.Name = "updated-content"
	if _, err := store.UpdateEvalDataset(ctx, p, dataset); err != nil {
		t.Fatalf("update dataset: %v", err)
	}

	evalCase, err := store.CreateEvalCase(ctx, p, domain.EvalCase{
		DatasetID: dataset.ID, Input: json.RawMessage(`{"input":"draft"}`), Expectations: json.RawMessage(`{"must_pass_schema":true}`),
	})
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	if _, err := store.GetEvalCase(ctx, p, evalCase.ID); err != nil {
		t.Fatalf("get case: %v", err)
	}
	cases, err := store.ListEvalCases(ctx, p, dataset.ID)
	if err != nil || len(cases) != 1 {
		t.Fatalf("list cases: count=%d err=%v", len(cases), err)
	}

	prompt, err := store.GetPromptByName(ctx, p, "content-draft")
	if err != nil || prompt.CurrentVersionID == nil {
		t.Fatalf("get seeded prompt: %v", err)
	}
	run, err := store.CreateEvalRun(ctx, p, domain.EvalRun{
		PromptVersionID: *prompt.CurrentVersionID, DatasetID: dataset.ID, Passed: 1, Verdict: "passed",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if got, err := store.GetEvalRun(ctx, p, run.ID); err != nil || got.Verdict != "passed" {
		t.Fatalf("get run: got=%+v err=%v", got, err)
	}
	runs, err := store.ListEvalRuns(ctx, p, dataset.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("list runs: count=%d err=%v", len(runs), err)
	}

	if err := store.DeleteEvalCase(ctx, p, evalCase.ID); err != nil {
		t.Fatalf("delete case: %v", err)
	}
	if err := store.DeleteEvalDataset(ctx, p, dataset.ID); err != nil {
		t.Fatalf("delete dataset: %v", err)
	}
}
