package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

const pageColumns = `id, tenant_id, workspace_id, slug, name, status, draft, current_version_id, latest_version, created_at, updated_at`

func scanPage(row pgx.Row, out *domain.LandingPage) error {
	return row.Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Slug, &out.Name, &out.Status, &out.Draft,
		&out.CurrentVersionID, &out.LatestVersion, &out.CreatedAt, &out.UpdatedAt)
}

func (s *Store) CreateLandingPage(ctx context.Context, p domain.Principal, page domain.LandingPage) (domain.LandingPage, error) {
	if page.Name == "" || page.Slug == "" {
		return domain.LandingPage{}, errors.New("page name and slug are required")
	}
	if len(page.Draft) == 0 {
		page.Draft = json.RawMessage(`{"template":"","meta":{}}`)
	}
	var out domain.LandingPage
	err := scanPage(s.pool.QueryRow(ctx, `INSERT INTO landing_pages (tenant_id,workspace_id,slug,name,draft) VALUES ($1,$2,$3,$4,$5) RETURNING `+pageColumns,
		p.TenantID, p.WorkspaceID, page.Slug, page.Name, page.Draft), &out)
	return out, err
}

func (s *Store) GetLandingPage(ctx context.Context, p domain.Principal, id string) (domain.LandingPage, error) {
	var out domain.LandingPage
	err := scanPage(s.pool.QueryRow(ctx, `SELECT `+pageColumns+` FROM landing_pages WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, p.TenantID, p.WorkspaceID, id), &out)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LandingPage{}, ErrNotFound
	}
	return out, err
}

func (s *Store) UpdateLandingPage(ctx context.Context, p domain.Principal, page domain.LandingPage) (domain.LandingPage, error) {
	if page.Name == "" || page.Slug == "" || len(page.Draft) == 0 {
		return domain.LandingPage{}, errors.New("page name, slug, and draft are required")
	}
	var out domain.LandingPage
	err := scanPage(s.pool.QueryRow(ctx, `UPDATE landing_pages SET slug=$4,name=$5,draft=$6,updated_at=now() WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3 RETURNING `+pageColumns,
		p.TenantID, p.WorkspaceID, page.ID, page.Slug, page.Name, page.Draft), &out)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LandingPage{}, ErrNotFound
	}
	return out, err
}

func (s *Store) ListLandingPages(ctx context.Context, p domain.Principal) ([]domain.LandingPage, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+pageColumns+` FROM landing_pages WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY created_at DESC`, p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LandingPage
	for rows.Next() {
		var item domain.LandingPage
		if err := rows.Scan(&item.ID, &item.TenantID, &item.WorkspaceID, &item.Slug, &item.Name, &item.Status, &item.Draft, &item.CurrentVersionID, &item.LatestVersion, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) PublishLandingPage(ctx context.Context, p domain.Principal, id, publishedBy, manifestKey string, definition json.RawMessage) (domain.PageVersion, error) {
	if publishedBy == "" || manifestKey == "" || len(definition) == 0 {
		return domain.PageVersion{}, errors.New("publisher, manifest, and definition are required")
	}
	if err := s.CheckMakerChecker(ctx, p, "pages", id, ""); err != nil {
		return domain.PageVersion{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.PageVersion{}, err
	}
	defer tx.Rollback(ctx)
	var tenant string
	var latest int
	var current *string
	err = tx.QueryRow(ctx, `SELECT tenant_id,latest_version,current_version_id FROM landing_pages WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`, p.TenantID, p.WorkspaceID, id).Scan(&tenant, &latest, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PageVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.PageVersion{}, err
	}
	if current != nil && *current != "" {
		var out domain.PageVersion
		err = tx.QueryRow(ctx, `SELECT id,page_id,tenant_id,version,definition,manifest_key,published_by,published_at FROM page_versions WHERE id=$1`, *current).Scan(&out.ID, &out.PageID, &out.TenantID, &out.Version, &out.Definition, &out.ManifestKey, &out.PublishedBy, &out.PublishedAt)
		if err != nil {
			return domain.PageVersion{}, err
		}
		if out.ManifestKey == manifestKey {
			return out, nil
		}
	}
	var out domain.PageVersion
	err = tx.QueryRow(ctx, `INSERT INTO page_versions (page_id,tenant_id,version,definition,manifest_key,published_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING id,page_id,tenant_id,version,definition,manifest_key,published_by,published_at`, id, tenant, latest+1, definition, manifestKey, publishedBy).Scan(&out.ID, &out.PageID, &out.TenantID, &out.Version, &out.Definition, &out.ManifestKey, &out.PublishedBy, &out.PublishedAt)
	if err != nil {
		return domain.PageVersion{}, fmt.Errorf("insert page version: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE landing_pages SET status='published',latest_version=$1,current_version_id=$2,updated_at=now() WHERE tenant_id=$3 AND workspace_id=$4 AND id=$5`, out.Version, out.ID, p.TenantID, p.WorkspaceID, id); err != nil {
		return domain.PageVersion{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.PageVersion{}, err
	}
	return out, nil
}

func (s *Store) GetPublishedLandingPage(ctx context.Context, slug string) (domain.LandingPage, domain.PageVersion, error) {
	var page domain.LandingPage
	var version domain.PageVersion
	err := scanPage(s.pool.QueryRow(ctx, `SELECT `+pageColumns+` FROM landing_pages WHERE slug=$1 AND status='published'`, slug), &page)
	if errors.Is(err, pgx.ErrNoRows) {
		return page, version, ErrNotFound
	}
	if err != nil {
		return page, version, err
	}
	if page.CurrentVersionID == nil {
		return page, version, ErrNotFound
	}
	err = s.pool.QueryRow(ctx, `SELECT id,page_id,tenant_id,version,definition,manifest_key,published_by,published_at FROM page_versions WHERE id=$1 AND page_id=$2`, *page.CurrentVersionID, page.ID).Scan(&version.ID, &version.PageID, &version.TenantID, &version.Version, &version.Definition, &version.ManifestKey, &version.PublishedBy, &version.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return page, domain.PageVersion{}, ErrNotFound
	}
	return page, version, err
}

func (s *Store) CreateAsset(ctx context.Context, p domain.Principal, asset domain.Asset) (domain.Asset, error) {
	var out domain.Asset
	err := s.pool.QueryRow(ctx, `INSERT INTO assets(tenant_id,workspace_id,filename,content_type,blob_key,size_bytes) VALUES($1,$2,$3,$4,$5,$6) RETURNING id,tenant_id,workspace_id,filename,content_type,blob_key,size_bytes,created_at`, p.TenantID, p.WorkspaceID, asset.Filename, asset.ContentType, asset.BlobKey, asset.SizeBytes).Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Filename, &out.ContentType, &out.BlobKey, &out.SizeBytes, &out.CreatedAt)
	return out, err
}

func (s *Store) ListAssets(ctx context.Context, p domain.Principal) ([]domain.Asset, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,tenant_id,workspace_id,filename,content_type,blob_key,size_bytes,created_at FROM assets WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY created_at DESC`, p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Asset
	for rows.Next() {
		var a domain.Asset
		if err := rows.Scan(&a.ID, &a.TenantID, &a.WorkspaceID, &a.Filename, &a.ContentType, &a.BlobKey, &a.SizeBytes, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
