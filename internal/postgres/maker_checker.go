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
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.MakerCheckerPolicy{}, err
	}
	defer tx.Rollback(ctx)

	if resourceType == "" {
		return domain.MakerCheckerPolicy{}, errors.New("resource type is required")
	}
	var out domain.MakerCheckerPolicy
	err = tx.QueryRow(ctx, `INSERT INTO maker_checker_policies (tenant_id, resource_type, require_checker, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (tenant_id, resource_type) DO UPDATE SET require_checker=EXCLUDED.require_checker, updated_at=now()
		RETURNING id, tenant_id, resource_type, require_checker, created_at, updated_at`,
		p.TenantID, resourceType, requireChecker).
		Scan(&out.ID, &out.TenantID, &out.ResourceType, &out.RequireChecker, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.MakerCheckerPolicy{}, err
	}
	if err := s.audit(ctx, tx, p, "maker_checker.set_policy", "maker_checker_policy", out.ID, map[string]any{
		"resource_type":   resourceType,
		"require_checker": requireChecker,
	}); err != nil {
		return domain.MakerCheckerPolicy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.MakerCheckerPolicy{}, err
	}
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

// EvaluateMakerChecker evaluates whether approverUserID is allowed to approve a resource
// given the maker-checker policy requirement and the set of known creator/editor actorIDs.
// If requireChecker is true, the set of actors must be non-empty (failing closed if unknown)
// and approverUserID must not be present in the actor set.
func EvaluateMakerChecker(requireChecker bool, approverUserID string, actorIDs []string) error {
	if !requireChecker {
		return nil
	}
	if len(actorIDs) == 0 {
		return ErrSelfApproval
	}
	for _, id := range actorIDs {
		if id != "" && id == approverUserID {
			return ErrSelfApproval
		}
	}
	return nil
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

	actorsMap := make(map[string]struct{})
	if creatorOrEditorID != "" {
		actorsMap[creatorOrEditorID] = struct{}{}
	}
	if resourceID != "" {
		rows, err := s.pool.Query(ctx, `SELECT DISTINCT actor_id FROM audit_events
			WHERE tenant_id=$1 AND resource_type=$2 AND resource_id=$3 AND actor_id IS NOT NULL AND actor_id != ''`,
			p.TenantID, resourceType, resourceID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var actorID string
				if scanErr := rows.Scan(&actorID); scanErr == nil && actorID != "" {
					actorsMap[actorID] = struct{}{}
				}
			}
		}
	}

	var actors []string
	for id := range actorsMap {
		actors = append(actors, id)
	}

	return EvaluateMakerChecker(requireChecker, p.UserID, actors)
}

