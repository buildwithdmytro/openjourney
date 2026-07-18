package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateScoringModel(ctx context.Context, p domain.Principal, model domain.ScoringModel) (domain.ScoringModel, error) {
	if model.Name == "" {
		return domain.ScoringModel{}, errors.New("scoring model name is required")
	}
	if model.Kind == "" {
		return domain.ScoringModel{}, errors.New("scoring model kind is required")
	}
	if model.Kind != "expression" && model.Kind != "llm" {
		return domain.ScoringModel{}, fmt.Errorf("invalid scoring model kind: %s", model.Kind)
	}

	var out domain.ScoringModel
	err := s.pool.QueryRow(ctx, `INSERT INTO scoring_models (tenant_id, workspace_id, name, kind)
		VALUES ($1, $2, $3, $4)
		RETURNING id, tenant_id, workspace_id, name, kind, current_version_id, latest_version, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, model.Name, model.Kind).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Kind, &out.CurrentVersionID, &out.LatestVersion, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.ScoringModel{}, err
	}

	_ = s.audit(ctx, p, "scoring_model.create", "scoring_model", out.ID, map[string]any{"name": out.Name})
	return out, nil
}

func (s *Store) GetScoringModel(ctx context.Context, p domain.Principal, id string) (domain.ScoringModel, error) {
	var out domain.ScoringModel
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, name, kind, current_version_id, latest_version, created_at, updated_at
		FROM scoring_models WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3`,
		p.TenantID, p.WorkspaceID, id).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Kind, &out.CurrentVersionID, &out.LatestVersion, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ScoringModel{}, ErrNotFound
	}
	if err != nil {
		return domain.ScoringModel{}, err
	}
	return out, nil
}

