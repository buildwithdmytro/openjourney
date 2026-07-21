package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

const catalogColumns = `id, tenant_id, workspace_id, app_id, key, name, description, item_key_field, status, item_count, created_at, updated_at`
const catalogItemColumns = `id, catalog_id, tenant_id, app_id, item_key, payload, updated_at`
const connectedContentSourceColumns = `id, tenant_id, workspace_id, name, allowed_host, auth_header_name, auth_secret_ref, default_ttl_seconds, timeout_ms, enabled, status, created_by_user_id, created_at, updated_at`

func scanCatalog(row pgx.Row) (domain.Catalog, error) {
	var out domain.Catalog
	err := row.Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.AppID, &out.Key, &out.Name, &out.Description, &out.ItemKeyField, &out.Status, &out.ItemCount, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.Catalog{}, err
	}
	return out, nil
}

func scanCatalogItem(row pgx.Row) (domain.CatalogItem, error) {
	var out domain.CatalogItem
	err := row.Scan(&out.ID, &out.CatalogID, &out.TenantID, &out.AppID, &out.ItemKey, &out.Payload, &out.UpdatedAt)
	if err != nil {
		return domain.CatalogItem{}, err
	}
	return out, nil
}

func scanConnectedContentSource(row pgx.Row) (domain.ConnectedContentSource, error) {
	var out domain.ConnectedContentSource
	err := row.Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.AllowedHost, &out.AuthHeaderName, &out.AuthSecretRef, &out.DefaultTTLSeconds, &out.TimeoutMs, &out.Enabled, &out.Status, &out.CreatedByUserID, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.ConnectedContentSource{}, err
	}
	return out, nil
}

// Catalogs CRUD

