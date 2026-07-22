package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateTeam(ctx context.Context, p domain.Principal, input domain.Team) (domain.Team, error) {
	if strings.TrimSpace(input.Name) == "" {
		return domain.Team{}, errors.New("team name is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Team{}, err
	}
	defer tx.Rollback(ctx)

	var team domain.Team
	err = tx.QueryRow(ctx, `INSERT INTO teams (tenant_id, workspace_id, name, description)
		VALUES ($1, $2, $3, $4)
		RETURNING id, tenant_id, workspace_id, name, description, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, strings.TrimSpace(input.Name), input.Description,
	).Scan(&team.ID, &team.TenantID, &team.WorkspaceID, &team.Name, &team.Description, &team.CreatedAt, &team.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "unique constraint") || strings.Contains(err.Error(), "teams_tenant_id_workspace_id_name_key") {
			return domain.Team{}, errors.New("team with this name already exists in this workspace")
		}
		return domain.Team{}, err
	}

	if len(input.MemberIDs) > 0 {
		for _, uid := range input.MemberIDs {
			if strings.TrimSpace(uid) == "" {
				continue
			}
			result, err := tx.Exec(ctx, `INSERT INTO team_members (team_id, user_id)
				SELECT $1, id FROM users WHERE id=$2 AND tenant_id=$3 ON CONFLICT DO NOTHING`, team.ID, uid, p.TenantID)
			if err != nil {
				return domain.Team{}, err
			}
			if result.RowsAffected() == 0 {
				return domain.Team{}, errors.New("team member is not in the tenant")
			}
			team.MemberIDs = append(team.MemberIDs, uid)
		}
	}
	if team.MemberIDs == nil {
		team.MemberIDs = []string{}
	}

	if len(input.RoleIDs) > 0 {
		for _, rid := range input.RoleIDs {
			if strings.TrimSpace(rid) == "" {
				continue
			}
			result, err := tx.Exec(ctx, `INSERT INTO team_roles (team_id, role_id)
				SELECT $1, id FROM roles WHERE id=$2 AND tenant_id=$3 ON CONFLICT DO NOTHING`, team.ID, rid, p.TenantID)
			if err != nil {
				return domain.Team{}, err
			}
			if result.RowsAffected() == 0 {
				return domain.Team{}, errors.New("team role is not in the tenant")
			}
			team.RoleIDs = append(team.RoleIDs, rid)
		}
	}
	if team.RoleIDs == nil {
		team.RoleIDs = []string{}
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Team{}, err
	}

	_ = s.audit(ctx, p, "team.create", "team", team.ID, map[string]any{"name": team.Name})
	return team, nil
}

func (s *Store) GetTeam(ctx context.Context, p domain.Principal, id string) (domain.Team, error) {
	var team domain.Team
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, name, description, created_at, updated_at
		FROM teams WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, id,
	).Scan(&team.ID, &team.TenantID, &team.WorkspaceID, &team.Name, &team.Description, &team.CreatedAt, &team.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Team{}, ports.ErrNotFound
		}
		return domain.Team{}, err
	}

	members, err := s.fetchTeamMembers(ctx, team.ID)
	if err != nil {
		return domain.Team{}, err
	}
	team.MemberIDs = members

	roles, err := s.fetchTeamRoles(ctx, team.ID)
	if err != nil {
		return domain.Team{}, err
	}
	team.RoleIDs = roles

	return team, nil
}

func (s *Store) UpdateTeam(ctx context.Context, p domain.Principal, input domain.Team) (domain.Team, error) {
	if strings.TrimSpace(input.Name) == "" {
		return domain.Team{}, errors.New("team name is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Team{}, err
	}
	defer tx.Rollback(ctx)

	var team domain.Team
	err = tx.QueryRow(ctx, `UPDATE teams SET name=$1, description=$2, updated_at=now()
		WHERE tenant_id=$3 AND workspace_id=$4 AND id=$5
		RETURNING id, tenant_id, workspace_id, name, description, created_at, updated_at`,
		strings.TrimSpace(input.Name), input.Description, p.TenantID, p.WorkspaceID, input.ID,
	).Scan(&team.ID, &team.TenantID, &team.WorkspaceID, &team.Name, &team.Description, &team.CreatedAt, &team.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Team{}, ports.ErrNotFound
		}
		if strings.Contains(err.Error(), "unique constraint") || strings.Contains(err.Error(), "teams_tenant_id_workspace_id_name_key") {
			return domain.Team{}, errors.New("team with this name already exists in this workspace")
		}
		return domain.Team{}, err
	}

	_, err = tx.Exec(ctx, `DELETE FROM team_members WHERE team_id=$1`, team.ID)
	if err != nil {
		return domain.Team{}, err
	}
	team.MemberIDs = []string{}
	for _, uid := range input.MemberIDs {
		if strings.TrimSpace(uid) == "" {
			continue
		}
		result, err := tx.Exec(ctx, `INSERT INTO team_members (team_id, user_id)
			SELECT $1, id FROM users WHERE id=$2 AND tenant_id=$3 ON CONFLICT DO NOTHING`, team.ID, uid, p.TenantID)
		if err != nil {
			return domain.Team{}, err
		}
		if result.RowsAffected() == 0 {
			return domain.Team{}, errors.New("team member is not in the tenant")
		}
		team.MemberIDs = append(team.MemberIDs, uid)
	}

	_, err = tx.Exec(ctx, `DELETE FROM team_roles WHERE team_id=$1`, team.ID)
	if err != nil {
		return domain.Team{}, err
	}
	team.RoleIDs = []string{}
	for _, rid := range input.RoleIDs {
		if strings.TrimSpace(rid) == "" {
			continue
		}
		result, err := tx.Exec(ctx, `INSERT INTO team_roles (team_id, role_id)
			SELECT $1, id FROM roles WHERE id=$2 AND tenant_id=$3 ON CONFLICT DO NOTHING`, team.ID, rid, p.TenantID)
		if err != nil {
			return domain.Team{}, err
		}
		if result.RowsAffected() == 0 {
			return domain.Team{}, errors.New("team role is not in the tenant")
		}
		team.RoleIDs = append(team.RoleIDs, rid)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Team{}, err
	}

	_ = s.audit(ctx, p, "team.update", "team", team.ID, map[string]any{"name": team.Name})
	return team, nil
}

func (s *Store) DeleteTeam(ctx context.Context, p domain.Principal, id string) error {
	res, err := s.pool.Exec(ctx, `DELETE FROM teams WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, id,
	)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ports.ErrNotFound
	}

	_ = s.audit(ctx, p, "team.delete", "team", id, nil)
	return nil
}

