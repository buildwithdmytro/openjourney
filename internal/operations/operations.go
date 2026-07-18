package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/scoring"
	"github.com/buildwithdmytro/openjourney/internal/telemetry"
)

type Store interface {
	ClaimOperationJob(context.Context) (domain.OperationJob, bool, error)
	CompleteOperationJob(context.Context, string) error
	FailOperationJob(context.Context, string, error) error
	ExportPrivacyData(context.Context, string) (domain.PrivacyData, error)
	CompletePrivacyExport(context.Context, string, string) error
	DeletePrivacyData(context.Context, string) ([]string, error)
	EnforceRetention(context.Context, string) (domain.RetentionReport, error)
}

type AIGenerationStore interface {
	GetAIGenerationJob(context.Context, string) (domain.AIGenerationJob, error)
	GetPromptVersion(context.Context, domain.Principal, string) (domain.PromptVersion, error)
	MarkAIGenerationProcessing(context.Context, string) error
	CompleteAIGeneration(context.Context, string, string) error
}

type AIGateway interface {
	Generate(context.Context, domain.Principal, ai.GenerateRequest) (*ai.GenerateResponse, error)
}

func Drain(ctx context.Context, store Store, blobs ports.BlobStore, maxItems int, watch bool) (int, error) {
	return DrainWithGateway(ctx, store, blobs, nil, maxItems, watch)
}

