package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreatePrompt(ctx context.Context, p domain.Principal, prompt domain.Prompt) (domain.Prompt, error) {
	if prompt.Name == "" {
		return domain.Prompt{}, errors.New("prompt name is required")
	}
	if prompt.TaskType == "" {
		return domain.Prompt{}, errors.New("task type is required")
	}
	if prompt.TaskType != "content_draft" && prompt.TaskType != "audience_dsl" && prompt.TaskType != "journey_draft" && prompt.TaskType != "performance_summary" && prompt.TaskType != "moderation" {
		return domain.Prompt{}, fmt.Errorf("invalid task type: %s", prompt.TaskType)
	}

	var out domain.Prompt
	err := s.pool.QueryRow(ctx, `INSERT INTO prompts (tenant_id, workspace_id, name, task_type)
		VALUES ($1, $2, $3, $4)
		RETURNING id, tenant_id, workspace_id, name, task_type, current_version_id, latest_version, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, prompt.Name, prompt.TaskType).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.TaskType, &out.CurrentVersionID, &out.LatestVersion, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.Prompt{}, err
	}

	_ = s.audit(ctx, p, "prompt.create", "prompt", out.ID, map[string]any{"name": out.Name})
	return out, nil
}

func (s *Store) GetPrompt(ctx context.Context, p domain.Principal, id string) (domain.Prompt, error) {
	var out domain.Prompt
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, name, task_type, current_version_id, latest_version, created_at, updated_at
		FROM prompts WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3`,
		p.TenantID, p.WorkspaceID, id).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.TaskType, &out.CurrentVersionID, &out.LatestVersion, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Prompt{}, ErrNotFound
	}
	if err != nil {
		return domain.Prompt{}, err
	}
	return out, nil
}

