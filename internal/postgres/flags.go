package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

const flagColumns = `id, tenant_id, workspace_id, app_id, environment, key, name, description, flag_type,
	default_value, variants, targeting_rules, rollout_pct, seed, enabled, status, current_version_id, created_at, updated_at`

func scanFeatureFlag(row pgx.Row) (domain.FeatureFlag, error) {
	var out domain.FeatureFlag
	var variants, rules []byte

	err := row.Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.AppID, &out.Environment, &out.Key,
		&out.Name, &out.Description, &out.FlagType, &out.DefaultValue, &variants, &rules, &out.RolloutPct,
		&out.Seed, &out.Enabled, &out.Status, &out.CurrentVersionID, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.FeatureFlag{}, err
	}

	if len(variants) > 0 {
		if err := json.Unmarshal(variants, &out.Variants); err != nil {
			return domain.FeatureFlag{}, err
		}
	}
	if len(rules) > 0 {
		if err := json.Unmarshal(rules, &out.TargetingRules); err != nil {
			return domain.FeatureFlag{}, err
		}
	}

	return out, nil
}

func normalizeFeatureFlag(f domain.FeatureFlag) (domain.FeatureFlag, error) {
	if f.Key == "" {
		return domain.FeatureFlag{}, errors.New("flag key is required")
	}
	if f.FlagType == "" {
		return domain.FeatureFlag{}, errors.New("flag_type is required")
	}
	if f.Environment == "" {
		f.Environment = "production"
	}
	if f.Status == "" {
		f.Status = "draft"
	}
	if f.Seed == "" {
		return domain.FeatureFlag{}, errors.New("seed is required")
	}
	if f.RolloutPct < 0 || f.RolloutPct > 100 {
		return domain.FeatureFlag{}, errors.New("rollout_pct must be between 0 and 100")
	}
	// Validate environment
	if f.Environment != "development" && f.Environment != "staging" && f.Environment != "production" {
		return domain.FeatureFlag{}, errors.New("environment must be one of: development, staging, production")
	}
	// Validate flag_type
	if f.FlagType != "boolean" && f.FlagType != "string" && f.FlagType != "number" && f.FlagType != "json" {
		return domain.FeatureFlag{}, errors.New("flag_type must be one of: boolean, string, number, json")
	}
	// Validate status
	if f.Status != "draft" && f.Status != "published" && f.Status != "disabled" {
		return domain.FeatureFlag{}, errors.New("status must be one of: draft, published, disabled")
	}

	return f, nil
}

func (s *Store) CreateFeatureFlag(ctx context.Context, p domain.Principal, input domain.FeatureFlag) (domain.FeatureFlag, error) {
	f, err := normalizeFeatureFlag(input)
	if err != nil {
		return domain.FeatureFlag{}, err
	}

	variantsJSON, err := json.Marshal(f.Variants)
	if err != nil {
		return domain.FeatureFlag{}, err
	}
	rulesJSON, err := json.Marshal(f.TargetingRules)
	if err != nil {
		return domain.FeatureFlag{}, err
	}

	out, err := scanFeatureFlag(s.pool.QueryRow(ctx, `INSERT INTO feature_flags
		(tenant_id, workspace_id, app_id, environment, key, name, description, flag_type,
		 default_value, variants, targeting_rules, rollout_pct, seed, enabled, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING `+flagColumns,
		p.TenantID, p.WorkspaceID, f.AppID, f.Environment, f.Key, f.Name, f.Description, f.FlagType,
		f.DefaultValue, variantsJSON, rulesJSON, f.RolloutPct, f.Seed, f.Enabled, f.Status))
	if err != nil {
		if strings.Contains(err.Error(), "unique constraint") {
			return domain.FeatureFlag{}, errors.New("flag key already exists for this environment and app")
		}
		return domain.FeatureFlag{}, err
	}

	_ = s.audit(ctx, p, "flag.create", "flag", out.ID, map[string]any{"key": out.Key, "environment": out.Environment})
	return out, nil
}

