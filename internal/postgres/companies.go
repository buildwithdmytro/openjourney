package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

func companyEvent(p domain.Principal, c domain.Company, members []domain.CompanyMember) domain.Event {
	externalID := c.ExternalID
	if externalID == "" {
		sum := sha256.Sum256([]byte(c.Name))
		externalID = "name-" + hex.EncodeToString(sum[:8])
	}
	items := make([]map[string]any, 0, len(members))
	for _, m := range members {
		items = append(items, map[string]any{"profile_external_id": m.ProfileID, "role": m.Role})
	}
	payload, _ := json.Marshal(map[string]any{"company": map[string]any{
		"external_id": externalID, "name": c.Name, "attributes": c.Attributes,
	}, "members": items})
	return domain.Event{Type: "company.updated", SchemaVersion: 1, ExternalID: externalID,
		IdempotencyKey: "company.updated:" + externalID, OccurredAt: time.Now().UTC(), Source: "company-api", Payload: payload}
}

func (s *Store) applyCompanyEvent(ctx context.Context, p domain.Principal, c domain.Company, members []domain.CompanyMember) (domain.Company, error) {
	resolved := make([]domain.CompanyMember, 0, len(members))
	for _, member := range members {
		profile, err := s.GetProfileByIDSystem(ctx, p.TenantID, p.WorkspaceID, member.ProfileID)
		if err != nil {
			return domain.Company{}, err
		}
		if profile.ExternalID == "" {
			return domain.Company{}, errors.New("company members require profiles with external_id")
		}
		member.ProfileID = profile.ExternalID
		resolved = append(resolved, member)
	}
	event := companyEvent(p, c, resolved)
	ids, err := s.AcceptEvents(ctx, p, []domain.Event{event})
	if err != nil {
		return domain.Company{}, err
	}
	accepted := domain.AcceptedEvent{ID: ids[0], Principal: p, Type: event.Type, SchemaVersion: event.SchemaVersion, ExternalID: event.ExternalID, IdempotencyKey: event.IdempotencyKey, OccurredAt: event.OccurredAt, Payload: event.Payload}
	if err := s.ProjectEvent(ctx, accepted); err != nil {
		return domain.Company{}, err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE projection_jobs SET status='done', locked_until=NULL, last_error=NULL WHERE event_id=$1`, ids[0]); err != nil {
		return domain.Company{}, err
	}
	return s.getCompanyByExternal(ctx, p, event.ExternalID)
}

func (s *Store) CreateCompany(ctx context.Context, p domain.Principal, c domain.Company, members []domain.CompanyMember) (domain.Company, error) {
	if c.Name == "" {
		return domain.Company{}, errors.New("name is required")
	}
	return s.applyCompanyEvent(ctx, p, c, members)
}

func (s *Store) UpdateCompany(ctx context.Context, p domain.Principal, c domain.Company, members []domain.CompanyMember) (domain.Company, error) {
	if c.ExternalID == "" {
		return domain.Company{}, errors.New("external_id is required")
	}
	return s.applyCompanyEvent(ctx, p, c, members)
}

func (s *Store) getCompanyByExternal(ctx context.Context, p domain.Principal, externalID string) (domain.Company, error) {
	var c domain.Company
	err := s.pool.QueryRow(ctx, `SELECT id,tenant_id,workspace_id,app_id,COALESCE(external_id,''),name,attributes,version,created_at,updated_at FROM companies WHERE tenant_id=$1 AND workspace_id=$2 AND app_id=$3 AND external_id=$4`, p.TenantID, p.WorkspaceID, p.AppID, externalID).
		Scan(&c.ID, &c.TenantID, &c.WorkspaceID, &c.AppID, &c.ExternalID, &c.Name, &c.Attributes, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Company{}, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	return s.loadCompanyMembers(ctx, c)
}

func (s *Store) GetCompany(ctx context.Context, p domain.Principal, id string) (domain.Company, error) {
	var c domain.Company
	err := s.pool.QueryRow(ctx, `SELECT id,tenant_id,workspace_id,app_id,COALESCE(external_id,''),name,attributes,version,created_at,updated_at FROM companies WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, p.TenantID, p.WorkspaceID, id).
		Scan(&c.ID, &c.TenantID, &c.WorkspaceID, &c.AppID, &c.ExternalID, &c.Name, &c.Attributes, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Company{}, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	return s.loadCompanyMembers(ctx, c)
}

func (s *Store) ListCompanies(ctx context.Context, p domain.Principal) ([]domain.Company, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,tenant_id,workspace_id,app_id,COALESCE(external_id,''),name,attributes,version,created_at,updated_at FROM companies WHERE tenant_id=$1 AND workspace_id=$2 AND app_id=$3 ORDER BY name`, p.TenantID, p.WorkspaceID, p.AppID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Company
	for rows.Next() {
		var c domain.Company
		if err := rows.Scan(&c.ID, &c.TenantID, &c.WorkspaceID, &c.AppID, &c.ExternalID, &c.Name, &c.Attributes, &c.Version, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		loaded, loadErr := s.loadCompanyMembers(ctx, c)
		if loadErr != nil {
			return nil, loadErr
		}
		out = append(out, loaded)
	}
	return out, rows.Err()
}

func (s *Store) loadCompanyMembers(ctx context.Context, c domain.Company) (domain.Company, error) {
	rows, err := s.pool.Query(ctx, `SELECT m.company_id,m.profile_id,m.tenant_id,COALESCE(m.role,''),m.created_at FROM company_members m WHERE m.tenant_id=$1 AND m.company_id=$2 ORDER BY m.created_at`, c.TenantID, c.ID)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var m domain.CompanyMember
		if err := rows.Scan(&m.CompanyID, &m.ProfileID, &m.TenantID, &m.Role, &m.CreatedAt); err != nil {
			return c, err
		}
		c.Members = append(c.Members, m)
	}
	return c, rows.Err()
}
