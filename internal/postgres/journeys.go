package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateJourney(ctx context.Context, p domain.Principal, j domain.Journey) (domain.Journey, error) {
	if j.Name == "" {
		return domain.Journey{}, errors.New("journey name is required")
	}
	if j.Status == "" {
		j.Status = "draft"
	}
	if len(j.Graph) == 0 {
		j.Graph = json.RawMessage("{}")
	}

	var out domain.Journey
	err := s.pool.QueryRow(ctx, `INSERT INTO journeys (tenant_id, workspace_id, name, description, status, graph)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, tenant_id, workspace_id, name, description, status, graph, latest_version, current_version_id, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, j.Name, j.Description, j.Status, j.Graph).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Description, &out.Status, &out.Graph, &out.LatestVersion, &out.CurrentVersionID, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.Journey{}, err
	}

	_ = s.audit(ctx, p, "journey.create", "journey", out.ID, map[string]any{"name": out.Name})
	return out, nil
}

func (s *Store) GetJourney(ctx context.Context, p domain.Principal, id string) (domain.Journey, error) {
	var out domain.Journey
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, name, description, status, graph, latest_version, current_version_id, created_at, updated_at
		FROM journeys WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, id).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Description, &out.Status, &out.Graph, &out.LatestVersion, &out.CurrentVersionID, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Journey{}, ErrNotFound
	}
	return out, err
}

func (s *Store) UpdateJourney(ctx context.Context, p domain.Principal, j domain.Journey) (domain.Journey, error) {
	existing, err := s.GetJourney(ctx, p, j.ID)
	if err != nil {
		return domain.Journey{}, err
	}
	if existing.Status == "published" && j.Status != "draft" {
		return domain.Journey{}, errors.New("published journeys cannot be edited without reverting to draft")
	}
	if j.Name == "" {
		j.Name = existing.Name
	}
	if j.Status == "" {
		j.Status = existing.Status
	}
	if len(j.Graph) == 0 {
		j.Graph = existing.Graph
	}

	var out domain.Journey
	err = s.pool.QueryRow(ctx, `UPDATE journeys
		SET name=$4, description=$5, status=$6, graph=$7, updated_at=now()
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3
		RETURNING id, tenant_id, workspace_id, name, description, status, graph, latest_version, current_version_id, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, j.ID, j.Name, j.Description, j.Status, j.Graph).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Description, &out.Status, &out.Graph, &out.LatestVersion, &out.CurrentVersionID, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Journey{}, ErrNotFound
	}
	if err != nil {
		return domain.Journey{}, err
	}

	_ = s.audit(ctx, p, "journey.update", "journey", out.ID, map[string]any{"status": out.Status})
	return out, nil
}

func (s *Store) ListJourneys(ctx context.Context, p domain.Principal) ([]domain.Journey, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, workspace_id, name, description, status, graph, latest_version, current_version_id, created_at, updated_at
		FROM journeys WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY created_at DESC`,
		p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Journey
	for rows.Next() {
		var j domain.Journey
		if err := rows.Scan(&j.ID, &j.TenantID, &j.WorkspaceID, &j.Name, &j.Description, &j.Status, &j.Graph, &j.LatestVersion, &j.CurrentVersionID, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
