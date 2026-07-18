package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

// redactConfigSecrets removes sensitive keys like "api_key" from the JSON raw message.
func redactConfigSecrets(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	delete(m, "api_key")
	delete(m, "secret")
	delete(m, "password")
	data, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return data
}

func (s *Store) CreateAIProviderConfig(ctx context.Context, p domain.Principal, cfg domain.AIProviderConfig) (domain.AIProviderConfig, error) {
	if cfg.Provider == "" {
		return domain.AIProviderConfig{}, errors.New("provider is required")
	}
	if cfg.Provider != "fake" && cfg.Provider != "anthropic" && cfg.Provider != "openai" {
		return domain.AIProviderConfig{}, fmt.Errorf("invalid provider: %s", cfg.Provider)
	}
	if len(cfg.Config) == 0 {
		cfg.Config = []byte("{}")
	}
	if cfg.Status == "" {
		cfg.Status = "active"
	}
	if cfg.Status != "active" && cfg.Status != "disabled" {
		return domain.AIProviderConfig{}, fmt.Errorf("invalid status: %s", cfg.Status)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AIProviderConfig{}, err
	}
	defer tx.Rollback(ctx)

	if cfg.IsDefault {
		_, err := tx.Exec(ctx, `UPDATE ai_provider_configs SET is_default = false WHERE tenant_id = $1 AND workspace_id = $2`, p.TenantID, p.WorkspaceID)
		if err != nil {
			return domain.AIProviderConfig{}, err
		}
	}

	var out domain.AIProviderConfig
	err = tx.QueryRow(ctx, `INSERT INTO ai_provider_configs 
		(tenant_id, workspace_id, provider, is_default, config, endpoint_allowlist, fallback_provider, monthly_budget_cents, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, tenant_id, workspace_id, provider, is_default, config, endpoint_allowlist, fallback_provider, monthly_budget_cents, status, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, cfg.Provider, cfg.IsDefault, redactConfigSecrets(cfg.Config), cfg.EndpointAllowlist, cfg.FallbackProvider, cfg.MonthlyBudgetCents, cfg.Status).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Provider, &out.IsDefault, &out.Config, &out.EndpointAllowlist, &out.FallbackProvider, &out.MonthlyBudgetCents, &out.Status, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.AIProviderConfig{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.AIProviderConfig{}, err
	}

	_ = s.audit(ctx, p, "ai_provider_config.create", "ai_provider_config", out.ID, map[string]any{"provider": out.Provider})
	out.Config = redactConfigSecrets(out.Config)
	return out, nil
}

func (s *Store) GetAIProviderConfig(ctx context.Context, p domain.Principal, id string) (domain.AIProviderConfig, error) {
	var out domain.AIProviderConfig
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, provider, is_default, config, endpoint_allowlist, fallback_provider, monthly_budget_cents, status, created_at, updated_at
		FROM ai_provider_configs WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, id).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Provider, &out.IsDefault, &out.Config, &out.EndpointAllowlist, &out.FallbackProvider, &out.MonthlyBudgetCents, &out.Status, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AIProviderConfig{}, ErrNotFound
	}
	if err != nil {
		return domain.AIProviderConfig{}, err
	}
	out.Config = redactConfigSecrets(out.Config)
	return out, nil
}

func (s *Store) GetDefaultAIProviderConfig(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error) {
	var out domain.AIProviderConfig
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, provider, is_default, config, endpoint_allowlist, fallback_provider, monthly_budget_cents, status, created_at, updated_at
		FROM ai_provider_configs WHERE tenant_id=$1 AND workspace_id=$2 AND is_default=true`,
		p.TenantID, p.WorkspaceID).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Provider, &out.IsDefault, &out.Config, &out.EndpointAllowlist, &out.FallbackProvider, &out.MonthlyBudgetCents, &out.Status, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AIProviderConfig{}, ErrNotFound
	}
	if err != nil {
		return domain.AIProviderConfig{}, err
	}
	out.Config = redactConfigSecrets(out.Config)
	return out, nil
}

