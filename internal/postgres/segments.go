package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateSegment(ctx context.Context, p domain.Principal, seg domain.Segment) (domain.Segment, error) {
	if seg.Name == "" {
		return domain.Segment{}, errors.New("name is required")
	}
	if seg.Status == "" {
		seg.Status = "draft"
	}
	if seg.Type == "" {
		seg.Type = "dynamic"
	}
	if len(seg.DSL) == 0 {
		seg.DSL = json.RawMessage("{}")
	}
	var out domain.Segment
	err := s.pool.QueryRow(ctx, `INSERT INTO segments (tenant_id, workspace_id, name, description, type, status, dsl, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 1)
		RETURNING id, tenant_id, workspace_id, name, COALESCE(description, ''), type, status, dsl, version, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, seg.Name, seg.Description, seg.Type, seg.Status, seg.DSL).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Description, &out.Type, &out.Status, &out.DSL, &out.Version, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.Segment{}, err
	}
	_ = s.audit(ctx, p, "segment.create", "segment", out.ID, map[string]any{"name": out.Name})
	return out, nil
}

func (s *Store) GetSegment(ctx context.Context, p domain.Principal, id string) (domain.Segment, error) {
	var out domain.Segment
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, name, COALESCE(description, ''), type, status, dsl, version, created_at, updated_at
		FROM segments WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, id).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Description, &out.Type, &out.Status, &out.DSL, &out.Version, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Segment{}, ErrNotFound
	}
	return out, err
}

func (s *Store) UpdateSegment(ctx context.Context, p domain.Principal, seg domain.Segment) (domain.Segment, error) {
	existing, err := s.GetSegment(ctx, p, seg.ID)
	if err != nil {
		return domain.Segment{}, err
	}

	var oldDSLVal, newDSLVal any
	_ = json.Unmarshal(existing.DSL, &oldDSLVal)
	_ = json.Unmarshal(seg.DSL, &newDSLVal)

	version := existing.Version
	if !reflect.DeepEqual(oldDSLVal, newDSLVal) {
		version++
	}

	var out domain.Segment
	err = s.pool.QueryRow(ctx, `UPDATE segments
		SET name=$1, description=$2, type=$3, status=$4, dsl=$5, version=$6, updated_at=now()
		WHERE tenant_id=$7 AND workspace_id=$8 AND id=$9
		RETURNING id, tenant_id, workspace_id, name, COALESCE(description, ''), type, status, dsl, version, created_at, updated_at`,
		seg.Name, seg.Description, seg.Type, seg.Status, seg.DSL, version, p.TenantID, p.WorkspaceID, seg.ID).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Description, &out.Type, &out.Status, &out.DSL, &out.Version, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Segment{}, ErrNotFound
	}
	if err != nil {
		return domain.Segment{}, err
	}
	_ = s.audit(ctx, p, "segment.update", "segment", out.ID, map[string]any{"name": out.Name})
	return out, nil
}

func (s *Store) ListSegments(ctx context.Context, p domain.Principal) ([]domain.Segment, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, workspace_id, name, COALESCE(description, ''), type, status, dsl, version, created_at, updated_at
		FROM segments WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY name`,
		p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Segment
	for rows.Next() {
		var seg domain.Segment
		err := rows.Scan(&seg.ID, &seg.TenantID, &seg.WorkspaceID, &seg.Name, &seg.Description, &seg.Type, &seg.Status, &seg.DSL, &seg.Version, &seg.CreatedAt, &seg.UpdatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, seg)
	}
	return out, rows.Err()
}

func (s *Store) SetSegmentMembers(ctx context.Context, p domain.Principal, segmentID string, members []domain.SegmentMember) error {
	_, err := s.GetSegment(ctx, p, segmentID)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `DELETE FROM segment_members WHERE tenant_id=$1 AND segment_id=$2`, p.TenantID, segmentID)
	if err != nil {
		return err
	}

	for _, member := range members {
		var exists bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM profiles WHERE tenant_id=$1 AND id=$2)`, p.TenantID, member.ProfileID).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("profile %s does not exist", member.ProfileID)
		}

		if member.Membership == "" {
			member.Membership = "include"
		}

		_, err = tx.Exec(ctx, `INSERT INTO segment_members (segment_id, profile_id, tenant_id, membership)
			VALUES ($1, $2, $3, $4)`,
			segmentID, member.ProfileID, p.TenantID, member.Membership)
		if err != nil {
			return err
		}
	}

	err = tx.Commit(ctx)
	if err != nil {
		return err
	}

	_ = s.audit(ctx, p, "segment.set_members", "segment", segmentID, map[string]any{"count": len(members)})
	return nil
}
