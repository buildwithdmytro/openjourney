package postgres

import (
	"context"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func (s *Store) RegisterDeviceToken(ctx context.Context, tenantID, workspaceID, appID, profileID, platform, provider, token string) (domain.DeviceToken, error) {
	var out domain.DeviceToken
	err := s.pool.QueryRow(ctx, `
		INSERT INTO device_tokens (tenant_id, workspace_id, app_id, profile_id, platform, provider, token, status, last_seen_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', now(), now())
		ON CONFLICT (tenant_id, app_id, token) DO UPDATE SET
			workspace_id = EXCLUDED.workspace_id,
			profile_id = EXCLUDED.profile_id,
			platform = EXCLUDED.platform,
			provider = EXCLUDED.provider,
			status = 'active',
			last_seen_at = now(),
			updated_at = now()
		RETURNING id, tenant_id, workspace_id, app_id, profile_id, platform, provider, token, status, last_seen_at, created_at, updated_at
	`, tenantID, workspaceID, appID, profileID, platform, provider, token).Scan(
		&out.ID, &out.TenantID, &out.WorkspaceID, &out.AppID, &out.ProfileID, &out.Platform, &out.Provider, &out.Token, &out.Status, &out.LastSeenAt, &out.CreatedAt, &out.UpdatedAt,
	)
	return out, err
}

func (s *Store) RetireDeviceToken(ctx context.Context, tenantID, appID, token string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE device_tokens
		SET status = 'retired', updated_at = now()
		WHERE tenant_id = $1 AND app_id = $2 AND token = $3 AND status = 'active'
	`, tenantID, appID, token)
	return err
}

func (s *Store) RetireDeviceTokenByID(ctx context.Context, tenantID, id string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE device_tokens
		SET status = 'retired', updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'active'
	`, tenantID, id)
	return err
}

func (s *Store) ListActiveDeviceTokens(ctx context.Context, tenantID, workspaceID, profileID string) ([]domain.DeviceToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, workspace_id, app_id, profile_id, platform, provider, token, status, last_seen_at, created_at, updated_at
		FROM device_tokens
		WHERE tenant_id = $1 AND workspace_id = $2 AND profile_id = $3 AND status = 'active'
		ORDER BY last_seen_at DESC
	`, tenantID, workspaceID, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.DeviceToken
	for rows.Next() {
		var item domain.DeviceToken
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.WorkspaceID, &item.AppID, &item.ProfileID, &item.Platform, &item.Provider, &item.Token, &item.Status, &item.LastSeenAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListDeviceTokensByProfile(ctx context.Context, tenantID, workspaceID, profileID string) ([]domain.DeviceToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, workspace_id, app_id, profile_id, platform, provider, token, status, last_seen_at, created_at, updated_at
		FROM device_tokens
		WHERE tenant_id = $1 AND workspace_id = $2 AND profile_id = $3
		ORDER BY last_seen_at DESC
	`, tenantID, workspaceID, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.DeviceToken
	for rows.Next() {
		var item domain.DeviceToken
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.WorkspaceID, &item.AppID, &item.ProfileID, &item.Platform, &item.Provider, &item.Token, &item.Status, &item.LastSeenAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