func (s *Store) UpdateAIProviderConfig(ctx context.Context, p domain.Principal, cfg domain.AIProviderConfig) (domain.AIProviderConfig, error) {
	if cfg.Provider != "fake" && cfg.Provider != "anthropic" && cfg.Provider != "openai" {
		return domain.AIProviderConfig{}, fmt.Errorf("invalid provider: %s", cfg.Provider)
	}
	if cfg.Status != "active" && cfg.Status != "disabled" {
		return domain.AIProviderConfig{}, fmt.Errorf("invalid status: %s", cfg.Status)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AIProviderConfig{}, err
	}
	defer tx.Rollback(ctx)

	// Verify existence
	var existingID string
	err = tx.QueryRow(ctx, `SELECT id FROM ai_provider_configs WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, p.TenantID, p.WorkspaceID, cfg.ID).Scan(&existingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AIProviderConfig{}, ErrNotFound
	}
	if err != nil {
		return domain.AIProviderConfig{}, err
	}

	if cfg.IsDefault {
		_, err := tx.Exec(ctx, `UPDATE ai_provider_configs SET is_default = false WHERE tenant_id = $1 AND workspace_id = $2`, p.TenantID, p.WorkspaceID)
		if err != nil {
			return domain.AIProviderConfig{}, err
		}
	}

	var out domain.AIProviderConfig
	err = tx.QueryRow(ctx, `UPDATE ai_provider_configs 
		SET provider=$1, is_default=$2, config=$3, endpoint_allowlist=$4, fallback_provider=$5, monthly_budget_cents=$6, status=$7, updated_at=now()
		WHERE tenant_id=$8 AND workspace_id=$9 AND id=$10
		RETURNING id, tenant_id, workspace_id, provider, is_default, config, endpoint_allowlist, fallback_provider, monthly_budget_cents, status, created_at, updated_at`,
		cfg.Provider, cfg.IsDefault, redactConfigSecrets(cfg.Config), cfg.EndpointAllowlist, cfg.FallbackProvider, cfg.MonthlyBudgetCents, cfg.Status, p.TenantID, p.WorkspaceID, cfg.ID).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Provider, &out.IsDefault, &out.Config, &out.EndpointAllowlist, &out.FallbackProvider, &out.MonthlyBudgetCents, &out.Status, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.AIProviderConfig{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.AIProviderConfig{}, err
	}

	_ = s.audit(ctx, p, "ai_provider_config.update", "ai_provider_config", out.ID, map[string]any{"provider": out.Provider})
	out.Config = redactConfigSecrets(out.Config)
	return out, nil
}

func (s *Store) ListAIProviderConfigs(ctx context.Context, p domain.Principal) ([]domain.AIProviderConfig, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, workspace_id, provider, is_default, config, endpoint_allowlist, fallback_provider, monthly_budget_cents, status, created_at, updated_at
		FROM ai_provider_configs WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY created_at DESC`,
		p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.AIProviderConfig
	for rows.Next() {
		var cfg domain.AIProviderConfig
		err := rows.Scan(&cfg.ID, &cfg.TenantID, &cfg.WorkspaceID, &cfg.Provider, &cfg.IsDefault, &cfg.Config, &cfg.EndpointAllowlist, &cfg.FallbackProvider, &cfg.MonthlyBudgetCents, &cfg.Status, &cfg.CreatedAt, &cfg.UpdatedAt)
		if err != nil {
			return nil, err
		}
		cfg.Config = redactConfigSecrets(cfg.Config)
		out = append(out, cfg)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAIProviderConfig(ctx context.Context, p domain.Principal, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM ai_provider_configs WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, p.TenantID, p.WorkspaceID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	_ = s.audit(ctx, p, "ai_provider_config.delete", "ai_provider_config", id, nil)
	return nil
}
