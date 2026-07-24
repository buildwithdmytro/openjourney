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
	var wsID string
	_ = s.pool.QueryRow(ctx, `SELECT id FROM workspaces WHERE tenant_id=$1 ORDER BY created_at LIMIT 1`, p.TenantID).Scan(&wsID)
	p.WorkspaceID = wsID
	return p, nil
}

func (s *Store) ensureWorkspaceID(ctx context.Context, p domain.Principal) string {
	if p.WorkspaceID != "" {
		return p.WorkspaceID
	}
	var wsID string
	_ = s.pool.QueryRow(ctx, `SELECT id FROM workspaces WHERE tenant_id=$1 ORDER BY created_at LIMIT 1`, p.TenantID).Scan(&wsID)
	if wsID == "" {
		_ = s.pool.QueryRow(ctx, `INSERT INTO workspaces(tenant_id, name) VALUES($1, 'Default') RETURNING id`, p.TenantID).Scan(&wsID)
	}
	return wsID
}

func (s *Store) fetchSCIMGroupMembers(ctx context.Context, tenantID, teamID string) ([]domain.SCIMGroupMember, error) {
	rows, err := s.pool.Query(ctx, `SELECT u.id, COALESCE(u.display_name, u.email, u.oidc_subject)
		FROM team_members tm
		JOIN users u ON u.id = tm.user_id AND u.tenant_id = $1
		WHERE tm.team_id = $2 ORDER BY u.id`, tenantID, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []domain.SCIMGroupMember
	for rows.Next() {
		var m domain.SCIMGroupMember
		if err := rows.Scan(&m.Value, &m.Display); err != nil {
			return nil, err
		}
		m.Ref = "/v1/scim/v2/Users/" + m.Value
		members = append(members, m)
	}
	if members == nil {
		members = []domain.SCIMGroupMember{}
	}
	return members, rows.Err()
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
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback(ctx)

	input.OIDCIssuer = "scim"
	input.OIDCSubject = strings.TrimSpace(input.OIDCSubject)
	if input.OIDCSubject == "" {
		return domain.User{}, errors.New("userName is required")
	}
	err = tx.QueryRow(ctx, `INSERT INTO users(tenant_id,oidc_issuer,oidc_subject,email,display_name,disabled_at)
		VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),CASE WHEN $6 THEN NULL ELSE now() END)
		RETURNING id,oidc_issuer,oidc_subject,COALESCE(email,''),COALESCE(display_name,''),false,created_at,disabled_at`,
		p.TenantID, input.OIDCIssuer, input.OIDCSubject, input.Email, input.DisplayName, active).
		Scan(&input.ID, &input.OIDCIssuer, &input.OIDCSubject, &input.Email, &input.DisplayName, &input.Local, &input.CreatedAt, &input.DisabledAt)
	if err != nil {
		return domain.User{}, err
	}
	if err := s.audit(ctx, tx, p, "scim.user.create", "user", input.ID, nil); err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, err
	}
	return input, nil
}