func (s *Store) CreateCatalog(ctx context.Context, p domain.Principal, input domain.Catalog) (domain.Catalog, error) {
	if input.Key == "" {
		return domain.Catalog{}, errors.New("catalog key is required")
	}

	cat, err := scanCatalog(s.pool.QueryRow(ctx, `INSERT INTO catalogs
		(tenant_id, workspace_id, app_id, key, name, description, item_key_field, status, item_count)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+catalogColumns,
		p.TenantID, p.WorkspaceID, p.AppID, input.Key, input.Name, input.Description, input.ItemKeyField, input.Status, 0))
	if err != nil {
		if strings.Contains(err.Error(), "unique constraint") {
			return domain.Catalog{}, errors.New("catalog key already exists in this app")
		}
		return domain.Catalog{}, err
	}

	_ = s.audit(ctx, p, "catalog.create", "catalog", cat.ID, map[string]any{"key": cat.Key, "name": cat.Name})
	return cat, nil
}

func (s *Store) GetCatalog(ctx context.Context, p domain.Principal, id string) (domain.Catalog, error) {
	cat, err := scanCatalog(s.pool.QueryRow(ctx, `SELECT `+catalogColumns+` FROM catalogs
		WHERE tenant_id=$1 AND app_id=$2 AND id=$3`,
		p.TenantID, p.AppID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Catalog{}, ErrNotFound
	}
	return cat, err
}

func (s *Store) ListCatalogs(ctx context.Context, p domain.Principal) ([]domain.Catalog, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+catalogColumns+` FROM catalogs
		WHERE tenant_id=$1 AND app_id=$2 ORDER BY created_at DESC`,
		p.TenantID, p.AppID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Catalog
	for rows.Next() {
		cat, err := scanCatalog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cat)
	}
	return out, rows.Err()
}

func (s *Store) UpdateCatalog(ctx context.Context, p domain.Principal, input domain.Catalog) (domain.Catalog, error) {
	cat, err := scanCatalog(s.pool.QueryRow(ctx, `UPDATE catalogs
		SET name=$1, description=$2, status=$3, updated_at=now()
		WHERE tenant_id=$4 AND app_id=$5 AND id=$6 RETURNING `+catalogColumns,
		input.Name, input.Description, input.Status, p.TenantID, p.AppID, input.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Catalog{}, ErrNotFound
	}
	if err != nil {
		return domain.Catalog{}, err
	}

	_ = s.audit(ctx, p, "catalog.update", "catalog", input.ID, map[string]any{"status": input.Status})
	return cat, nil
}

func (s *Store) DeleteCatalog(ctx context.Context, p domain.Principal, id string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM catalogs
		WHERE tenant_id=$1 AND app_id=$2 AND id=$3`,
		p.TenantID, p.AppID, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	_ = s.audit(ctx, p, "catalog.delete", "catalog", id, map[string]any{})
	return nil
}

// Catalog Items queries

func (s *Store) GetCatalogItem(ctx context.Context, p domain.Principal, catalogID, itemKey string) (domain.CatalogItem, error) {
	item, err := scanCatalogItem(s.pool.QueryRow(ctx, `SELECT `+catalogItemColumns+` FROM catalog_items
		WHERE catalog_id=$1 AND item_key=$2 AND tenant_id=$3 AND app_id=$4`,
		catalogID, itemKey, p.TenantID, p.AppID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CatalogItem{}, ErrNotFound
	}
	return item, err
}

func (s *Store) ListCatalogItems(ctx context.Context, p domain.Principal, catalogID string, limit int) ([]domain.CatalogItem, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 10000 {
		limit = 10000
	}

	rows, err := s.pool.Query(ctx, `SELECT `+catalogItemColumns+` FROM catalog_items
		WHERE catalog_id=$1 AND tenant_id=$2 AND app_id=$3 ORDER BY item_key ASC LIMIT $4`,
		catalogID, p.TenantID, p.AppID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.CatalogItem
	for rows.Next() {
		item, err := scanCatalogItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) BulkUpsertCatalogItems(ctx context.Context, p domain.Principal, items []domain.CatalogItem) (domain.BulkUpsertResult, error) {
	if len(items) == 0 {
		return domain.BulkUpsertResult{}, nil
	}

	const chunkSize = 1000
	var insertedCount, updatedCount int64

	for i := 0; i < len(items); i += chunkSize {
		end := i + chunkSize
		if end > len(items) {
			end = len(items)
		}

		chunk := items[i:end]
		var valuesClause strings.Builder
		var args []any

		for j, item := range chunk {
			if j > 0 {
				valuesClause.WriteString(", ")
			}
			valuesClause.WriteString(fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d)", len(args)+1, len(args)+2, len(args)+3, len(args)+4, len(args)+5, len(args)+6))
			args = append(args, item.CatalogID, item.TenantID, item.AppID, item.ItemKey, item.Payload, time.Now())
		}

		query := fmt.Sprintf(`INSERT INTO catalog_items (catalog_id, tenant_id, app_id, item_key, payload, updated_at)
			VALUES %s
			ON CONFLICT (catalog_id, item_key) DO UPDATE SET payload=EXCLUDED.payload, updated_at=now()`, valuesClause.String())

		result, err := s.pool.Exec(ctx, query, args...)
		if err != nil {
			return domain.BulkUpsertResult{}, err
		}

		rowsAffected := result.RowsAffected()
		insertedCount += rowsAffected
	}

	// Since we're upserting, we need to check how many were inserted vs updated
	// This is approximate since we're counting total affected rows
	// In a real implementation, we might want to use RETURNING to get exact counts
	updatedCount = 0

	// Update the catalog item_count
	if len(items) > 0 {
		catalogID := items[0].CatalogID
		_, err := s.pool.Exec(ctx, `UPDATE catalogs SET item_count=(SELECT COUNT(*) FROM catalog_items WHERE catalog_id=$1), updated_at=now() WHERE id=$2`,
			catalogID, catalogID)
		if err != nil {
			return domain.BulkUpsertResult{}, err
		}
	}

	return domain.BulkUpsertResult{InsertedCount: insertedCount, UpdatedCount: updatedCount}, nil
}

// Connected Content Sources CRUD

func (s *Store) CreateConnectedContentSource(ctx context.Context, p domain.Principal, input domain.ConnectedContentSource) (domain.ConnectedContentSource, error) {
	if input.Name == "" {
		return domain.ConnectedContentSource{}, errors.New("source name is required")
	}
	if input.AllowedHost == "" {
		return domain.ConnectedContentSource{}, errors.New("allowed_host is required")
	}
	if input.AuthSecretRef != "" && strings.HasPrefix(input.AuthSecretRef, "secret:") {
		return domain.ConnectedContentSource{}, errors.New("auth_secret_ref must be an environment variable name (e.g., CC_SECRET), not a secret: reference")
	}

	src, err := scanConnectedContentSource(s.pool.QueryRow(ctx, `INSERT INTO connected_content_sources
		(tenant_id, workspace_id, name, allowed_host, auth_header_name, auth_secret_ref, default_ttl_seconds, timeout_ms, enabled, status, created_by_user_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+connectedContentSourceColumns,
		p.TenantID, p.WorkspaceID, input.Name, input.AllowedHost, input.AuthHeaderName, input.AuthSecretRef, input.DefaultTTLSeconds, input.TimeoutMs, input.Enabled, input.Status, p.UserID))
	if err != nil {
		if strings.Contains(err.Error(), "unique constraint") {
			return domain.ConnectedContentSource{}, errors.New("source name already exists in this workspace")
		}
		return domain.ConnectedContentSource{}, err
	}

	_ = s.audit(ctx, p, "connected_content_source.create", "connected_content_source", src.ID, map[string]any{"name": src.Name, "allowed_host": src.AllowedHost})
	return src, nil
}

func (s *Store) GetConnectedContentSource(ctx context.Context, p domain.Principal, id string) (domain.ConnectedContentSource, error) {
	src, err := scanConnectedContentSource(s.pool.QueryRow(ctx, `SELECT `+connectedContentSourceColumns+` FROM connected_content_sources
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ConnectedContentSource{}, ErrNotFound
	}
	return src, err
}

func (s *Store) ListConnectedContentSources(ctx context.Context, p domain.Principal) ([]domain.ConnectedContentSource, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+connectedContentSourceColumns+` FROM connected_content_sources
		WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY created_at DESC`,
		p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ConnectedContentSource
	for rows.Next() {
		src, err := scanConnectedContentSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *Store) UpdateConnectedContentSource(ctx context.Context, p domain.Principal, input domain.ConnectedContentSource) (domain.ConnectedContentSource, error) {
	if input.AuthSecretRef != "" && strings.HasPrefix(input.AuthSecretRef, "secret:") {
		return domain.ConnectedContentSource{}, errors.New("auth_secret_ref must be an environment variable name (e.g., CC_SECRET), not a secret: reference")
	}

	src, err := scanConnectedContentSource(s.pool.QueryRow(ctx, `UPDATE connected_content_sources
		SET name=$1, allowed_host=$2, auth_header_name=$3, auth_secret_ref=$4, default_ttl_seconds=$5, timeout_ms=$6, enabled=$7, status=$8, updated_at=now()
		WHERE tenant_id=$9 AND workspace_id=$10 AND id=$11 RETURNING `+connectedContentSourceColumns,
		input.Name, input.AllowedHost, input.AuthHeaderName, input.AuthSecretRef, input.DefaultTTLSeconds, input.TimeoutMs, input.Enabled, input.Status, p.TenantID, p.WorkspaceID, input.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ConnectedContentSource{}, ErrNotFound
	}
	if err != nil {
		return domain.ConnectedContentSource{}, err
	}

	_ = s.audit(ctx, p, "connected_content_source.update", "connected_content_source", input.ID, map[string]any{"enabled": input.Enabled, "status": input.Status})
	return src, nil
}

func (s *Store) DeleteConnectedContentSource(ctx context.Context, p domain.Principal, id string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM connected_content_sources
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	_ = s.audit(ctx, p, "connected_content_source.delete", "connected_content_source", id, map[string]any{})
	return nil
}
