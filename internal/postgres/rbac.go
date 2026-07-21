package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

var allowedPermissions = map[string]struct{}{
	"*": {}, "events:write": {}, "profiles:read": {}, "schemas:read": {}, "schemas:write": {},
	"api_keys:read": {}, "api_keys:write": {}, "privacy:write": {}, "operations:read": {}, "operations:write": {},
	"users:read": {}, "users:write": {}, "roles:read": {}, "roles:write": {},
	"segments:read": {}, "segments:write": {}, "templates:read": {}, "templates:write": {},
	"campaigns:read": {}, "campaigns:write": {}, "suppressions:read": {}, "suppressions:write": {},
	"journeys:read": {}, "journeys:write": {}, "journeys:publish": {},
	"experiments:read": {}, "experiments:write": {}, "reports:read": {}, "reports:write": {},
	"device_tokens:read": {}, "device_tokens:write": {},
	"ai:read": {}, "ai:configure": {}, "ai:invoke": {}, "prompts:read": {}, "prompts:write": {},
	"scoring:read": {}, "scoring:write": {}, "scoring:compute": {},
	"forms:read": {}, "forms:write": {}, "forms:publish": {},
	"pages:read": {}, "pages:write": {}, "pages:publish": {},
	"assets:read": {}, "assets:write": {}, "links:read": {}, "links:write": {},
	"companies:read": {}, "companies:write": {}, "stages:read": {}, "stages:write": {},
	"imports:read": {}, "imports:write": {},
	"extensions:read": {}, "extensions:write": {}, "extensions:install": {},
	"connectors:read": {}, "connectors:write": {}, "connectors:run": {},
	"messages:read": {}, "messages:write": {},
	"flags:read": {}, "flags:write": {},
	"catalogs:read": {}, "catalogs:write": {},
}