func (s *Store) UpdateSCIMUser(ctx context.Context, p domain.Principal, id string, input domain.User, active bool) (domain.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback(ctx)

	var u domain.User
	err = tx.QueryRow(ctx, `UPDATE users SET oidc_subject=COALESCE(NULLIF($1,''),oidc_subject),email=NULLIF($2,''),
		display_name=NULLIF($3,''),disabled_at=CASE WHEN $4 THEN NULL ELSE COALESCE(disabled_at,now()) END,updated_at=now()
		WHERE tenant_id=$5 AND id=$6 RETURNING id,oidc_issuer,oidc_subject,COALESCE(email,''),COALESCE(display_name,''),false,created_at,disabled_at`,
		input.OIDCSubject, input.Email, input.DisplayName, active, p.TenantID, id).Scan(&u.ID, &u.OIDCIssuer, &u.OIDCSubject, &u.Email, &u.DisplayName, &u.Local, &u.CreatedAt, &u.DisabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, ErrNotFound
	}
	if err == nil {
		if err := s.audit(ctx, tx, p, "scim.user.update", "user", id, nil); err != nil {
			return domain.User{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.User{}, err
		}
	}
	return u, err
}

func (s *Store) ListSCIMGroups(ctx context.Context, tenantID string) ([]domain.SCIMGroup, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name FROM teams WHERE tenant_id = $1 ORDER BY name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []domain.SCIMGroup
	for rows.Next() {
		var g domain.SCIMGroup
		if err := rows.Scan(&g.ID, &g.DisplayName); err != nil {
			return nil, err
		}
		members, err := s.fetchSCIMGroupMembers(ctx, tenantID, g.ID)
		if err != nil {
			return nil, err
		}
		g.Members = members
		groups = append(groups, g)
	}
	if groups == nil {
		groups = []domain.SCIMGroup{}
	}
	return groups, rows.Err()
}

func (s *Store) GetSCIMGroup(ctx context.Context, tenantID, id string) (domain.SCIMGroup, error) {
	var g domain.SCIMGroup
	err := s.pool.QueryRow(ctx, `SELECT t.id, t.name
		FROM teams t
		LEFT JOIN scim_group_mappings m ON m.team_id = t.id AND m.tenant_id = t.tenant_id
		WHERE t.tenant_id = $1 AND (t.id = $2 OR m.external_group = $2 OR m.id = $2)`, tenantID, id).
		Scan(&g.ID, &g.DisplayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SCIMGroup{}, ErrNotFound
	}
	if err != nil {
		return domain.SCIMGroup{}, err
	}
	members, err := s.fetchSCIMGroupMembers(ctx, tenantID, g.ID)
	if err != nil {
		return domain.SCIMGroup{}, err
	}
	g.Members = members
	return g, nil
}

func (s *Store) CreateSCIMGroup(ctx context.Context, p domain.Principal, group domain.SCIMGroup) (domain.SCIMGroup, error) {
	group.DisplayName = strings.TrimSpace(group.DisplayName)
	if group.DisplayName == "" {
		return domain.SCIMGroup{}, errors.New("displayName is required")
	}
	wsID := s.ensureWorkspaceID(ctx, p)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.SCIMGroup{}, err
	}
	defer tx.Rollback(ctx)

	var teamID string
	err = tx.QueryRow(ctx, `SELECT id FROM teams WHERE tenant_id = $1 AND workspace_id = $2 AND name = $3`,
		p.TenantID, wsID, group.DisplayName).Scan(&teamID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO teams (tenant_id, workspace_id, name, description)
			VALUES ($1, $2, $3, 'SCIM Group') RETURNING id`, p.TenantID, wsID, group.DisplayName).Scan(&teamID)
	}
	if err != nil {
		return domain.SCIMGroup{}, err
	}

	_, _ = tx.Exec(ctx, `INSERT INTO scim_group_mappings (tenant_id, external_group, team_id)
		VALUES ($1, $2, $3) ON CONFLICT (tenant_id, external_group) DO UPDATE SET team_id = EXCLUDED.team_id`,
		p.TenantID, group.DisplayName, teamID)

	for _, m := range group.Members {
		if strings.TrimSpace(m.Value) != "" {
			_, _ = tx.Exec(ctx, `INSERT INTO team_members (team_id, user_id)
				SELECT $1, id FROM users WHERE id = $2 AND tenant_id = $3 ON CONFLICT DO NOTHING`,
				teamID, m.Value, p.TenantID)
		}
	}

	if err := s.audit(ctx, tx, p, "scim.group.create", "team", teamID, map[string]any{"displayName": group.DisplayName}); err != nil {
		return domain.SCIMGroup{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SCIMGroup{}, err
	}
	return s.GetSCIMGroup(ctx, p.TenantID, teamID)
}

func (s *Store) UpdateSCIMGroup(ctx context.Context, p domain.Principal, id string, group domain.SCIMGroup) (domain.SCIMGroup, error) {
	existing, err := s.GetSCIMGroup(ctx, p.TenantID, id)
	if err != nil {
		return domain.SCIMGroup{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.SCIMGroup{}, err
	}
	defer tx.Rollback(ctx)

	if group.DisplayName != "" {
		_, err = tx.Exec(ctx, `UPDATE teams SET name = $1, updated_at = now() WHERE tenant_id = $2 AND id = $3`,
			group.DisplayName, p.TenantID, existing.ID)
		if err != nil {
			return domain.SCIMGroup{}, err
		}
	}

	_, err = tx.Exec(ctx, `DELETE FROM team_members tm USING teams t
		WHERE tm.team_id = t.id AND t.tenant_id = $1 AND tm.team_id = $2`, p.TenantID, existing.ID)
	if err != nil {
		return domain.SCIMGroup{}, err
	}

	for _, m := range group.Members {
		if strings.TrimSpace(m.Value) != "" {
			_, _ = tx.Exec(ctx, `INSERT INTO team_members (team_id, user_id)
				SELECT $1, id FROM users WHERE id = $2 AND tenant_id = $3 ON CONFLICT DO NOTHING`,
				existing.ID, m.Value, p.TenantID)
		}
	}

	if err := s.audit(ctx, tx, p, "scim.group.update", "team", existing.ID, nil); err != nil {
		return domain.SCIMGroup{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SCIMGroup{}, err
	}
	return s.GetSCIMGroup(ctx, p.TenantID, existing.ID)
}

func (s *Store) PatchSCIMGroup(ctx context.Context, p domain.Principal, id string, patch domain.SCIMGroupPatch) (domain.SCIMGroup, error) {
	existing, err := s.GetSCIMGroup(ctx, p.TenantID, id)
	if err != nil {
		return domain.SCIMGroup{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.SCIMGroup{}, err
	}
	defer tx.Rollback(ctx)

	for _, op := range patch.Operations {
		opName := strings.ToLower(strings.TrimSpace(op.Op))
		switch opName {
		case "add":
			for _, m := range op.Value {
				if strings.TrimSpace(m.Value) != "" {
					_, _ = tx.Exec(ctx, `INSERT INTO team_members (team_id, user_id)
						SELECT $1, id FROM users WHERE id = $2 AND tenant_id = $3 ON CONFLICT DO NOTHING`,
						existing.ID, m.Value, p.TenantID)
				}
			}
		case "remove":
			targetUserIDs := make([]string, 0)
			for _, m := range op.Value {
				if strings.TrimSpace(m.Value) != "" {
					targetUserIDs = append(targetUserIDs, m.Value)
				}
			}
			if len(targetUserIDs) == 0 && strings.Contains(op.Path, "value eq") {
				idx := strings.Index(op.Path, "value eq")
				sub := strings.TrimSpace(op.Path[idx+8:])
				sub = strings.Trim(sub, `[]"'`)
				if sub != "" {
					targetUserIDs = append(targetUserIDs, sub)
				}
			}
			for _, uid := range targetUserIDs {
				_, _ = tx.Exec(ctx, `DELETE FROM team_members tm USING teams t
					WHERE tm.team_id = t.id AND t.tenant_id = $1 AND tm.team_id = $2 AND tm.user_id = $3`, p.TenantID, existing.ID, uid)
			}
		case "replace":
			_, _ = tx.Exec(ctx, `DELETE FROM team_members tm USING teams t
				WHERE tm.team_id = t.id AND t.tenant_id = $1 AND tm.team_id = $2`, p.TenantID, existing.ID)
			for _, m := range op.Value {
				if strings.TrimSpace(m.Value) != "" {
					_, _ = tx.Exec(ctx, `INSERT INTO team_members (team_id, user_id)
						SELECT $1, id FROM users WHERE id = $2 AND tenant_id = $3 ON CONFLICT DO NOTHING`,
						existing.ID, m.Value, p.TenantID)
				}
			}
		}
	}

	if err := s.audit(ctx, tx, p, "scim.group.patch", "team", existing.ID, nil); err != nil {
		return domain.SCIMGroup{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SCIMGroup{}, err
	}
	return s.GetSCIMGroup(ctx, p.TenantID, existing.ID)
}

func (s *Store) DeleteSCIMGroup(ctx context.Context, p domain.Principal, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	existing, err := s.GetSCIMGroup(ctx, p.TenantID, id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM teams WHERE tenant_id = $1 AND id = $2`, p.TenantID, existing.ID)
	if err != nil {
		return err
	}
	if err := s.audit(ctx, tx, p, "scim.group.delete", "team", existing.ID, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}
