package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

func (s *Store) AuthenticateOIDC(ctx context.Context, claims domain.OIDCClaims) (domain.Principal, error) {
	principal := domain.Principal{
		TenantID: claims.TenantID, WorkspaceID: claims.WorkspaceID,
		AppID: claims.AppID, ActorType: "user",
	}
	err := s.pool.QueryRow(ctx, `SELECT u.id,
		COALESCE(array_agg(DISTINCT permission) FILTER (WHERE permission IS NOT NULL),'{}')
		FROM users u
		JOIN LATERAL (
			SELECT r.permissions
			FROM role_bindings b JOIN roles r ON r.id=b.role_id AND r.tenant_id=u.tenant_id
			WHERE b.user_id=u.id AND b.tenant_id=u.tenant_id
			  AND (b.workspace_id IS NULL OR b.workspace_id=$4)
			UNION ALL
			SELECT r.permissions
			FROM team_members tm
			JOIN teams t ON t.id=tm.team_id AND t.tenant_id=u.tenant_id AND t.workspace_id=$4
			JOIN team_roles tr ON tr.team_id=t.id
			JOIN roles r ON r.id=tr.role_id AND r.tenant_id=u.tenant_id
			WHERE tm.user_id=u.id
		) effective_roles ON true
		LEFT JOIN LATERAL unnest(effective_roles.permissions) permission ON true
		WHERE u.oidc_issuer=$1 AND u.oidc_subject=$2 AND u.tenant_id=$3
		  AND u.disabled_at IS NULL
		  AND EXISTS(SELECT 1 FROM applications a
		      WHERE a.id=$5 AND a.workspace_id=$4 AND a.tenant_id=$3)
		GROUP BY u.id`,
		claims.Issuer, claims.Subject, claims.TenantID, claims.WorkspaceID, claims.AppID).
		Scan(&principal.UserID, &principal.Scopes)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Principal{}, ErrUnauthorized
	}
	if err != nil {
		return domain.Principal{}, err
	}
	_, _ = s.pool.Exec(ctx, `UPDATE users SET email=NULLIF($1,''),display_name=NULLIF($2,''),updated_at=now()
		WHERE id=$3`, claims.Email, claims.Name, principal.UserID)
	return principal, nil
}

func (s *Store) CreateLocalSession(ctx context.Context, email, password string, ttl time.Duration) (domain.AuthSession, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || password == "" {
		return domain.AuthSession{}, ErrUnauthorized
	}
	var userID, tenantID, workspaceID, appID, passwordHash string
	var scopes []string
	err := s.pool.QueryRow(ctx, `SELECT u.id,u.tenant_id,a.workspace_id,a.id,u.password_hash,
		COALESCE(array_agg(DISTINCT permission) FILTER (WHERE permission IS NOT NULL),'{}')
		FROM users u
		JOIN applications a ON a.tenant_id=u.tenant_id
		JOIN LATERAL (
			SELECT r.permissions
			FROM role_bindings b JOIN roles r ON r.id=b.role_id AND r.tenant_id=u.tenant_id
			WHERE b.user_id=u.id AND b.tenant_id=u.tenant_id
			  AND (b.workspace_id IS NULL OR b.workspace_id=a.workspace_id)
			UNION ALL
			SELECT r.permissions
			FROM team_members tm
			JOIN teams t ON t.id=tm.team_id AND t.tenant_id=u.tenant_id AND t.workspace_id=a.workspace_id
			JOIN team_roles tr ON tr.team_id=t.id
			JOIN roles r ON r.id=tr.role_id AND r.tenant_id=u.tenant_id
			WHERE tm.user_id=u.id
		) effective_roles ON true
		LEFT JOIN LATERAL unnest(effective_roles.permissions) permission ON true
		WHERE lower(u.email)=lower($1) AND u.password_hash IS NOT NULL AND u.disabled_at IS NULL
		GROUP BY u.id,u.tenant_id,a.workspace_id,a.id,u.password_hash,u.created_at
		ORDER BY u.created_at LIMIT 1`, email).
		Scan(&userID, &tenantID, &workspaceID, &appID, &passwordHash, &scopes)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AuthSession{}, ErrUnauthorized
	}
	if err != nil {
		return domain.AuthSession{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) != nil {
		return domain.AuthSession{}, ErrUnauthorized
	}
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return domain.AuthSession{}, err
	}
	raw := "ojs_" + base64.RawURLEncoding.EncodeToString(random)
	hash := sha256.Sum256([]byte(raw))
	expiresAt := time.Now().UTC().Add(ttl)
	if _, err := s.pool.Exec(ctx, `INSERT INTO user_sessions
		(tenant_id,workspace_id,app_id,user_id,token_hash,expires_at,last_used_at)
		VALUES($1,$2,$3,$4,$5,$6,now())`, tenantID, workspaceID, appID, userID, hash[:], expiresAt); err != nil {
		return domain.AuthSession{}, err
	}
	_ = s.audit(ctx, domain.Principal{
		TenantID: tenantID, WorkspaceID: workspaceID, AppID: appID, UserID: userID, ActorType: "user", Scopes: scopes,
	}, "auth.login", "user_session", "", nil)
	return domain.AuthSession{AccessToken: raw, TokenType: "Bearer", ExpiresAt: expiresAt}, nil
}

func (s *Store) RevokeLocalSession(ctx context.Context, rawToken string) error {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil
	}
	hash := sha256.Sum256([]byte(rawToken))
	_, err := s.pool.Exec(ctx, `UPDATE user_sessions
		SET revoked_at=now()
		WHERE token_hash=$1 AND revoked_at IS NULL`, hash[:])
	return err
}

func hashPassword(password string) (string, error) {
	if len(password) < 12 {
		return "", errors.New("password must be at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