func (s *Store) ListRoles(ctx context.Context, p domain.Principal) ([]domain.Role, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,name,permissions,system,created_at
		FROM roles WHERE tenant_id=$1 ORDER BY name`, p.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Role
	for rows.Next() {
		var item domain.Role
		if err := rows.Scan(&item.ID, &item.Name, &item.Permissions, &item.System, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) CreateRole(ctx context.Context, p domain.Principal, name string, permissions []string) (domain.Role, error) {
	if name == "" || len(permissions) == 0 {
		return domain.Role{}, errors.New("name and permissions are required")
	}
	for _, permission := range permissions {
		if _, exists := allowedPermissions[permission]; !exists {
			return domain.Role{}, errors.New("unknown permission: " + permission)
		}
	}
	var role domain.Role
	err := s.pool.QueryRow(ctx, `INSERT INTO roles(tenant_id,name,permissions)
		VALUES($1,$2,$3) RETURNING id,name,permissions,system,created_at`,
		p.TenantID, name, permissions).
		Scan(&role.ID, &role.Name, &role.Permissions, &role.System, &role.CreatedAt)
	if err != nil {
		return domain.Role{}, err
	}
	_ = s.audit(ctx, p, "role.create", "role", role.ID, map[string]any{"permissions": permissions})
	return role, nil
}

func (s *Store) ListUsers(ctx context.Context, p domain.Principal) ([]domain.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT u.id,u.oidc_issuer,u.oidc_subject,COALESCE(u.email,''),
		COALESCE(u.display_name,''),(u.password_hash IS NOT NULL),u.created_at,
		COALESCE(array_agg(b.role_id::text) FILTER (WHERE b.role_id IS NOT NULL),'{}')
		FROM users u LEFT JOIN role_bindings b ON b.user_id=u.id
		WHERE u.tenant_id=$1 GROUP BY u.id ORDER BY u.created_at`, p.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.User
	for rows.Next() {
		var item domain.User
		if err := rows.Scan(&item.ID, &item.OIDCIssuer, &item.OIDCSubject, &item.Email,
			&item.DisplayName, &item.Local, &item.CreatedAt, &item.RoleIDs); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) CreateUser(ctx context.Context, p domain.Principal, input domain.User) (domain.User, error) {
	if len(input.RoleIDs) == 0 {
		return domain.User{}, errors.New("role_ids are required")
	}
	localUser := input.Password != ""
	if localUser {
		input.Email = strings.TrimSpace(strings.ToLower(input.Email))
		if input.Email == "" {
			return domain.User{}, errors.New("email is required for local users")
		}
		input.OIDCIssuer = "local"
		input.OIDCSubject = input.Email
	} else if input.OIDCIssuer == "" || input.OIDCSubject == "" {
		return domain.User{}, errors.New("oidc_issuer and oidc_subject are required for OIDC users")
	}
	passwordHash := ""
	if localUser {
		var err error
		passwordHash, err = hashPassword(input.Password)
		if err != nil {
			return domain.User{}, err
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `INSERT INTO users
		(tenant_id,oidc_issuer,oidc_subject,email,display_name,password_hash)
		VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''))
		RETURNING id,oidc_issuer,oidc_subject,COALESCE(email,''),COALESCE(display_name,''),(password_hash IS NOT NULL),created_at`,
		p.TenantID, input.OIDCIssuer, input.OIDCSubject, input.Email, input.DisplayName, passwordHash).
		Scan(&input.ID, &input.OIDCIssuer, &input.OIDCSubject, &input.Email, &input.DisplayName, &input.Local, &input.CreatedAt)
	if err != nil {
		return domain.User{}, err
	}
	for _, roleID := range input.RoleIDs {
		tag, err := tx.Exec(ctx, `INSERT INTO role_bindings(tenant_id,workspace_id,user_id,role_id)
			SELECT $1,$2,$3,id FROM roles WHERE id=$4 AND tenant_id=$1`,
			p.TenantID, p.WorkspaceID, input.ID, roleID)
		if err != nil {
			return domain.User{}, err
		}
		if tag.RowsAffected() != 1 {
			return domain.User{}, ErrNotFound
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, err
	}
	input.Password = ""
	_ = s.audit(ctx, p, "user.create", "user", input.ID, map[string]any{"roles": input.RoleIDs})
	return input, nil
}

func (s *Store) seedDevelopmentRole(ctx context.Context, tenantID string) error {
	permissions := []string{"*"}
	_, err := s.pool.Exec(ctx, `INSERT INTO roles(tenant_id,name,permissions,system)
		VALUES($1,'Administrator',$2,true) ON CONFLICT(tenant_id,name) DO NOTHING`,
		tenantID, permissions)
	return err
}

func (s *Store) EnsureLocalAdmin(ctx context.Context, email, password string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || password == "" {
		return errors.New("OPENJOURNEY_ADMIN_EMAIL and OPENJOURNEY_ADMIN_PASSWORD must be configured together")
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var tenantID, workspaceID, appID string
	err = tx.QueryRow(ctx, `SELECT t.id,w.id,a.id FROM tenants t
		JOIN workspaces w ON w.tenant_id=t.id
		JOIN applications a ON a.tenant_id=t.id AND a.workspace_id=w.id
		ORDER BY t.created_at,w.created_at,a.created_at LIMIT 1`).Scan(&tenantID, &workspaceID, &appID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.QueryRow(ctx, "INSERT INTO tenants(name) VALUES('Self-hosted') RETURNING id").Scan(&tenantID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "INSERT INTO workspaces(tenant_id,name) VALUES($1,'Default') RETURNING id", tenantID).Scan(&workspaceID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "INSERT INTO applications(tenant_id,workspace_id,name) VALUES($1,$2,'Control plane') RETURNING id", tenantID, workspaceID).Scan(&appID); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tenant_quotas(tenant_id) VALUES($1)
		ON CONFLICT(tenant_id) DO NOTHING`, tenantID); err != nil {
		return err
	}
	var roleID string
	if err := tx.QueryRow(ctx, `INSERT INTO roles(tenant_id,name,permissions,system)
		VALUES($1,'Administrator',$2,true)
		ON CONFLICT(tenant_id,name) DO UPDATE SET permissions=EXCLUDED.permissions
		RETURNING id`, tenantID, []string{"*"}).Scan(&roleID); err != nil {
		return err
	}
	var userID string
	err = tx.QueryRow(ctx, `INSERT INTO users
		(tenant_id,oidc_issuer,oidc_subject,email,display_name,password_hash)
		VALUES($1,'local',$2,$2,'Administrator',$3)
		ON CONFLICT(oidc_issuer,oidc_subject) DO UPDATE
		SET password_hash=EXCLUDED.password_hash,email=EXCLUDED.email,display_name=EXCLUDED.display_name,
		    disabled_at=NULL,updated_at=now()
		RETURNING id`, tenantID, email, passwordHash).Scan(&userID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO role_bindings(tenant_id,workspace_id,user_id,role_id)
		VALUES($1,$2,$3,$4) ON CONFLICT(workspace_id,user_id,role_id) DO NOTHING`,
		tenantID, workspaceID, userID, roleID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) roleExists(ctx context.Context, tenantID, roleID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM roles WHERE tenant_id=$1 AND id=$2)",
		tenantID, roleID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return exists, err
}