func (s *Store) GetScoringModelByName(ctx context.Context, p domain.Principal, name string) (domain.ScoringModel, error) {
	var out domain.ScoringModel
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, name, kind, current_version_id, latest_version, created_at, updated_at
		FROM scoring_models WHERE tenant_id = $1 AND workspace_id = $2 AND name = $3`,
		p.TenantID, p.WorkspaceID, name).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Kind, &out.CurrentVersionID, &out.LatestVersion, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ScoringModel{}, ErrNotFound
	}
	if err != nil {
		return domain.ScoringModel{}, err
	}
	return out, nil
}

func (s *Store) ListScoringModels(ctx context.Context, p domain.Principal) ([]domain.ScoringModel, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, workspace_id, name, kind, current_version_id, latest_version, created_at, updated_at
		FROM scoring_models WHERE tenant_id = $1 AND workspace_id = $2 ORDER BY name ASC`,
		p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ScoringModel
	for rows.Next() {
		var item domain.ScoringModel
		if err := rows.Scan(&item.ID, &item.TenantID, &item.WorkspaceID, &item.Name, &item.Kind, &item.CurrentVersionID, &item.LatestVersion, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpdateScoringModel(ctx context.Context, p domain.Principal, model domain.ScoringModel) (domain.ScoringModel, error) {
	if model.Name == "" {
		return domain.ScoringModel{}, errors.New("scoring model name is required")
	}
	if model.Kind == "" {
		return domain.ScoringModel{}, errors.New("scoring model kind is required")
	}
	if model.Kind != "expression" && model.Kind != "llm" {
		return domain.ScoringModel{}, fmt.Errorf("invalid scoring model kind: %s", model.Kind)
	}

	var out domain.ScoringModel
	err := s.pool.QueryRow(ctx, `UPDATE scoring_models 
		SET name = $1, kind = $2, current_version_id = $3, latest_version = $4, updated_at = now()
		WHERE tenant_id = $5 AND workspace_id = $6 AND id = $7
		RETURNING id, tenant_id, workspace_id, name, kind, current_version_id, latest_version, created_at, updated_at`,
		model.Name, model.Kind, model.CurrentVersionID, model.LatestVersion, p.TenantID, p.WorkspaceID, model.ID).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Kind, &out.CurrentVersionID, &out.LatestVersion, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ScoringModel{}, ErrNotFound
	}
	if err != nil {
		return domain.ScoringModel{}, err
	}

	_ = s.audit(ctx, p, "scoring_model.update", "scoring_model", out.ID, map[string]any{"name": out.Name})
	return out, nil
}

func (s *Store) DeleteScoringModel(ctx context.Context, p domain.Principal, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM scoring_models WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3`, p.TenantID, p.WorkspaceID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	_ = s.audit(ctx, p, "scoring_model.delete", "scoring_model", id, nil)
	return nil
}

func (s *Store) CreateScoringModelVersion(ctx context.Context, p domain.Principal, sv domain.ScoringModelVersion) (domain.ScoringModelVersion, error) {
	if sv.ScoringModelID == "" {
		return domain.ScoringModelVersion{}, errors.New("scoring_model_id is required")
	}
	if sv.ScoreName == "" {
		return domain.ScoringModelVersion{}, errors.New("score_name is required")
	}
	if len(sv.Definition) == 0 {
		sv.Definition = json.RawMessage("{}")
	}
	if sv.Status == "" {
		sv.Status = "draft"
	}
	if sv.Status != "draft" && sv.Status != "active" && sv.Status != "archived" {
		return domain.ScoringModelVersion{}, fmt.Errorf("invalid status: %s", sv.Status)
	}
	if sv.EvalStatus == "" {
		sv.EvalStatus = "pending"
	}
	if sv.EvalStatus != "pending" && sv.EvalStatus != "passed" && sv.EvalStatus != "failed" {
		return domain.ScoringModelVersion{}, fmt.Errorf("invalid eval status: %s", sv.EvalStatus)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.ScoringModelVersion{}, err
	}
	defer tx.Rollback(ctx)

	// Verify scoring model exists and lock it
	var latestVersion int
	err = tx.QueryRow(ctx, `SELECT latest_version FROM scoring_models WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3 FOR UPDATE`,
		p.TenantID, p.WorkspaceID, sv.ScoringModelID).Scan(&latestVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ScoringModelVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.ScoringModelVersion{}, err
	}

	newVersion := latestVersion + 1

	var out domain.ScoringModelVersion
	err = tx.QueryRow(ctx, `INSERT INTO scoring_model_versions 
		(scoring_model_id, tenant_id, version, score_name, definition, output_min, output_max, manifest_key, status, eval_status, published_by, published_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULL, NULL)
		RETURNING id, scoring_model_id, tenant_id, version, score_name, definition, output_min, output_max, manifest_key, status, eval_status, published_by, published_at, created_at`,
		sv.ScoringModelID, p.TenantID, newVersion, sv.ScoreName, sv.Definition, sv.OutputMin, sv.OutputMax, sv.ManifestKey, sv.Status, sv.EvalStatus).
		Scan(&out.ID, &out.ScoringModelID, &out.TenantID, &out.Version, &out.ScoreName, &out.Definition, &out.OutputMin, &out.OutputMax, &out.ManifestKey, &out.Status, &out.EvalStatus, &out.PublishedBy, &out.PublishedAt, &out.CreatedAt)
	if err != nil {
		return domain.ScoringModelVersion{}, err
	}

	_, err = tx.Exec(ctx, `UPDATE scoring_models SET latest_version = $1, updated_at = now() WHERE tenant_id = $2 AND workspace_id = $3 AND id = $4`,
		newVersion, p.TenantID, p.WorkspaceID, sv.ScoringModelID)
	if err != nil {
		return domain.ScoringModelVersion{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ScoringModelVersion{}, err
	}

	_ = s.audit(ctx, p, "scoring_model_version.create", "scoring_model_version", out.ID, map[string]any{"version": out.Version})
	return out, nil
}

func (s *Store) GetScoringModelVersion(ctx context.Context, p domain.Principal, id string) (domain.ScoringModelVersion, error) {
	var out domain.ScoringModelVersion
	err := s.pool.QueryRow(ctx, `SELECT id, scoring_model_id, tenant_id, version, score_name, definition, output_min, output_max, manifest_key, status, eval_status, published_by, published_at, created_at
		FROM scoring_model_versions WHERE tenant_id = $1 AND id = $2`,
		p.TenantID, id).
		Scan(&out.ID, &out.ScoringModelID, &out.TenantID, &out.Version, &out.ScoreName, &out.Definition, &out.OutputMin, &out.OutputMax, &out.ManifestKey, &out.Status, &out.EvalStatus, &out.PublishedBy, &out.PublishedAt, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ScoringModelVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.ScoringModelVersion{}, err
	}
	return out, nil
}

func (s *Store) GetScoringModelVersionByNumber(ctx context.Context, p domain.Principal, modelID string, version int) (domain.ScoringModelVersion, error) {
	var out domain.ScoringModelVersion
	err := s.pool.QueryRow(ctx, `SELECT id, scoring_model_id, tenant_id, version, score_name, definition, output_min, output_max, manifest_key, status, eval_status, published_by, published_at, created_at
		FROM scoring_model_versions WHERE tenant_id = $1 AND scoring_model_id = $2 AND version = $3`,
		p.TenantID, modelID, version).
		Scan(&out.ID, &out.ScoringModelID, &out.TenantID, &out.Version, &out.ScoreName, &out.Definition, &out.OutputMin, &out.OutputMax, &out.ManifestKey, &out.Status, &out.EvalStatus, &out.PublishedBy, &out.PublishedAt, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ScoringModelVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.ScoringModelVersion{}, err
	}
	return out, nil
}

func (s *Store) ListScoringModelVersions(ctx context.Context, p domain.Principal, modelID string) ([]domain.ScoringModelVersion, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, scoring_model_id, tenant_id, version, score_name, definition, output_min, output_max, manifest_key, status, eval_status, published_by, published_at, created_at
		FROM scoring_model_versions WHERE tenant_id = $1 AND scoring_model_id = $2 ORDER BY version DESC`,
		p.TenantID, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ScoringModelVersion
	for rows.Next() {
		var item domain.ScoringModelVersion
		err := rows.Scan(&item.ID, &item.ScoringModelID, &item.TenantID, &item.Version, &item.ScoreName, &item.Definition, &item.OutputMin, &item.OutputMax, &item.ManifestKey, &item.Status, &item.EvalStatus, &item.PublishedBy, &item.PublishedAt, &item.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) PublishScoringModelVersion(ctx context.Context, p domain.Principal, modelID string, version int, approverUserID string, manifestKey string) (domain.ScoringModelVersion, error) {
	if approverUserID == "" {
		return domain.ScoringModelVersion{}, errors.New("approver user id is required")
	}
	if manifestKey == "" {
		return domain.ScoringModelVersion{}, errors.New("manifest key is required")
	}
	if p.ActorType != "user" || p.UserID == "" {
		return domain.ScoringModelVersion{}, ErrUnauthorized
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.ScoringModelVersion{}, err
	}
	defer tx.Rollback(ctx)

	var sv domain.ScoringModelVersion
	err = tx.QueryRow(ctx, `SELECT id, scoring_model_id, tenant_id, version, score_name, definition, output_min, output_max, manifest_key, status, eval_status, published_by, published_at, created_at
		FROM scoring_model_versions WHERE tenant_id = $1 AND scoring_model_id = $2 AND version = $3 FOR UPDATE`,
		p.TenantID, modelID, version).
		Scan(&sv.ID, &sv.ScoringModelID, &sv.TenantID, &sv.Version, &sv.ScoreName, &sv.Definition, &sv.OutputMin, &sv.OutputMax, &sv.ManifestKey, &sv.Status, &sv.EvalStatus, &sv.PublishedBy, &sv.PublishedAt, &sv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ScoringModelVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.ScoringModelVersion{}, err
	}

	if sv.Status == "active" {
		return sv, nil
	}

	if sv.EvalStatus != "passed" {
		return domain.ScoringModelVersion{}, errors.New("cannot publish version with non-passed eval status")
	}

	_, err = tx.Exec(ctx, `UPDATE scoring_model_versions SET status = 'archived' WHERE tenant_id = $1 AND scoring_model_id = $2 AND status = 'active'`,
		p.TenantID, modelID)
	if err != nil {
		return domain.ScoringModelVersion{}, err
	}

	var out domain.ScoringModelVersion
	err = tx.QueryRow(ctx, `UPDATE scoring_model_versions 
		SET status = 'active', published_by = $1, published_at = now(), manifest_key = $2
		WHERE tenant_id = $3 AND id = $4
		RETURNING id, scoring_model_id, tenant_id, version, score_name, definition, output_min, output_max, manifest_key, status, eval_status, published_by, published_at, created_at`,
		approverUserID, manifestKey, p.TenantID, sv.ID).
		Scan(&out.ID, &out.ScoringModelID, &out.TenantID, &out.Version, &out.ScoreName, &out.Definition, &out.OutputMin, &out.OutputMax, &out.ManifestKey, &out.Status, &out.EvalStatus, &out.PublishedBy, &out.PublishedAt, &out.CreatedAt)
	if err != nil {
		return domain.ScoringModelVersion{}, err
	}

	_, err = tx.Exec(ctx, `UPDATE scoring_models SET current_version_id = $1, updated_at = now() WHERE tenant_id = $2 AND id = $3`,
		out.ID, p.TenantID, modelID)
	if err != nil {
		return domain.ScoringModelVersion{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ScoringModelVersion{}, err
	}

	_ = s.audit(ctx, p, "scoring_model_version.publish", "scoring_model_version", out.ID, map[string]any{"version": out.Version})
	return out, nil
}

func (s *Store) SetScoringModelVersionEvalStatus(ctx context.Context, p domain.Principal, id string, evalStatus string) error {
	if evalStatus != "pending" && evalStatus != "passed" && evalStatus != "failed" {
		return fmt.Errorf("invalid eval status: %s", evalStatus)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE scoring_model_versions SET eval_status = $1 WHERE tenant_id = $2 AND id = $3`,
		evalStatus, p.TenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