func (s *Store) GetFeatureFlag(ctx context.Context, p domain.Principal, id string) (domain.FeatureFlag, error) {
	f, err := scanFeatureFlag(s.pool.QueryRow(ctx, `SELECT `+flagColumns+` FROM feature_flags
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FeatureFlag{}, ErrNotFound
	}
	return f, err
}

func (s *Store) UpdateFeatureFlag(ctx context.Context, p domain.Principal, input domain.FeatureFlag) (domain.FeatureFlag, error) {
	f, err := normalizeFeatureFlag(input)
	if err != nil {
		return domain.FeatureFlag{}, err
	}
	if f.ID == "" {
		return domain.FeatureFlag{}, errors.New("flag id is required")
	}

	variantsJSON, err := json.Marshal(f.Variants)
	if err != nil {
		return domain.FeatureFlag{}, err
	}
	rulesJSON, err := json.Marshal(f.TargetingRules)
	if err != nil {
		return domain.FeatureFlag{}, err
	}

	out, err := scanFeatureFlag(s.pool.QueryRow(ctx, `UPDATE feature_flags
		SET name=$4, description=$5, default_value=$6, variants=$7, targeting_rules=$8,
		    rollout_pct=$9, enabled=$10, status=$11, updated_at=now()
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3
		RETURNING `+flagColumns,
		p.TenantID, p.WorkspaceID, f.ID, f.Name, f.Description, f.DefaultValue, variantsJSON, rulesJSON,
		f.RolloutPct, f.Enabled, f.Status))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FeatureFlag{}, ErrNotFound
	}
	if err != nil {
		return domain.FeatureFlag{}, err
	}

	_ = s.audit(ctx, p, "flag.update", "flag", out.ID, map[string]any{"key": out.Key})
	return out, nil
}

func (s *Store) ListFeatureFlags(ctx context.Context, p domain.Principal) ([]domain.FeatureFlag, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+flagColumns+` FROM feature_flags
		WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY environment, key`,
		p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.FeatureFlag
	for rows.Next() {
		f, err := scanFeatureFlag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) ListActiveFlags(ctx context.Context, tenantID, appID, environment string) ([]domain.FeatureFlag, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+flagColumns+` FROM feature_flags
		WHERE tenant_id=$1 AND app_id=$2 AND environment=$3
		AND status='published' AND enabled=true
		ORDER BY key`,
		tenantID, appID, environment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.FeatureFlag
	for rows.Next() {
		f, err := scanFeatureFlag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) PublishFeatureFlag(ctx context.Context, p domain.Principal, flagID string, approverUserID string, manifestKey string) (domain.FeatureFlagVersion, error) {
	if approverUserID == "" {
		return domain.FeatureFlagVersion{}, errors.New("approver user id is required")
	}
	if manifestKey == "" {
		return domain.FeatureFlagVersion{}, errors.New("manifest key is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.FeatureFlagVersion{}, err
	}
	defer tx.Rollback(ctx)

	var draft domain.FeatureFlag
	err = tx.QueryRow(ctx, `SELECT `+flagColumns+` FROM feature_flags
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`,
		p.TenantID, p.WorkspaceID, flagID).
		Scan(&draft.ID, &draft.TenantID, &draft.WorkspaceID, &draft.AppID, &draft.Environment, &draft.Key,
			&draft.Name, &draft.Description, &draft.FlagType, &draft.DefaultValue, &draft.Variants,
			&draft.TargetingRules, &draft.RolloutPct, &draft.Seed, &draft.Enabled, &draft.Status,
			&draft.CurrentVersionID, &draft.CreatedAt, &draft.UpdatedAt)
	if err != nil {
		var variants, rules []byte
		if scanErr := tx.QueryRow(ctx, `SELECT `+flagColumns+` FROM feature_flags
			WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`,
			p.TenantID, p.WorkspaceID, flagID).
			Scan(&draft.ID, &draft.TenantID, &draft.WorkspaceID, &draft.AppID, &draft.Environment, &draft.Key,
				&draft.Name, &draft.Description, &draft.FlagType, &draft.DefaultValue, &variants, &rules, &draft.RolloutPct,
				&draft.Seed, &draft.Enabled, &draft.Status, &draft.CurrentVersionID, &draft.CreatedAt, &draft.UpdatedAt); scanErr != nil {
			if errors.Is(scanErr, pgx.ErrNoRows) {
				return domain.FeatureFlagVersion{}, ErrNotFound
			}
			return domain.FeatureFlagVersion{}, scanErr
		}
		if len(variants) > 0 {
			if err := json.Unmarshal(variants, &draft.Variants); err != nil {
				return domain.FeatureFlagVersion{}, err
			}
		}
		if len(rules) > 0 {
			if err := json.Unmarshal(rules, &draft.TargetingRules); err != nil {
				return domain.FeatureFlagVersion{}, err
			}
		}
	}

	// Build canonical definition: all rule config that matters for evaluation
	definition := map[string]any{
		"key":             draft.Key,
		"type":            draft.FlagType,
		"default_value":   draft.DefaultValue,
		"variants":        draft.Variants,
		"targeting_rules": draft.TargetingRules,
		"rollout_pct":     draft.RolloutPct,
		"seed":            draft.Seed,
		"enabled":         draft.Enabled,
	}
	canonicalJSON, err := json.Marshal(definition)
	if err != nil {
		return domain.FeatureFlagVersion{}, fmt.Errorf("marshal definition: %w", err)
	}

	digest := sha256.Sum256(canonicalJSON)
	digestStr := fmt.Sprintf("%x", digest)

	// Verify manifest key digest matches
	if strings.Contains(manifestKey, "/manifests/") && !strings.HasSuffix(manifestKey, fmt.Sprintf("/manifests/%s.json", digestStr)) {
		return domain.FeatureFlagVersion{}, errors.New("flag draft changed during publication; retry publish")
	}

	// Get next version number
	var version int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM feature_flag_versions WHERE flag_id=$1`, flagID).Scan(&version); err != nil {
		return domain.FeatureFlagVersion{}, err
	}

	// Insert version row
	var out domain.FeatureFlagVersion
	err = tx.QueryRow(ctx, `INSERT INTO feature_flag_versions
		(flag_id, version, definition_key, definition_sha, definition, created_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, flag_id, tenant_id, workspace_id, version, definition_key, definition_sha, definition, created_by_user_id, created_at`,
		flagID, version, manifestKey, digestStr, canonicalJSON, approverUserID).
		Scan(&out.ID, &out.FlagID, &out.TenantID, &out.WorkspaceID, &out.Version, &out.DefinitionKey,
			&out.DefinitionSha, &out.Definition, &out.CreatedByUserID, &out.CreatedAt)
	if err != nil {
		return domain.FeatureFlagVersion{}, err
	}

	// Update flag's current_version_id and status
	if _, err := tx.Exec(ctx, `UPDATE feature_flags
		SET status='published', current_version_id=$1, updated_at=now()
		WHERE tenant_id=$2 AND workspace_id=$3 AND id=$4`,
		out.ID, p.TenantID, p.WorkspaceID, flagID); err != nil {
		return domain.FeatureFlagVersion{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.FeatureFlagVersion{}, err
	}

	_ = s.audit(ctx, p, "flag.publish", "flag", flagID, map[string]any{"version": version, "key": draft.Key})
	return out, nil
}
