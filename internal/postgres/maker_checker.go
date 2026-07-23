package postgres

import (
	"context"
	"errors"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

var ErrSelfApproval = errors.New("self approval forbidden: creator cannot approve their own draft")

func (s *Store) GetMakerCheckerPolicy(ctx context.Context, p domain.Principal, resourceType string) (bool, error) {
	if resourceType == "" {
		return false, errors.New("resource type is required")
	}
	var requireChecker bool
	err := s.pool.QueryRow(ctx, `SELECT require_checker FROM maker_checker_policies WHERE tenant_id=$1 AND resource_type=$2`,
		p.TenantID, resourceType).Scan(&requireChecker)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return requireChecker, nil
}

func (s *Store) SetMakerCheckerPolicy(ctx context.Context, p domain.Principal, resourceType string, requireChecker bool) (domain.MakerCheckerPolicy, error) {
	if resourceType == "" {
		return domain.MakerCheckerPolicy{}, errors.New("resource type is required")
	}
	var out domain.MakerCheckerPolicy
	err := s.pool.QueryRow(ctx, `INSERT INTO maker_checker_policies (tenant_id, resource_type, require_checker, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (tenant_id, resource_type) DO UPDATE SET require_checker=EXCLUDED.require_checker, updated_at=now()
		RETURNING id, tenant_id, resource_type, require_checker, created_at, updated_at`,
		p.TenantID, resourceType, requireChecker).
		Scan(&out.ID, &out.TenantID, &out.ResourceType, &out.RequireChecker, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.MakerCheckerPolicy{}, err
	}
	_ = s.audit(ctx, p, "maker_checker.set_policy", "maker_checker_policy", out.ID, map[string]any{
		"resource_type":   resourceType,
		"require_checker": requireChecker,
	})
	return out, nil
}

func (s *Store) ListMakerCheckerPolicies(ctx context.Context, p domain.Principal) ([]domain.MakerCheckerPolicy, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, resource_type, require_checker, created_at, updated_at
		FROM maker_checker_policies WHERE tenant_id=$1 ORDER BY resource_type`, p.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.MakerCheckerPolicy
	for rows.Next() {
		var item domain.MakerCheckerPolicy
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ResourceType, &item.RequireChecker, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if result == nil {
		result = []domain.MakerCheckerPolicy{}
	}
	return result, rows.Err()
}

func (s *Store) CheckMakerChecker(ctx context.Context, p domain.Principal, resourceType, resourceID string, creatorOrEditorID string) error {
	if p.ActorType != "user" || p.UserID == "" {
		return errors.New("publishing requires an authenticated user")
	}
	requireChecker, err := s.GetMakerCheckerPolicy(ctx, p, resourceType)
	if err != nil {
		return err
	}
	if !requireChecker {
		return nil
	}
	if creatorOrEditorID == "" && resourceID != "" {
		var actorID string
		err := s.pool.QueryRow(ctx, `SELECT actor_id FROM audit_events
			WHERE tenant_id=$1 AND resource_type=$2 AND resource_id=$3 AND actor_id IS NOT NULL
			ORDER BY occurred_at DESC LIMIT 1`, p.TenantID, resourceType, resourceID).Scan(&actorID)
		if err == nil && actorID != "" {
			creatorOrEditorID = actorID
		}
	}
	if creatorOrEditorID != "" && creatorOrEditorID == p.UserID {
		return ErrSelfApproval
	}
	return nil
}
