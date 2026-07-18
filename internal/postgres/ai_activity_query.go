package postgres

import (
	"context"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

// ListAIActivity returns only activity belonging to the caller's tenant and
// workspace. The limit is bounded to keep this audit endpoint predictable.
func (s *Store) ListAIActivity(ctx context.Context, p domain.Principal, limit int) ([]domain.AIActivity, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT id,tenant_id,workspace_id,actor_user_id,action,provider,model,prompt_version_id,
		retrieval_refs,tool_calls,classification,input_tokens,output_tokens,cost_cents,latency_ms,
		policy_decision,approver_user_id,output_ref,created_at
		FROM ai_activity
		WHERE tenant_id=$1 AND workspace_id=$2
		ORDER BY created_at DESC,id DESC LIMIT $3`, p.TenantID, p.WorkspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	activities := make([]domain.AIActivity, 0)
	for rows.Next() {
		var activity domain.AIActivity
		if err := rows.Scan(&activity.ID, &activity.TenantID, &activity.WorkspaceID, &activity.ActorUserID,
			&activity.Action, &activity.Provider, &activity.Model, &activity.PromptVersionID, &activity.RetrievalRefs, &activity.ToolCalls,
			&activity.Classification, &activity.InputTokens, &activity.OutputTokens, &activity.CostCents,
			&activity.LatencyMs, &activity.PolicyDecision, &activity.ApproverUserID, &activity.OutputRef,
			&activity.CreatedAt); err != nil {
			return nil, err
		}
		activities = append(activities, activity)
	}
	return activities, rows.Err()
}
