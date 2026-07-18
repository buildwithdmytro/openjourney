package postgres

import (
	"context"
	"errors"
	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func (s *Store) CreateStageRule(ctx context.Context, p domain.Principal, rule domain.StageRule) (domain.StageRule, error) {
	if rule.Stage == "" || rule.SegmentID == "" {
		return domain.StageRule{}, errors.New("stage and segment_id are required")
	}
	var out domain.StageRule
	err := s.pool.QueryRow(ctx, `INSERT INTO stage_rules (tenant_id, workspace_id, stage, segment_id, priority, enabled) VALUES ($1,$2,$3,$4,$5,true) RETURNING id,tenant_id,workspace_id,stage,segment_id,priority,enabled,created_at`, p.TenantID, p.WorkspaceID, rule.Stage, rule.SegmentID, rule.Priority).Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Stage, &out.SegmentID, &out.Priority, &out.Enabled, &out.CreatedAt)
	if err == nil {
		_ = s.audit(ctx, p, "stage_rule.create", "stage_rule", out.ID, map[string]any{"stage": out.Stage})
	}
	return out, err
}