func (s *Store) GetPromptByName(ctx context.Context, p domain.Principal, name string) (domain.Prompt, error) {
	var out domain.Prompt
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, name, task_type, current_version_id, latest_version, created_at, updated_at
		FROM prompts WHERE tenant_id = $1 AND workspace_id = $2 AND name = $3`,
		p.TenantID, p.WorkspaceID, name).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.TaskType, &out.CurrentVersionID, &out.LatestVersion, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Prompt{}, ErrNotFound
	}
	if err != nil {
		return domain.Prompt{}, err
	}
	return out, nil
}

func (s *Store) ListPrompts(ctx context.Context, p domain.Principal) ([]domain.Prompt, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, workspace_id, name, task_type, current_version_id, latest_version, created_at, updated_at
		FROM prompts WHERE tenant_id = $1 AND workspace_id = $2 ORDER BY name ASC`,
		p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Prompt
	for rows.Next() {
		var item domain.Prompt
		if err := rows.Scan(&item.ID, &item.TenantID, &item.WorkspaceID, &item.Name, &item.TaskType, &item.CurrentVersionID, &item.LatestVersion, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpdatePrompt(ctx context.Context, p domain.Principal, prompt domain.Prompt) (domain.Prompt, error) {
	if prompt.Name == "" {
		return domain.Prompt{}, errors.New("prompt name is required")
	}
	if prompt.TaskType == "" {
		return domain.Prompt{}, errors.New("task type is required")
	}
	if prompt.TaskType != "content_draft" && prompt.TaskType != "audience_dsl" && prompt.TaskType != "journey_draft" && prompt.TaskType != "performance_summary" && prompt.TaskType != "moderation" {
		return domain.Prompt{}, fmt.Errorf("invalid task type: %s", prompt.TaskType)
	}

	var out domain.Prompt
	err := s.pool.QueryRow(ctx, `UPDATE prompts 
		SET name = $1, task_type = $2, current_version_id = $3, latest_version = $4, updated_at = now()
		WHERE tenant_id = $5 AND workspace_id = $6 AND id = $7
		RETURNING id, tenant_id, workspace_id, name, task_type, current_version_id, latest_version, created_at, updated_at`,
		prompt.Name, prompt.TaskType, prompt.CurrentVersionID, prompt.LatestVersion, p.TenantID, p.WorkspaceID, prompt.ID).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.TaskType, &out.CurrentVersionID, &out.LatestVersion, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Prompt{}, ErrNotFound
	}
	if err != nil {
		return domain.Prompt{}, err
	}

	_ = s.audit(ctx, p, "prompt.update", "prompt", out.ID, map[string]any{"name": out.Name})
	return out, nil
}

func (s *Store) DeletePrompt(ctx context.Context, p domain.Principal, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM prompts WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3`, p.TenantID, p.WorkspaceID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	_ = s.audit(ctx, p, "prompt.delete", "prompt", id, nil)
	return nil
}

func (s *Store) CreatePromptVersion(ctx context.Context, p domain.Principal, pv domain.PromptVersion) (domain.PromptVersion, error) {
	if pv.PromptID == "" {
		return domain.PromptVersion{}, errors.New("prompt_id is required")
	}
	if pv.Template == "" {
		return domain.PromptVersion{}, errors.New("template is required")
	}
	if len(pv.InputSchema) == 0 {
		pv.InputSchema = json.RawMessage("{}")
	}
	if len(pv.OutputSchema) == 0 {
		pv.OutputSchema = json.RawMessage("{}")
	}
	if pv.Provider == "" {
		return domain.PromptVersion{}, errors.New("provider is required")
	}
	if pv.Model == "" {
		return domain.PromptVersion{}, errors.New("model is required")
	}
	if len(pv.Params) == 0 {
		pv.Params = json.RawMessage("{}")
	}
	if len(pv.SafetyPolicy) == 0 {
		pv.SafetyPolicy = json.RawMessage("{}")
	}
	if pv.Status == "" {
		pv.Status = "draft"
	}
	if pv.Status != "draft" && pv.Status != "active" && pv.Status != "archived" {
		return domain.PromptVersion{}, fmt.Errorf("invalid status: %s", pv.Status)
	}
	if pv.EvalStatus == "" {
		pv.EvalStatus = "pending"
	}
	if pv.EvalStatus != "pending" && pv.EvalStatus != "passed" && pv.EvalStatus != "failed" {
		return domain.PromptVersion{}, fmt.Errorf("invalid eval status: %s", pv.EvalStatus)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.PromptVersion{}, err
	}
	defer tx.Rollback(ctx)

	// Verify prompt exists and lock it
	var latestVersion int
	err = tx.QueryRow(ctx, `SELECT latest_version FROM prompts WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3 FOR UPDATE`,
		p.TenantID, p.WorkspaceID, pv.PromptID).Scan(&latestVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PromptVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.PromptVersion{}, err
	}

	newVersion := latestVersion + 1

	var out domain.PromptVersion
	err = tx.QueryRow(ctx, `INSERT INTO prompt_versions 
		(prompt_id, tenant_id, version, template, input_schema, output_schema, provider, model, params, safety_policy, manifest_key, status, eval_status, published_by, published_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NULL, NULL)
		RETURNING id, prompt_id, tenant_id, version, template, input_schema, output_schema, provider, model, params, safety_policy, manifest_key, status, eval_status, published_by, published_at, created_at`,
		pv.PromptID, p.TenantID, newVersion, pv.Template, pv.InputSchema, pv.OutputSchema, pv.Provider, pv.Model, pv.Params, pv.SafetyPolicy, pv.ManifestKey, pv.Status, pv.EvalStatus).
		Scan(&out.ID, &out.PromptID, &out.TenantID, &out.Version, &out.Template, &out.InputSchema, &out.OutputSchema, &out.Provider, &out.Model, &out.Params, &out.SafetyPolicy, &out.ManifestKey, &out.Status, &out.EvalStatus, &out.PublishedBy, &out.PublishedAt, &out.CreatedAt)
	if err != nil {
		return domain.PromptVersion{}, err
	}

	_, err = tx.Exec(ctx, `UPDATE prompts SET latest_version = $1, updated_at = now() WHERE tenant_id = $2 AND workspace_id = $3 AND id = $4`,
		newVersion, p.TenantID, p.WorkspaceID, pv.PromptID)
	if err != nil {
		return domain.PromptVersion{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.PromptVersion{}, err
	}

	_ = s.audit(ctx, p, "prompt_version.create", "prompt_version", out.ID, map[string]any{"version": out.Version})
	return out, nil
}

func (s *Store) GetPromptVersion(ctx context.Context, p domain.Principal, id string) (domain.PromptVersion, error) {
	var out domain.PromptVersion
	err := s.pool.QueryRow(ctx, `SELECT id, prompt_id, tenant_id, version, template, input_schema, output_schema, provider, model, params, safety_policy, manifest_key, status, eval_status, published_by, published_at, created_at
		FROM prompt_versions WHERE tenant_id = $1 AND id = $2`,
		p.TenantID, id).
		Scan(&out.ID, &out.PromptID, &out.TenantID, &out.Version, &out.Template, &out.InputSchema, &out.OutputSchema, &out.Provider, &out.Model, &out.Params, &out.SafetyPolicy, &out.ManifestKey, &out.Status, &out.EvalStatus, &out.PublishedBy, &out.PublishedAt, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PromptVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.PromptVersion{}, err
	}
	return out, nil
}

func (s *Store) GetPromptVersionByNumber(ctx context.Context, p domain.Principal, promptID string, version int) (domain.PromptVersion, error) {
	var out domain.PromptVersion
	err := s.pool.QueryRow(ctx, `SELECT id, prompt_id, tenant_id, version, template, input_schema, output_schema, provider, model, params, safety_policy, manifest_key, status, eval_status, published_by, published_at, created_at
		FROM prompt_versions WHERE tenant_id = $1 AND prompt_id = $2 AND version = $3`,
		p.TenantID, promptID, version).
		Scan(&out.ID, &out.PromptID, &out.TenantID, &out.Version, &out.Template, &out.InputSchema, &out.OutputSchema, &out.Provider, &out.Model, &out.Params, &out.SafetyPolicy, &out.ManifestKey, &out.Status, &out.EvalStatus, &out.PublishedBy, &out.PublishedAt, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PromptVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.PromptVersion{}, err
	}
	return out, nil
}

func (s *Store) ListPromptVersions(ctx context.Context, p domain.Principal, promptID string) ([]domain.PromptVersion, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, prompt_id, tenant_id, version, template, input_schema, output_schema, provider, model, params, safety_policy, manifest_key, status, eval_status, published_by, published_at, created_at
		FROM prompt_versions WHERE tenant_id = $1 AND prompt_id = $2 ORDER BY version DESC`,
		p.TenantID, promptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.PromptVersion
	for rows.Next() {
		var item domain.PromptVersion
		err := rows.Scan(&item.ID, &item.PromptID, &item.TenantID, &item.Version, &item.Template, &item.InputSchema, &item.OutputSchema, &item.Provider, &item.Model, &item.Params, &item.SafetyPolicy, &item.ManifestKey, &item.Status, &item.EvalStatus, &item.PublishedBy, &item.PublishedAt, &item.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) PublishPromptVersion(ctx context.Context, p domain.Principal, promptID string, version int, approverUserID string, manifestKey string) (domain.PromptVersion, error) {
	if approverUserID == "" {
		return domain.PromptVersion{}, errors.New("approver user id is required")
	}
	if manifestKey == "" {
		return domain.PromptVersion{}, errors.New("manifest key is required")
	}
	if p.ActorType != "user" || p.UserID == "" {
		return domain.PromptVersion{}, ErrUnauthorized
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.PromptVersion{}, err
	}
	defer tx.Rollback(ctx)

	var pv domain.PromptVersion
	err = tx.QueryRow(ctx, `SELECT id, prompt_id, tenant_id, version, template, input_schema, output_schema, provider, model, params, safety_policy, manifest_key, status, eval_status, published_by, published_at, created_at
		FROM prompt_versions WHERE tenant_id = $1 AND prompt_id = $2 AND version = $3 FOR UPDATE`,
		p.TenantID, promptID, version).
		Scan(&pv.ID, &pv.PromptID, &pv.TenantID, &pv.Version, &pv.Template, &pv.InputSchema, &pv.OutputSchema, &pv.Provider, &pv.Model, &pv.Params, &pv.SafetyPolicy, &pv.ManifestKey, &pv.Status, &pv.EvalStatus, &pv.PublishedBy, &pv.PublishedAt, &pv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PromptVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.PromptVersion{}, err
	}

	if pv.Status == "active" {
		return pv, nil
	}

	if pv.EvalStatus != "passed" {
		return domain.PromptVersion{}, errors.New("cannot publish version with non-passed eval status")
	}

	_, err = tx.Exec(ctx, `UPDATE prompt_versions SET status = 'archived' WHERE tenant_id = $1 AND prompt_id = $2 AND status = 'active'`,
		p.TenantID, promptID)
	if err != nil {
		return domain.PromptVersion{}, err
	}

	var out domain.PromptVersion
	err = tx.QueryRow(ctx, `UPDATE prompt_versions 
		SET status = 'active', published_by = $1, published_at = now(), manifest_key = $2
		WHERE tenant_id = $3 AND id = $4
		RETURNING id, prompt_id, tenant_id, version, template, input_schema, output_schema, provider, model, params, safety_policy, manifest_key, status, eval_status, published_by, published_at, created_at`,
		approverUserID, manifestKey, p.TenantID, pv.ID).
		Scan(&out.ID, &out.PromptID, &out.TenantID, &out.Version, &out.Template, &out.InputSchema, &out.OutputSchema, &out.Provider, &out.Model, &out.Params, &out.SafetyPolicy, &out.ManifestKey, &out.Status, &out.EvalStatus, &out.PublishedBy, &out.PublishedAt, &out.CreatedAt)
	if err != nil {
		return domain.PromptVersion{}, err
	}

	_, err = tx.Exec(ctx, `UPDATE prompts SET current_version_id = $1, updated_at = now() WHERE tenant_id = $2 AND id = $3`,
		out.ID, p.TenantID, promptID)
	if err != nil {
		return domain.PromptVersion{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.PromptVersion{}, err
	}

	_ = s.audit(ctx, p, "prompt_version.publish", "prompt_version", out.ID, map[string]any{"version": out.Version})
	return out, nil
}

func (s *Store) SetPromptVersionEvalStatus(ctx context.Context, p domain.Principal, id string, evalStatus string) error {
	if evalStatus != "pending" && evalStatus != "passed" && evalStatus != "failed" {
		return fmt.Errorf("invalid eval status: %s", evalStatus)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE prompt_versions SET eval_status = $1 WHERE tenant_id = $2 AND id = $3`,
		evalStatus, p.TenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
