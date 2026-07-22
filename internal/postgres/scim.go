package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) AuthenticateSCIM(ctx context.Context, rawToken string) (domain.Principal, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return domain.Principal{}, ErrUnauthorized
	}
	hash := sha256.Sum256([]byte(rawToken))
	var p domain.Principal
	err := s.pool.QueryRow(ctx, `UPDATE scim_tokens SET last_used_at=now()
		WHERE token_hash=$1 AND disabled_at IS NULL RETURNING tenant_id`, hex.EncodeToString(hash[:])).Scan(&p.TenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Principal{}, ErrUnauthorized
	}
	if err != nil {
		return domain.Principal{}, err
	}
	p.ActorType = "scim"
	return p, nil
}

func (s *Store) ListSCIMUsers(ctx context.Context, tenantID string) ([]domain.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,oidc_issuer,oidc_subject,COALESCE(email,''),COALESCE(display_name,''),
		(password_hash IS NOT NULL),created_at,disabled_at FROM users WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.OIDCIssuer, &u.OIDCSubject, &u.Email, &u.DisplayName, &u.Local, &u.CreatedAt, &u.DisabledAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) GetSCIMUser(ctx context.Context, tenantID, id string) (domain.User, error) {
	var u domain.User
	err := s.pool.QueryRow(ctx, `SELECT id,oidc_issuer,oidc_subject,COALESCE(email,''),COALESCE(display_name,''),
		(password_hash IS NOT NULL),created_at,disabled_at FROM users WHERE tenant_id=$1 AND id=$2`, tenantID, id).
		Scan(&u.ID, &u.OIDCIssuer, &u.OIDCSubject, &u.Email, &u.DisplayName, &u.Local, &u.CreatedAt, &u.DisabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) CreateSCIMUser(ctx context.Context, p domain.Principal, input domain.User, active bool) (domain.User, error) {
	input.OIDCIssuer = "scim"
	input.OIDCSubject = strings.TrimSpace(input.OIDCSubject)
	if input.OIDCSubject == "" {
		return domain.User{}, errors.New("userName is required")
	}
	err := s.pool.QueryRow(ctx, `INSERT INTO users(tenant_id,oidc_issuer,oidc_subject,email,display_name,disabled_at)
		VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),CASE WHEN $6 THEN NULL ELSE now() END)
		RETURNING id,oidc_issuer,oidc_subject,COALESCE(email,''),COALESCE(display_name,''),false,created_at,disabled_at`,
		p.TenantID, input.OIDCIssuer, input.OIDCSubject, input.Email, input.DisplayName, active).
		Scan(&input.ID, &input.OIDCIssuer, &input.OIDCSubject, &input.Email, &input.DisplayName, &input.Local, &input.CreatedAt, &input.DisabledAt)
	if err != nil {
		return domain.User{}, err
	}
	_ = s.audit(ctx, p, "scim.user.create", "user", input.ID, nil)
	return input, nil
}

func (s *Store) UpdateSCIMUser(ctx context.Context, p domain.Principal, id string, input domain.User, active bool) (domain.User, error) {
	var u domain.User
	err := s.pool.QueryRow(ctx, `UPDATE users SET oidc_subject=COALESCE(NULLIF($1,''),oidc_subject),email=NULLIF($2,''),
		display_name=NULLIF($3,''),disabled_at=CASE WHEN $4 THEN NULL ELSE COALESCE(disabled_at,now()) END,updated_at=now()
		WHERE tenant_id=$5 AND id=$6 RETURNING id,oidc_issuer,oidc_subject,COALESCE(email,''),COALESCE(display_name,''),false,created_at,disabled_at`,
		input.OIDCSubject, input.Email, input.DisplayName, active, p.TenantID, id).Scan(&u.ID, &u.OIDCIssuer, &u.OIDCSubject, &u.Email, &u.DisplayName, &u.Local, &u.CreatedAt, &u.DisabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, ErrNotFound
	}
	if err == nil {
		_ = s.audit(ctx, p, "scim.user.update", "user", id, nil)
	}
	return u, err
}
