package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateSAMLProvider(ctx context.Context, p domain.Principal, input domain.SAMLProvider) (domain.SAMLProvider, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.SAMLProvider{}, err
	}
	defer tx.Rollback(ctx)

	if input.IDPEntityID == "" || input.IDPSSOURL == "" || input.IDPCert == "" || input.SPEntityID == "" {
		return domain.SAMLProvider{}, errors.New("idp_entity_id, idp_sso_url, idp_cert, and sp_entity_id are required")
	}
	if input.Status == "" {
		input.Status = "active"
	}
	input.TenantID = p.TenantID
	err = tx.QueryRow(ctx, `INSERT INTO saml_providers
		(tenant_id, idp_entity_id, idp_sso_url, idp_cert, sp_entity_id, default_role_id, enabled, status)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6,'')::uuid, $7, $8)
		RETURNING id, default_role_id, created_at, updated_at`,
		p.TenantID, input.IDPEntityID, input.IDPSSOURL, input.IDPCert, input.SPEntityID,
		input.DefaultRoleID, input.Enabled, input.Status).
		Scan(&input.ID, &input.DefaultRoleID, &input.CreatedAt, &input.UpdatedAt)
	if err != nil {
		return domain.SAMLProvider{}, err
	}
	if err := s.audit(ctx, tx, p, "saml_provider.create", "saml_provider", input.ID, map[string]any{
		"idp_entity_id": input.IDPEntityID,
	}); err != nil {
		return domain.SAMLProvider{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SAMLProvider{}, err
	}
	return input, nil
}

func (s *Store) GetSAMLProvider(ctx context.Context, tenantID, idpEntityID string) (domain.SAMLProvider, error) {
	var sp domain.SAMLProvider
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, idp_entity_id, idp_sso_url, idp_cert, sp_entity_id,
		COALESCE(default_role_id::text,''), enabled, status, created_at, updated_at
		FROM saml_providers
		WHERE tenant_id=$1 AND idp_entity_id=$2`, tenantID, idpEntityID).
		Scan(&sp.ID, &sp.TenantID, &sp.IDPEntityID, &sp.IDPSSOURL, &sp.IDPCert, &sp.SPEntityID,
			&sp.DefaultRoleID, &sp.Enabled, &sp.Status, &sp.CreatedAt, &sp.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SAMLProvider{}, ErrNotFound
	}
	if err != nil {
		return domain.SAMLProvider{}, err
	}
	return sp, nil
}

func (s *Store) ListSAMLProviders(ctx context.Context, tenantID string) ([]domain.SAMLProvider, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, idp_entity_id, idp_sso_url, idp_cert, sp_entity_id,
		COALESCE(default_role_id::text,''), enabled, status, created_at, updated_at
		FROM saml_providers
		WHERE tenant_id=$1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SAMLProvider
	for rows.Next() {
		var sp domain.SAMLProvider
		if err := rows.Scan(&sp.ID, &sp.TenantID, &sp.IDPEntityID, &sp.IDPSSOURL, &sp.IDPCert, &sp.SPEntityID,
			&sp.DefaultRoleID, &sp.Enabled, &sp.Status, &sp.CreatedAt, &sp.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func (s *Store) UpsertSAMLUserAndCreateSession(ctx context.Context, tenantID, idpEntityID, nameID, email, displayName string) (domain.AuthSession, error) {
	if tenantID == "" || idpEntityID == "" || nameID == "" {
		return domain.AuthSession{}, ErrUnauthorized
	}

	var providerID string
	var defaultRoleID *string
	var enabled bool
	var status string
	err := s.pool.QueryRow(ctx, `SELECT id, default_role_id, enabled, status
		FROM saml_providers
		WHERE tenant_id=$1 AND idp_entity_id=$2`, tenantID, idpEntityID).
		Scan(&providerID, &defaultRoleID, &enabled, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AuthSession{}, ErrUnauthorized
	}
	if err != nil {
		return domain.AuthSession{}, err
	}
	if !enabled || status != "active" {
		return domain.AuthSession{}, ErrUnauthorized
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AuthSession{}, err
	}
	defer tx.Rollback(ctx)

	var userID string
	var disabledAt *time.Time
	err = tx.QueryRow(ctx, `SELECT id, disabled_at FROM users
		WHERE tenant_id=$1 AND oidc_issuer=$2 AND oidc_subject=$3`, tenantID, idpEntityID, nameID).
		Scan(&userID, &disabledAt)

	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO users (tenant_id, oidc_issuer, oidc_subject, email, display_name)
			VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''))
			RETURNING id`, tenantID, idpEntityID, nameID, email, displayName).Scan(&userID)
		if err != nil {
			return domain.AuthSession{}, err
		}

		if defaultRoleID != nil && *defaultRoleID != "" {
			var wsID string
			_ = tx.QueryRow(ctx, `SELECT workspace_id FROM applications WHERE tenant_id=$1 LIMIT 1`, tenantID).Scan(&wsID)
			_, _ = tx.Exec(ctx, `INSERT INTO role_bindings (tenant_id, workspace_id, user_id, role_id)
				SELECT $1, NULLIF($2,'')::uuid, $3, id FROM roles WHERE id=$4 AND tenant_id=$1
				ON CONFLICT DO NOTHING`, tenantID, wsID, userID, *defaultRoleID)
		}
	} else if err != nil {
		return domain.AuthSession{}, err
	} else {
		if disabledAt != nil {
			return domain.AuthSession{}, ErrUnauthorized
		}
		_, _ = tx.Exec(ctx, `UPDATE users SET email=COALESCE(NULLIF($1,''), email), display_name=COALESCE(NULLIF($2,''), display_name), updated_at=now()
			WHERE tenant_id=$3 AND id=$4`, email, displayName, tenantID, userID)
	}

	var workspaceID, appID string
	err = s.pool.QueryRow(ctx, `SELECT workspace_id, id FROM applications WHERE tenant_id=$1 ORDER BY created_at LIMIT 1`, tenantID).
		Scan(&workspaceID, &appID)
	if err != nil {
		return domain.AuthSession{}, err
	}

	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return domain.AuthSession{}, err
	}
	raw := "ojs_" + base64.RawURLEncoding.EncodeToString(random)
	hash := sha256.Sum256([]byte(raw))
	expiresAt := time.Now().UTC().Add(12 * time.Hour)

	if _, err := s.pool.Exec(ctx, `INSERT INTO user_sessions
		(tenant_id, workspace_id, app_id, user_id, token_hash, expires_at, last_used_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())`, tenantID, workspaceID, appID, userID, hash[:], expiresAt); err != nil {
		return domain.AuthSession{}, err
	}

	if err := s.audit(ctx, tx, domain.Principal{
		TenantID: tenantID, WorkspaceID: workspaceID, AppID: appID, UserID: userID, ActorType: "user",
	}, "saml.login", "user_session", "", nil); err != nil {
		return domain.AuthSession{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AuthSession{}, err
	}

	return domain.AuthSession{AccessToken: raw, TokenType: "Bearer", ExpiresAt: expiresAt}, nil
}
