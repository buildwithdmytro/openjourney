package postgres

import (
	"context"
	"encoding/json"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

// RecordAIActivity appends the immutable AI activity row and mirrors it to
// the audit stream in one transaction. There are deliberately no update or
// delete methods for ai_activity.
func (s *Store) RecordAIActivity(ctx context.Context, p domain.Principal, activity domain.AIActivity) (domain.AIActivity, error) {
	if activity.TenantID == "" {
		activity.TenantID = p.TenantID
	}
	if activity.WorkspaceID == "" {
		activity.WorkspaceID = p.WorkspaceID
	}
	if len(activity.RetrievalRefs) == 0 {
		activity.RetrievalRefs = json.RawMessage("[]")
	}
	if len(activity.ToolCalls) == 0 {
		activity.ToolCalls = json.RawMessage("[]")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AIActivity{}, err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `INSERT INTO ai_activity
		(tenant_id,workspace_id,actor_user_id,action,provider,model,prompt_version_id,
		retrieval_refs,tool_calls,classification,input_tokens,output_tokens,cost_cents,
		latency_ms,policy_decision,approver_user_id,output_ref)
		VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,NULLIF($7,'')::uuid,$8,$9,$10,$11,$12,$13,$14,
		$15,NULLIF($16,'')::uuid,$17)
		RETURNING id,created_at`,
		activity.TenantID, activity.WorkspaceID, activityPtrString(activity.ActorUserID), activity.Action,
		activity.Provider, activity.Model, activityPtrString(activity.PromptVersionID), activity.RetrievalRefs,
		activity.ToolCalls, activityPtrString(activity.Classification), activity.InputTokens, activity.OutputTokens,
		activity.CostCents, activity.LatencyMs, activity.PolicyDecision, activityPtrString(activity.ApproverUserID),
		activityPtrString(activity.OutputRef)).Scan(&activity.ID, &activity.CreatedAt)
	if err != nil {
		return domain.AIActivity{}, err
	}

	meta := map[string]any{
		"provider": activity.Provider, "model": activity.Model,
		"prompt_version_id": activity.PromptVersionID,
		"policy_decision":   activity.PolicyDecision, "cost_cents": activity.CostCents,
		"input_tokens": activity.InputTokens, "output_tokens": activity.OutputTokens,
	}
	if err := s.audit(ctx, tx, p, "ai.action", "ai_activity", activity.ID, meta); err != nil {
		return domain.AIActivity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AIActivity{}, err
	}
	return activity, nil
}

func activityPtrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
