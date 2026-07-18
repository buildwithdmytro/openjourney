package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/buildwithdmytro/openjourney/internal/audience"
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

func (s *Store) SetClickHouse(conn clickhouse.Conn) {
	s.chConn = conn
}

func (s *Store) resolveSegmentIDs(ctx context.Context, p domain.Principal, seg domain.Segment) (map[string]bool, map[string]int, error) {
	var node audience.Node
	var err error
	isDSL := len(seg.DSL) > 0 && string(seg.DSL) != "{}" && string(seg.DSL) != "null"
	if isDSL {
		node, err = audience.Parse(seg.DSL)
		if err != nil {
			return nil, nil, err
		}
	}

	rows, err := s.pool.Query(ctx, `SELECT id, COALESCE(external_id, '') FROM profiles
		WHERE tenant_id = $1 AND workspace_id = $2`, p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	profileIDToExternalID := make(map[string]string)
	externalIDToProfileID := make(map[string]string)
	hashToProfileID := make(map[string]string)
	allProfileIDs := make(map[string]bool)

	for rows.Next() {
		var id, extID string
		if err := rows.Scan(&id, &extID); err != nil {
			return nil, nil, err
		}
		allProfileIDs[id] = true
		if extID != "" {
			profileIDToExternalID[id] = extID
			externalIDToProfileID[extID] = id
			h := sha256.Sum256([]byte(extID))
			hashToProfileID[fmt.Sprintf("%x", h)] = id
		}
	}

	perLegCounts := make(map[string]int)

	var eval func(n audience.Node) (map[string]bool, error)
	eval = func(n audience.Node) (map[string]bool, error) {
		switch nodeType := n.(type) {
		case *audience.And:
			res := make(map[string]bool)
			first := true
			for _, cond := range nodeType.Conditions {
				set, err := eval(cond)
				if err != nil {
					return nil, err
				}
				if first {
					for k := range set {
						res[k] = true
					}
					first = false
				} else {
					for k := range res {
						if !set[k] {
							delete(res, k)
						}
					}
				}
			}
			return res, nil

		case *audience.Or:
			res := make(map[string]bool)
			for _, cond := range nodeType.Conditions {
				set, err := eval(cond)
				if err != nil {
					return nil, err
				}
				for k := range set {
					res[k] = true
				}
			}
			return res, nil

		case *audience.Not:
			set, err := eval(nodeType.Condition)
			if err != nil {
				return nil, err
			}
			res := make(map[string]bool)
			for k := range allProfileIDs {
				if !set[k] {
					res[k] = true
				}
			}
			return res, nil

		case *audience.ProfileAttribute:
			sql, args, err := audience.CompileProfile(nodeType)
			if err != nil {
				return nil, err
			}

			pgArgs := append([]any{p.TenantID, p.WorkspaceID}, args...)
			pRows, err := s.pool.Query(ctx, sql, pgArgs...)
			if err != nil {
				return nil, err
			}
			defer pRows.Close()

			matchingProfileIDs := make(map[string]bool)
			for pRows.Next() {
				var extID string
				if err := pRows.Scan(&extID); err != nil {
					return nil, err
				}
				if pID, exists := externalIDToProfileID[extID]; exists {
					matchingProfileIDs[pID] = true
				}
			}
			perLegCounts["profile_attributes"] += len(matchingProfileIDs)
			return matchingProfileIDs, nil

		case *audience.Score:
			sql, args, err := audience.CompileProfile(nodeType)
			if err != nil {
				return nil, err
			}

			pgArgs := append([]any{p.TenantID, p.WorkspaceID}, args...)
			pRows, err := s.pool.Query(ctx, sql, pgArgs...)
			if err != nil {
				return nil, err
			}
			defer pRows.Close()

			matchingProfileIDs := make(map[string]bool)
			for pRows.Next() {
				var extID string
				if err := pRows.Scan(&extID); err != nil {
					return nil, err
				}
				if pID, exists := externalIDToProfileID[extID]; exists {
					matchingProfileIDs[pID] = true
				}
			}
			perLegCounts["scores"] += len(matchingProfileIDs)
			return matchingProfileIDs, nil

		case *audience.Consent:
			sql, pgArgs := audience.CompileConsent(nodeType, p.TenantID, p.AppID)
			cRows, err := s.pool.Query(ctx, sql, pgArgs...)
			if err != nil {
				return nil, err
			}
			defer cRows.Close()

			matchingProfileIDs := make(map[string]bool)
			for cRows.Next() {
				var pID string
				if err := cRows.Scan(&pID); err != nil {
					return nil, err
				}
				if allProfileIDs[pID] {
					matchingProfileIDs[pID] = true
				}
			}
			perLegCounts["consent"] += len(matchingProfileIDs)
			return matchingProfileIDs, nil

		case *audience.EventHistory:
			if s.chConn == nil {
				return nil, fmt.Errorf("ClickHouse connection not available for event-history evaluation")
			}
			sql, pgArgs := audience.CompileClickHouse(nodeType, p.TenantID)
			matchingProfileIDs := make(map[string]bool)

			rows, err := s.chConn.Query(ctx, sql, pgArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()

			for rows.Next() {
				var subjectHash string
				if err := rows.Scan(&subjectHash); err != nil {
					return nil, err
				}
				if pID, exists := hashToProfileID[subjectHash]; exists {
					matchingProfileIDs[pID] = true
				}
			}

			if nodeType.Operator == "has_not_occurred" {
				negated := make(map[string]bool)
				for k := range allProfileIDs {
					if !matchingProfileIDs[k] {
						negated[k] = true
					}
				}
				matchingProfileIDs = negated
			}

			perLegCounts["event_history"] += len(matchingProfileIDs)
			return matchingProfileIDs, nil

		default:
			return nil, fmt.Errorf("unknown AST node type: %T", n)
		}
	}

	eligibleProfileIDs := make(map[string]bool)
	if isDSL {
		var err error
		eligibleProfileIDs, err = eval(node)
		if err != nil {
			return nil, nil, err
		}
	}

	mRows, err := s.pool.Query(ctx, `SELECT profile_id, membership FROM segment_members
		WHERE tenant_id = $1 AND segment_id = $2`, p.TenantID, seg.ID)
	if err != nil {
		return nil, nil, err
	}
	defer mRows.Close()

	for mRows.Next() {
		var pID, membership string
		if err := mRows.Scan(&pID, &membership); err != nil {
			return nil, nil, err
		}
		if allProfileIDs[pID] {
			if membership == "exclude" {
				delete(eligibleProfileIDs, pID)
			} else if membership == "include" {
				eligibleProfileIDs[pID] = true
			}
		}
	}

	return eligibleProfileIDs, perLegCounts, nil
}

func (s *Store) PreviewSegment(ctx context.Context, p domain.Principal, id string) (int, map[string]int, error) {
	seg, err := s.GetSegment(ctx, p, id)
	if err != nil {
		return 0, nil, err
	}
	eligible, counts, err := s.resolveSegmentIDs(ctx, p, seg)
	if err != nil {
		return 0, nil, err
	}
	return len(eligible), counts, nil
}

func (s *Store) ResolveSegment(ctx context.Context, p domain.Principal, id string) ([]string, error) {
	seg, err := s.GetSegment(ctx, p, id)
	if err != nil {
		return nil, err
	}
	eligible, _, err := s.resolveSegmentIDs(ctx, p, seg)
	if err != nil {
		return nil, err
	}
	var out []string
	for k := range eligible {
		out = append(out, k)
	}
	return out, nil
}
