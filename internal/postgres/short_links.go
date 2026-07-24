package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

const shortLinkColumns = `id, tenant_id, workspace_id, slug, destination_url, utm, created_at`

const shortLinkListLimit = 1000

func scanShortLink(row pgx.Row, out *domain.ShortLink) error {
	return row.Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Slug, &out.DestinationURL, &out.UTM, &out.CreatedAt)
}

func (s *Store) CreateShortLink(ctx context.Context, p domain.Principal, link domain.ShortLink) (domain.ShortLink, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.ShortLink{}, err
	}
	defer tx.Rollback(ctx)

	if link.Slug == "" || link.DestinationURL == "" {
		return domain.ShortLink{}, errors.New("slug and destination_url are required")
	}
	if len(link.UTM) == 0 {
		link.UTM = json.RawMessage(`{}`)
	}
	var out domain.ShortLink
	err = scanShortLink(tx.QueryRow(ctx, `INSERT INTO short_links
		(tenant_id, workspace_id, slug, destination_url, utm) VALUES ($1,$2,$3,$4,$5)
		RETURNING `+shortLinkColumns, p.TenantID, p.WorkspaceID, link.Slug, link.DestinationURL, link.UTM), &out)
	if err != nil {
		return domain.ShortLink{}, err
	}
	if err := s.audit(ctx, tx, p, "short_link.create", "short_link", out.ID, map[string]any{"slug": out.Slug}); err != nil {
		return domain.ShortLink{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ShortLink{}, err
	}
	return out, nil
}

func (s *Store) ListShortLinks(ctx context.Context, p domain.Principal) ([]domain.ShortLink, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+shortLinkColumns+` FROM short_links WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY created_at DESC LIMIT $3`, p.TenantID, p.WorkspaceID, shortLinkListLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ShortLink
	for rows.Next() {
		var link domain.ShortLink
		if err := rows.Scan(&link.ID, &link.TenantID, &link.WorkspaceID, &link.Slug, &link.DestinationURL, &link.UTM, &link.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, rows.Err()
}

// GetShortLinkBySlug is deliberately public and only returns an unambiguous slug.
// A colliding slug across tenants must not redirect to an arbitrary tenant.
func (s *Store) GetShortLinkBySlug(ctx context.Context, slug string) (domain.ShortLink, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+shortLinkColumns+` FROM short_links WHERE slug=$1 LIMIT 2`, slug)
	if err != nil {
		return domain.ShortLink{}, err
	}
	defer rows.Close()
	var out domain.ShortLink
	if !rows.Next() {
		return domain.ShortLink{}, ErrNotFound
	}
	if err := rows.Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Slug, &out.DestinationURL, &out.UTM, &out.CreatedAt); err != nil {
		return domain.ShortLink{}, err
	}
	if rows.Next() {
		return domain.ShortLink{}, errors.New("short-link slug is ambiguous")
	}
	return out, nil
}