func DrainWithGateway(ctx context.Context, store Store, blobs ports.BlobStore, gateway AIGateway, maxItems int, watch bool) (int, error) {
	processed := 0
	for processed < maxItems {
		job, found, err := store.ClaimOperationJob(ctx)
		if err != nil {
			return processed, err
		}
		if !found {
			if !watch {
				return processed, nil
			}
			select {
			case <-ctx.Done():
				return processed, nil
			case <-time.After(500 * time.Millisecond):
				continue
			}
		}
		if err := execute(ctx, store, blobs, gateway, job.Type, job.Payload); err != nil {
			if failErr := store.FailOperationJob(ctx, job.ID, err); failErr != nil {
				return processed, failErr
			}
			continue
		}
		if err := store.CompleteOperationJob(ctx, job.ID); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func execute(ctx context.Context, store Store, blobs ports.BlobStore, gateway AIGateway, jobType string, payload json.RawMessage) error {
	var input struct {
		RequestID string          `json:"request_id"`
		TenantID  string          `json:"tenant_id"`
		TaskType  string          `json:"task_type"`
		Input     json.RawMessage `json:"input"`
		Scopes    []string        `json:"scopes"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		return errors.New("operation payload must be a JSON object")
	}
	switch jobType {
	case "privacy.export":
		if input.RequestID == "" {
			return errors.New("privacy.export payload requires request_id")
		}
		data, err := store.ExportPrivacyData(ctx, input.RequestID)
		if err != nil {
			return err
		}
		content, err := json.Marshal(data)
		if err != nil {
			return err
		}
		key := fmt.Sprintf("privacy/%s/%s/export.json", data.TenantID, input.RequestID)
		if err := blobs.Put(ctx, key, content, "application/json"); err != nil {
			return err
		}
		return store.CompletePrivacyExport(ctx, input.RequestID, key)
	case "privacy.delete":
		if input.RequestID == "" {
			return errors.New("privacy.delete payload requires request_id")
		}
		objectKeys, err := store.DeletePrivacyData(ctx, input.RequestID)
		if err != nil {
			return err
		}
		for _, key := range objectKeys {
			if err := blobs.Delete(ctx, key); err != nil {
				return err
			}
		}
		return nil
	case "retention.enforce":
		tenantID := input.TenantID
		if tenantID == "" {
			return errors.New("retention.enforce payload requires tenant_id")
		}
		_, err := store.EnforceRetention(ctx, tenantID)
		return err
	case "ai.generate":
		return executeAIGeneration(ctx, store, blobs, gateway, input.RequestID, input.TaskType, input.Input, input.Scopes)
	case "scores.compute":
		return executeScoring(ctx, store, gateway, input.RequestID, input.Scopes)
	default:
		return fmt.Errorf("unsupported operation type %q", jobType)
	}
}

func executeAIGeneration(ctx context.Context, store Store, blobs ports.BlobStore, gateway AIGateway, requestID, taskType string, rawInput json.RawMessage, scopes []string) error {
	if requestID == "" {
		return errors.New("ai.generate payload requires request_id")
	}
	if gateway == nil {
		return errors.New("ai.generate requires an AI gateway")
	}
	jobStore, ok := store.(AIGenerationStore)
	if !ok {
		return errors.New("operation store does not support AI generation")
	}
	job, err := jobStore.GetAIGenerationJob(ctx, requestID)
	if err != nil {
		return err
	}
	if taskType == "" {
		return errors.New("ai.generate payload requires task_type")
	}
	var input struct {
		Prompt          string          `json:"prompt"`
		SystemPrompt    string          `json:"system_prompt"`
		PromptVersionID string          `json:"prompt_version_id"`
		OutputSchema    json.RawMessage `json:"output_schema"`
		Model           string          `json:"model"`
		Temperature     float64         `json:"temperature"`
		MaxTokens       int             `json:"max_tokens"`
	}
	if len(rawInput) == 0 || !json.Valid(rawInput) {
		return errors.New("ai.generate input must be valid JSON")
	}
	if err := json.Unmarshal(rawInput, &input); err != nil {
		return err
	}
	if input.PromptVersionID == "" {
		return errors.New("ai.generate input requires prompt_version_id")
	}
	if input.Prompt == "" {
		return errors.New("ai.generate input requires prompt")
	}
	principal := domain.Principal{
		TenantID: job.TenantID, WorkspaceID: job.WorkspaceID, UserID: job.RequestedBy,
		ActorType: "ai_agent", Scopes: append([]string(nil), scopes...),
	}
	promptVersion, err := jobStore.GetPromptVersion(ctx, principal, input.PromptVersionID)
	if err != nil {
		return err
	}
	if promptVersion.Status != "active" || promptVersion.EvalStatus != "passed" {
		return fmt.Errorf("prompt version %q is not usable", input.PromptVersionID)
	}
	if err := jobStore.MarkAIGenerationProcessing(ctx, requestID); err != nil {
		return err
	}
	response, err := gateway.Generate(ctx, principal, ai.GenerateRequest{
		Prompt: input.Prompt, SystemPrompt: input.SystemPrompt, PromptVersionID: input.PromptVersionID,
		OutputSchema: promptVersion.OutputSchema, Model: promptVersion.Model, Temperature: input.Temperature, MaxTokens: input.MaxTokens,
		Action: "ai." + taskType,
	})
	if err != nil {
		return err
	}
	content, err := json.Marshal(struct {
		TaskType string   `json:"task_type"`
		Content  string   `json:"content"`
		Usage    ai.Usage `json:"usage"`
	}{taskType, response.Content, response.Usage})
	if err != nil {
		return err
	}
	key := fmt.Sprintf("ai/generations/%s/%s.json", job.TenantID, requestID)
	if err := blobs.Put(ctx, key, content, "application/json"); err != nil {
		return err
	}
	return jobStore.CompleteAIGeneration(ctx, requestID, key)
}

type ScoringStore interface {
	ports.Store
	GetScoringJob(context.Context, string) (domain.ScoringJob, error)
	GetScoringModel(context.Context, domain.Principal, string) (domain.ScoringModel, error)
	GetScoringModelVersion(context.Context, domain.Principal, string) (domain.ScoringModelVersion, error)
	GetScoringModelVersionByNumber(context.Context, domain.Principal, string, int) (domain.ScoringModelVersion, error)
	MarkScoringProcessing(context.Context, string) error
	CompleteScoring(context.Context, string) error
	FailScoring(context.Context, string, string) error
	ResolveSegment(context.Context, domain.Principal, string) ([]string, error)
	GetProfileByIDSystem(context.Context, string, string, string) (domain.Profile, error)
	GetFirstAppID(context.Context, string, string) (string, error)
	UpsertProfileScores(context.Context, []domain.ProfileScore) error
	GetEventCount(ctx context.Context, tenantID, workspaceID, externalID, anonymousID, eventType string, days int) (int64, error)
}

func executeScoring(ctx context.Context, store Store, gateway AIGateway, requestID string, scopes []string) error {
	if requestID == "" {
		return errors.New("scores.compute payload requires request_id")
	}
	scoringStore, ok := store.(ScoringStore)
	if !ok {
		return errors.New("operation store does not support scoring compute")
	}
	job, err := scoringStore.GetScoringJob(ctx, requestID)
	if err != nil {
		return err
	}

	principal := domain.Principal{
		TenantID: job.TenantID, WorkspaceID: job.WorkspaceID, UserID: job.RequestedBy,
		ActorType: "ai_agent", Scopes: append([]string(nil), scopes...),
	}

	model, err := scoringStore.GetScoringModel(ctx, principal, job.ScoringModelID)
	if err != nil {
		return err
	}
	if model.CurrentVersionID == nil || *model.CurrentVersionID == "" {
		return fmt.Errorf("scoring model %q has no active version", job.ScoringModelID)
	}

	sv, err := scoringStore.GetScoringModelVersion(ctx, principal, *model.CurrentVersionID)
	if err != nil {
		return err
	}
	if sv.Status != "active" || sv.EvalStatus != "passed" {
		return fmt.Errorf("scoring model version %q is not active or passed", sv.ID)
	}

	if err := scoringStore.MarkScoringProcessing(ctx, requestID); err != nil {
		return err
	}

	profileIDs, err := scoringStore.ResolveSegment(ctx, principal, job.SegmentID)
	if err != nil {
		return err
	}

	appID, err := scoringStore.GetFirstAppID(ctx, job.TenantID, job.WorkspaceID)
	if err != nil {
		return err
	}

	var scores []domain.ProfileScore
	for _, profileID := range profileIDs {
		profile, err := scoringStore.GetProfileByIDSystem(ctx, job.TenantID, job.WorkspaceID, profileID)
		if err != nil {
			return err
		}

		var profileAttrs map[string]any
		if len(profile.Attributes) > 0 {
			_ = json.Unmarshal(profile.Attributes, &profileAttrs)
		}
		if profileAttrs == nil {
			profileAttrs = make(map[string]any)
		}

		var scoreVal float64
		if model.Kind == "expression" {
			var def struct {
				Expr string `json:"expr"`
			}
			if err := json.Unmarshal(sv.Definition, &def); err != nil {
				return err
			}
			env, err := scoring.BuildExpressionEnv(ctx, scoringStore, job.TenantID, job.WorkspaceID, profile, def.Expr)
			if err != nil {
				return err
			}
			scoreVal, err = scoring.Evaluate(def.Expr, env, sv.OutputMin, sv.OutputMax)
			if err != nil {
				return err
			}
		} else if model.Kind == "llm" {
			if gateway == nil {
				return errors.New("scores.compute for llm kind requires an AI gateway")
			}
			aiGateway, ok := gateway.(*ai.Gateway)
			if !ok {
				return errors.New("ai gateway is not of type *ai.Gateway")
			}
			env := map[string]any{
				"profile": profileAttrs,
			}
			scoreVal, err = scoring.EvaluateLLM(ctx, scoringStore, aiGateway, principal, sv, env)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("unsupported scoring model kind: %s", model.Kind)
		}

		scores = append(scores, domain.ProfileScore{
			TenantID:       job.TenantID,
			WorkspaceID:    job.WorkspaceID,
			AppID:          appID,
			ProfileID:      profileID,
			ScoringModelID: job.ScoringModelID,
			ScoreName:      sv.ScoreName,
			Value:          scoreVal,
			ModelVersion:   sv.Version,
		})
	}

	if len(scores) > 0 {
		if err := scoringStore.UpsertProfileScores(ctx, scores); err != nil {
			return err
		}
		telemetry.ScoresComputed.Add(ctx, int64(len(scores)))
	}

	return scoringStore.CompleteScoring(ctx, requestID)
}
