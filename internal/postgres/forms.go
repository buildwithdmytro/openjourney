package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

func scanForm(row pgx.Row, out *domain.Form) error {
	return row.Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Status, &out.Draft,
		&out.CurrentVersionID, &out.LatestVersion, &out.CreatedAt, &out.UpdatedAt)
}

const formColumns = `id, tenant_id, workspace_id, name, status, draft, current_version_id, latest_version, created_at, updated_at`

func (s *Store) CreateForm(ctx context.Context, p domain.Principal, form domain.Form) (domain.Form, error) {
	if form.Name == "" {
		return domain.Form{}, errors.New("form name is required")
	}
	if len(form.Draft) == 0 {
		form.Draft = json.RawMessage(`{"fields":[]}`)
	}
	var out domain.Form
	err := scanForm(s.pool.QueryRow(ctx, `INSERT INTO forms (tenant_id, workspace_id, name, draft)
		VALUES ($1, $2, $3, $4) RETURNING `+formColumns, p.TenantID, p.WorkspaceID, form.Name, form.Draft), &out)
	if err != nil {
		return domain.Form{}, err
	}
	_ = s.audit(ctx, p, "form.create", "form", out.ID, map[string]any{"name": out.Name})
	return out, nil
}

func (s *Store) GetForm(ctx context.Context, p domain.Principal, id string) (domain.Form, error) {
	var out domain.Form
	err := scanForm(s.pool.QueryRow(ctx, `SELECT `+formColumns+` FROM forms WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, p.TenantID, p.WorkspaceID, id), &out)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Form{}, ErrNotFound
	}
	return out, err
}

func (s *Store) UpdateForm(ctx context.Context, p domain.Principal, form domain.Form) (domain.Form, error) {
	if form.Name == "" || len(form.Draft) == 0 {
		return domain.Form{}, errors.New("form name and draft are required")
	}
	var out domain.Form
	err := scanForm(s.pool.QueryRow(ctx, `UPDATE forms SET name=$4, draft=$5, updated_at=now()
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3 RETURNING `+formColumns,
		p.TenantID, p.WorkspaceID, form.ID, form.Name, form.Draft), &out)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Form{}, ErrNotFound
	}
	return out, err
}

func (s *Store) ListForms(ctx context.Context, p domain.Principal) ([]domain.Form, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+formColumns+` FROM forms WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY created_at DESC`, p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Form
	for rows.Next() {
		var item domain.Form
		if err := rows.Scan(&item.ID, &item.TenantID, &item.WorkspaceID, &item.Name, &item.Status, &item.Draft, &item.CurrentVersionID, &item.LatestVersion, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) PublishForm(ctx context.Context, p domain.Principal, id, publishedBy, manifestKey string, definition json.RawMessage) (domain.FormVersion, error) {
	if publishedBy == "" || manifestKey == "" || len(definition) == 0 {
		return domain.FormVersion{}, errors.New("publisher, manifest, and definition are required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.FormVersion{}, err
	}
	defer tx.Rollback(ctx)
	var tenant, name string
	var latest int
	var current *string
	err = tx.QueryRow(ctx, `SELECT tenant_id, name, latest_version, current_version_id FROM forms WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`, p.TenantID, p.WorkspaceID, id).Scan(&tenant, &name, &latest, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FormVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.FormVersion{}, err
	}
	if current != nil && *current != "" {
		var out domain.FormVersion
		err = tx.QueryRow(ctx, `SELECT id, form_id, tenant_id, version, definition, manifest_key, published_by, published_at FROM form_versions WHERE id=$1`, *current).Scan(&out.ID, &out.FormID, &out.TenantID, &out.Version, &out.Definition, &out.ManifestKey, &out.PublishedBy, &out.PublishedAt)
		if err != nil {
			return domain.FormVersion{}, err
		}
		if out.ManifestKey == manifestKey {
			return out, nil
		}
	}
	var out domain.FormVersion
	err = tx.QueryRow(ctx, `INSERT INTO form_versions (form_id, tenant_id, version, definition, manifest_key, published_by)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, form_id, tenant_id, version, definition, manifest_key, published_by, published_at`, id, tenant, latest+1, definition, manifestKey, publishedBy).
		Scan(&out.ID, &out.FormID, &out.TenantID, &out.Version, &out.Definition, &out.ManifestKey, &out.PublishedBy, &out.PublishedAt)
	if err != nil {
		return domain.FormVersion{}, fmt.Errorf("insert form version: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE forms SET status='published', latest_version=$1, current_version_id=$2, updated_at=now() WHERE tenant_id=$3 AND workspace_id=$4 AND id=$5`, out.Version, out.ID, p.TenantID, p.WorkspaceID, id)
	if err != nil {
		return domain.FormVersion{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.FormVersion{}, err
	}
	return out, nil
}

// GetPublishedForm is intentionally not tenant-scoped: the public form ID is
// the lookup key. It returns only the immutable version pinned by the form.
func (s *Store) GetPublishedForm(ctx context.Context, id string) (domain.Form, domain.FormVersion, error) {
	var form domain.Form
	var version domain.FormVersion
	err := scanForm(s.pool.QueryRow(ctx, `SELECT `+formColumns+` FROM forms WHERE id=$1 AND status='published'`, id), &form)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Form{}, domain.FormVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.Form{}, domain.FormVersion{}, err
	}
	if form.CurrentVersionID == nil {
		return domain.Form{}, domain.FormVersion{}, ErrNotFound
	}
	err = s.pool.QueryRow(ctx, `SELECT id, form_id, tenant_id, version, definition, manifest_key, published_by, published_at
		FROM form_versions WHERE id=$1 AND form_id=$2`, *form.CurrentVersionID, form.ID).
		Scan(&version.ID, &version.FormID, &version.TenantID, &version.Version, &version.Definition,
			&version.ManifestKey, &version.PublishedBy, &version.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Form{}, domain.FormVersion{}, ErrNotFound
	}
	return form, version, err
}

func (s *Store) RecordFormSubmission(ctx context.Context, p domain.Principal, formID string, version int,
	payload, utm json.RawMessage, sourceEventID string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO form_submissions
		(tenant_id,workspace_id,app_id,form_id,form_version,payload,utm,source_event_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (tenant_id,source_event_id) DO NOTHING`,
		p.TenantID, p.WorkspaceID, p.AppID, formID, version, payload, utm, sourceEventID)
	return err
}
