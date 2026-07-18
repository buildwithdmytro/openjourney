package operations

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type generationStore struct {
	job         domain.AIGenerationJob
	version     domain.PromptVersion
	processing  bool
	completed   string
	jobComplete bool
}

func (s *generationStore) ClaimOperationJob(context.Context) (domain.OperationJob, bool, error) {
	return domain.OperationJob{ID: "job-1", Type: "ai.generate", Payload: json.RawMessage(`{"request_id":"generation-1","task_type":"content_draft","scopes":["ai:invoke"],"input":{"prompt":"draft it","prompt_version_id":"version-1"}}`)}, true, nil
}
func (s *generationStore) CompleteOperationJob(context.Context, string) error {
	s.jobComplete = true
	return nil
}
func (s *generationStore) FailOperationJob(context.Context, string, error) error { return nil }
func (s *generationStore) ExportPrivacyData(context.Context, string) (domain.PrivacyData, error) {
	return domain.PrivacyData{}, errors.New("unused")
}
func (s *generationStore) CompletePrivacyExport(context.Context, string, string) error {
	return errors.New("unused")
}
func (s *generationStore) DeletePrivacyData(context.Context, string) ([]string, error) {
	return nil, errors.New("unused")
}
func (s *generationStore) EnforceRetention(context.Context, string) (domain.RetentionReport, error) {
	return domain.RetentionReport{}, errors.New("unused")
}
func (s *generationStore) GetAIGenerationJob(context.Context, string) (domain.AIGenerationJob, error) {
	return s.job, nil
}
func (s *generationStore) GetPromptVersion(context.Context, domain.Principal, string) (domain.PromptVersion, error) {
	return s.version, nil
}
func (s *generationStore) MarkAIGenerationProcessing(context.Context, string) error {
	s.processing = true
	return nil
}
func (s *generationStore) CompleteAIGeneration(_ context.Context, _ string, ref string) error {
	s.completed = ref
	return nil
}

type generationBlobs struct{ objects map[string][]byte }

func (b *generationBlobs) Put(_ context.Context, key string, value []byte, _ string) error {
	b.objects[key] = value
	return nil
}
func (b *generationBlobs) Get(context.Context, string) ([]byte, error) {
	return nil, errors.New("unused")
}
func (b *generationBlobs) Delete(context.Context, string) error { return nil }

type generationGateway struct {
	principal domain.Principal
	request   ai.GenerateRequest
	err       error
}

func (g *generationGateway) Generate(_ context.Context, p domain.Principal, req ai.GenerateRequest) (*ai.GenerateResponse, error) {
	g.principal, g.request = p, req
	if g.err != nil {
		return nil, g.err
	}
	return &ai.GenerateResponse{Content: `{"subject":"hello"}`, Usage: ai.Usage{InputTokens: 2, OutputTokens: 3, CostCents: 1}}, nil
}

func TestAIGenerationWorkerCompletesPinnedGeneration(t *testing.T) {
	store := &generationStore{
		job:     domain.AIGenerationJob{ID: "generation-1", TenantID: "tenant-1", WorkspaceID: "workspace-1", RequestedBy: "user-1"},
		version: domain.PromptVersion{ID: "version-1", Status: "active", EvalStatus: "passed", Model: "pinned-model", OutputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	blobs := &generationBlobs{objects: map[string][]byte{}}
	gateway := &generationGateway{}
	count, err := DrainWithGateway(context.Background(), store, blobs, gateway, 1, false)
	if err != nil || count != 1 {
		t.Fatalf("drain count=%d err=%v", count, err)
	}
	if !store.processing || store.completed != "ai/generations/tenant-1/generation-1.json" || !store.jobComplete {
		t.Fatalf("generation was not completed: processing=%v ref=%q jobComplete=%v", store.processing, store.completed, store.jobComplete)
	}
	if gateway.principal.ActorType != "ai_agent" || gateway.principal.UserID != "user-1" || gateway.request.Model != "pinned-model" {
		t.Fatalf("worker did not use governed pinned principal/request: principal=%+v request=%+v", gateway.principal, gateway.request)
	}
	if _, ok := blobs.objects[store.completed]; !ok {
		t.Fatalf("result blob %q was not written", store.completed)
	}
}

func TestAIGenerationWorkerRejectsUnevaluatedPromptBeforeProvider(t *testing.T) {
	store := &generationStore{
		job:     domain.AIGenerationJob{ID: "generation-1", TenantID: "tenant-1", WorkspaceID: "workspace-1", RequestedBy: "user-1"},
		version: domain.PromptVersion{ID: "version-1", Status: "draft", EvalStatus: "pending", Model: "pinned-model"},
	}
	gateway := &generationGateway{}
	err := executeAIGeneration(context.Background(), store, &generationBlobs{objects: map[string][]byte{}}, gateway, "generation-1", "content_draft", json.RawMessage(`{"prompt":"draft","prompt_version_id":"version-1"}`), []string{"ai:invoke"})
	if err == nil || gateway.request.Prompt != "" || store.processing {
		t.Fatalf("unevaluated prompt was invoked: err=%v request=%+v processing=%v", err, gateway.request, store.processing)
	}
}