func (s *Store) ListTeams(ctx context.Context, p domain.Principal) ([]domain.Team, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, workspace_id, name, description, created_at, updated_at
		FROM teams WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY name`,
		p.TenantID, p.WorkspaceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var teams []domain.Team
	for rows.Next() {
		var t domain.Team
		if err := rows.Scan(&t.ID, &t.TenantID, &t.WorkspaceID, &t.Name, &t.Description, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.MemberIDs = []string{}
		t.RoleIDs = []string{}
		teams = append(teams, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range teams {
		members, err := s.fetchTeamMembers(ctx, teams[i].ID)
		if err != nil {
			return nil, err
		}
		teams[i].MemberIDs = members

		roles, err := s.fetchTeamRoles(ctx, teams[i].ID)
		if err != nil {
			return nil, err
		}
		teams[i].RoleIDs = roles
	}

	if teams == nil {
		teams = []domain.Team{}
	}
	return teams, nil
}

func (s *Store) fetchTeamMembers(ctx context.Context, teamID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT user_id FROM team_members WHERE team_id=$1 ORDER BY user_id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memberIDs []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		memberIDs = append(memberIDs, uid)
	}
	if memberIDs == nil {
		memberIDs = []string{}
	}
	return memberIDs, rows.Err()
}

func (s *Store) fetchTeamRoles(ctx context.Context, teamID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT role_id FROM team_roles WHERE team_id=$1 ORDER BY role_id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roleIDs []string
	for rows.Next() {
		var rid string
		if err := rows.Scan(&rid); err != nil {
			return nil, err
		}
		roleIDs = append(roleIDs, rid)
	}
	if roleIDs == nil {
		roleIDs = []string{}
	}
	return roleIDs, rows.Err()
}
