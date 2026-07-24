package postgres

import (
	"context"
	"errors"
	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func (s *Store) CreateStageRule(ctx context.Context, p domain.Principal, rule domain.StageRule) (domain.StageRule, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.StageRule{}, err
	}
	defer tx.Rollback(ctx)

	if rule.Stage == "" || rule.SegmentID == "" {
		return domain.StageRule{}, errors.New("stage and segment_id are required")
	}
	var out domain.StageRule
	err = tx.QueryRow(ctx, `INSERT INTO stage_rules (tenant_id, workspace_id, stage, segment_id, priority, enabled) VALUES ($1,$2,$3,$4,$5,true) RETURNING id,tenant_id,workspace_id,stage,segment_id,priority,enabled,created_at`, p.TenantID, p.WorkspaceID, rule.Stage, rule.SegmentID, rule.Priority).Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Stage, &out.SegmentID, &out.Priority, &out.Enabled, &out.CreatedAt)
	if err == nil {
		if err := s.audit(ctx, tx, p, "stage_rule.create", "stage_rule", out.ID, map[string]any{"stage": out.Stage}); err != nil {
		return domain.StageRule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.StageRule{}, err
	}
	}
	return out, err
}
