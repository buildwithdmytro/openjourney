package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/go-jose/go-jose/v4"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateExtension(ctx context.Context, p domain.Principal, ext domain.Extension) (domain.Extension, error) {
	if ext.Name == "" {
		return domain.Extension{}, errors.New("extension name is required")
	}
	if ext.Publisher == "" {
		return domain.Extension{}, errors.New("extension publisher is required")
	}
	var out domain.Extension
	err := s.pool.QueryRow(ctx, `INSERT INTO extensions (tenant_id, workspace_id, name, publisher, status)
		VALUES ($1, $2, $3, $4, COALESCE(NULLIF($5, ''), 'installed'))
		RETURNING id, tenant_id, workspace_id, name, publisher, current_version_id, latest_version, status, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, ext.Name, ext.Publisher, ext.Status).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Publisher, &out.CurrentVersionID, &out.LatestVersion, &out.Status, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.Extension{}, err
	}
	_ = s.audit(ctx, p, "extension.create", "extension", out.ID, map[string]any{"name": out.Name})
	return out, nil
}

func (s *Store) GetExtension(ctx context.Context, p domain.Principal, id string) (domain.Extension, error) {
	var out domain.Extension
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, name, publisher, current_version_id, latest_version, status, created_at, updated_at
		FROM extensions WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3`,
		p.TenantID, p.WorkspaceID, id).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Publisher, &out.CurrentVersionID, &out.LatestVersion, &out.Status, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Extension{}, ErrNotFound
	}
	if err != nil {
		return domain.Extension{}, err
	}
	return out, nil
}

func (s *Store) GetExtensionByName(ctx context.Context, p domain.Principal, name string) (domain.Extension, error) {
	var out domain.Extension
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, name, publisher, current_version_id, latest_version, status, created_at, updated_at
		FROM extensions WHERE tenant_id = $1 AND workspace_id = $2 AND name = $3`,
		p.TenantID, p.WorkspaceID, name).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Publisher, &out.CurrentVersionID, &out.LatestVersion, &out.Status, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Extension{}, ErrNotFound
	}
	if err != nil {
		return domain.Extension{}, err
	}
	return out, nil
}

func (s *Store) ListExtensions(ctx context.Context, p domain.Principal) ([]domain.Extension, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, workspace_id, name, publisher, current_version_id, latest_version, status, created_at, updated_at
		FROM extensions WHERE tenant_id = $1 AND workspace_id = $2 ORDER BY name`,
		p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Extension
	for rows.Next() {
		var item domain.Extension
		err := rows.Scan(&item.ID, &item.TenantID, &item.WorkspaceID, &item.Name, &item.Publisher, &item.CurrentVersionID, &item.LatestVersion, &item.Status, &item.CreatedAt, &item.UpdatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpdateExtension(ctx context.Context, p domain.Principal, ext domain.Extension) (domain.Extension, error) {
	var out domain.Extension
	err := s.pool.QueryRow(ctx, `UPDATE extensions
		SET name = COALESCE(NULLIF($4, ''), name),
		    publisher = COALESCE(NULLIF($5, ''), publisher),
		    status = COALESCE(NULLIF($6, ''), status),
		    updated_at = now()
		WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3
		RETURNING id, tenant_id, workspace_id, name, publisher, current_version_id, latest_version, status, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, ext.ID, ext.Name, ext.Publisher, ext.Status).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Publisher, &out.CurrentVersionID, &out.LatestVersion, &out.Status, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Extension{}, ErrNotFound
	}
	if err != nil {
		return domain.Extension{}, err
	}
	_ = s.audit(ctx, p, "extension.update", "extension", out.ID, map[string]any{"status": out.Status})
	return out, nil
}

func (s *Store) DeleteExtension(ctx context.Context, p domain.Principal, id string) (domain.Extension, error) {
	var out domain.Extension
	err := s.pool.QueryRow(ctx, `DELETE FROM extensions WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3
		RETURNING id, tenant_id, workspace_id, name, publisher, current_version_id, latest_version, status, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, id).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Publisher, &out.CurrentVersionID, &out.LatestVersion, &out.Status, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Extension{}, ErrNotFound
	}
	if err != nil {
		return domain.Extension{}, err
	}
	_ = s.audit(ctx, p, "extension.delete", "extension", id, nil)
	return out, nil
}

func (s *Store) CreateExtensionVersion(ctx context.Context, p domain.Principal, ev domain.ExtensionVersion) (domain.ExtensionVersion, error) {
	if ev.ExtensionID == "" {
		return domain.ExtensionVersion{}, errors.New("extension ID is required")
	}
	if ev.Kind == "" {
		return domain.ExtensionVersion{}, errors.New("extension version kind is required")
	}
	if ev.Transport == "" {
		return domain.ExtensionVersion{}, errors.New("extension version transport is required")
	}
	if len(ev.Manifest) == 0 {
		return domain.ExtensionVersion{}, errors.New("extension version manifest is required")
	}
	if ev.Signature == "" {
		return domain.ExtensionVersion{}, errors.New("extension version signature is required")
	}

	var extExists bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM extensions WHERE tenant_id = $1 AND id = $2)", p.TenantID, ev.ExtensionID).Scan(&extExists)
	if err != nil {
		return domain.ExtensionVersion{}, err
	}
	if !extExists {
		return domain.ExtensionVersion{}, fmt.Errorf("extension not found: %s", ev.ExtensionID)
	}

	var out domain.ExtensionVersion
	var outManifest []byte
	var outScopes []string

	err = s.pool.QueryRow(ctx, `INSERT INTO extension_versions 
		(extension_id, tenant_id, version, kind, transport, manifest, requested_scopes, signature, signing_key_id, wasm_blob_key, manifest_key, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE(NULLIF($9, ''), 'pending'), $10, COALESCE(NULLIF($11, ''), ''), COALESCE(NULLIF($12, ''), 'draft'))
		RETURNING id, extension_id, tenant_id, version, kind, transport, manifest, requested_scopes, signature, signing_key_id, wasm_blob_key, manifest_key, status, installed_by, installed_at, created_at`,
		ev.ExtensionID, p.TenantID, ev.Version, ev.Kind, ev.Transport, ev.Manifest, ev.RequestedScopes, ev.Signature, ev.SigningKeyID, ev.WasmBlobKey, ev.ManifestKey, ev.Status).
		Scan(&out.ID, &out.ExtensionID, &out.TenantID, &out.Version, &out.Kind, &out.Transport, &outManifest, &outScopes, &out.Signature, &out.SigningKeyID, &out.WasmBlobKey, &out.ManifestKey, &out.Status, &out.InstalledBy, &out.InstalledAt, &out.CreatedAt)
	if err != nil {
		return domain.ExtensionVersion{}, err
	}
	out.Manifest = json.RawMessage(outManifest)
	out.RequestedScopes = outScopes

	_ = s.audit(ctx, p, "extension_version.create", "extension_version", out.ID, map[string]any{"version": out.Version})
	return out, nil
}

func (s *Store) GetExtensionVersion(ctx context.Context, p domain.Principal, id string) (domain.ExtensionVersion, error) {
	var out domain.ExtensionVersion
	var outManifest []byte
	var outScopes []string
	err := s.pool.QueryRow(ctx, `SELECT id, extension_id, tenant_id, version, kind, transport, manifest, requested_scopes, signature, signing_key_id, wasm_blob_key, manifest_key, status, installed_by, installed_at, created_at
		FROM extension_versions WHERE tenant_id = $1 AND id = $2`,
		p.TenantID, id).
		Scan(&out.ID, &out.ExtensionID, &out.TenantID, &out.Version, &out.Kind, &out.Transport, &outManifest, &outScopes, &out.Signature, &out.SigningKeyID, &out.WasmBlobKey, &out.ManifestKey, &out.Status, &out.InstalledBy, &out.InstalledAt, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ExtensionVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.ExtensionVersion{}, err
	}
	out.Manifest = json.RawMessage(outManifest)
	out.RequestedScopes = outScopes
	return out, nil
}

func (s *Store) GetExtensionVersionByNumber(ctx context.Context, p domain.Principal, extensionID string, version int) (domain.ExtensionVersion, error) {
	var out domain.ExtensionVersion
	var outManifest []byte
	var outScopes []string
	err := s.pool.QueryRow(ctx, `SELECT id, extension_id, tenant_id, version, kind, transport, manifest, requested_scopes, signature, signing_key_id, wasm_blob_key, manifest_key, status, installed_by, installed_at, created_at
		FROM extension_versions WHERE tenant_id = $1 AND extension_id = $2 AND version = $3`,
		p.TenantID, extensionID, version).
		Scan(&out.ID, &out.ExtensionID, &out.TenantID, &out.Version, &out.Kind, &out.Transport, &outManifest, &outScopes, &out.Signature, &out.SigningKeyID, &out.WasmBlobKey, &out.ManifestKey, &out.Status, &out.InstalledBy, &out.InstalledAt, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ExtensionVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.ExtensionVersion{}, err
	}
	out.Manifest = json.RawMessage(outManifest)
	out.RequestedScopes = outScopes
	return out, nil
}

func (s *Store) ListExtensionVersions(ctx context.Context, p domain.Principal, extensionID string) ([]domain.ExtensionVersion, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, extension_id, tenant_id, version, kind, transport, manifest, requested_scopes, signature, signing_key_id, wasm_blob_key, manifest_key, status, installed_by, installed_at, created_at
		FROM extension_versions WHERE tenant_id = $1 AND extension_id = $2 ORDER BY version DESC`,
		p.TenantID, extensionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ExtensionVersion
	for rows.Next() {
		var item domain.ExtensionVersion
		var itemManifest []byte
		var itemScopes []string
		err := rows.Scan(&item.ID, &item.ExtensionID, &item.TenantID, &item.Version, &item.Kind, &item.Transport, &itemManifest, &itemScopes, &item.Signature, &item.SigningKeyID, &item.WasmBlobKey, &item.ManifestKey, &item.Status, &item.InstalledBy, &item.InstalledAt, &item.CreatedAt)
		if err != nil {
			return nil, err
		}
		item.Manifest = json.RawMessage(itemManifest)
		item.RequestedScopes = itemScopes
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) PublishExtensionVersion(ctx context.Context, p domain.Principal, extensionID string, version int, approverUserID string, manifestKey string) (domain.ExtensionVersion, error) {
	if approverUserID == "" {
		return domain.ExtensionVersion{}, errors.New("approver user id is required")
	}
	if manifestKey == "" {
		return domain.ExtensionVersion{}, errors.New("manifest key is required")
	}
	if p.ActorType != "user" || p.UserID == "" {
		return domain.ExtensionVersion{}, ErrUnauthorized
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.ExtensionVersion{}, err
	}
	defer tx.Rollback(ctx)

	var ev domain.ExtensionVersion
	var manifestJSON []byte
	var requestedScopes []string
	err = tx.QueryRow(ctx, `SELECT id, extension_id, tenant_id, version, kind, transport, manifest, requested_scopes, signature, signing_key_id, wasm_blob_key, manifest_key, status, installed_by, installed_at, created_at
		FROM extension_versions WHERE tenant_id = $1 AND extension_id = $2 AND version = $3 FOR UPDATE`,
		p.TenantID, extensionID, version).
		Scan(&ev.ID, &ev.ExtensionID, &ev.TenantID, &ev.Version, &ev.Kind, &ev.Transport, &manifestJSON, &requestedScopes, &ev.Signature, &ev.SigningKeyID, &ev.WasmBlobKey, &ev.ManifestKey, &ev.Status, &ev.InstalledBy, &ev.InstalledAt, &ev.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ExtensionVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.ExtensionVersion{}, err
	}
	ev.Manifest = json.RawMessage(manifestJSON)
	ev.RequestedScopes = requestedScopes

	if ev.Status == "active" {
		return ev, nil
	}

	// Verify JWS signature using go-jose
	object, err := jose.ParseSigned(ev.Signature, []jose.SignatureAlgorithm{
		jose.RS256, jose.RS384, jose.RS512,
		jose.ES256, jose.ES384, jose.ES512,
		jose.EdDSA,
		jose.HS256, jose.HS384, jose.HS512,
	})
	if err != nil {
		return domain.ExtensionVersion{}, fmt.Errorf("invalid JWS signature: %w", err)
	}

	var kid string
	for _, sig := range object.Signatures {
		kid = sig.Header.KeyID
		break
	}
	if kid == "" {
		return domain.ExtensionVersion{}, errors.New("missing kid in JWS header")
	}

	s.trustedKeysMu.RLock()
	pubKey, ok := s.trustedKeys[kid]
	s.trustedKeysMu.RUnlock()
	if !ok {
		return domain.ExtensionVersion{}, fmt.Errorf("untrusted signing key id: %q", kid)
	}

	payload, err := object.Verify(pubKey)
	if err != nil {
		return domain.ExtensionVersion{}, fmt.Errorf("JWS verification failed: %w", err)
	}

	// Verify that verified JWS payload matches ev.Manifest content
	var m1, m2 any
	if err := json.Unmarshal(manifestJSON, &m1); err != nil {
		return domain.ExtensionVersion{}, fmt.Errorf("unmarshal manifest: %w", err)
	}
	if err := json.Unmarshal(payload, &m2); err != nil {
		return domain.ExtensionVersion{}, fmt.Errorf("unmarshal verified payload: %w", err)
	}

	b1, _ := json.Marshal(m1)
	b2, _ := json.Marshal(m2)
	if string(b1) != string(b2) {
		return domain.ExtensionVersion{}, errors.New("manifest content does not match signature payload")
	}

	// Archive other active versions of this extension
	_, err = tx.Exec(ctx, `UPDATE extension_versions SET status = 'archived' WHERE tenant_id = $1 AND extension_id = $2 AND status = 'active'`,
		p.TenantID, extensionID)
	if err != nil {
		return domain.ExtensionVersion{}, err
	}

	// Set version status to 'active'
	var out domain.ExtensionVersion
	var outManifest []byte
	var outScopes []string
	err = tx.QueryRow(ctx, `UPDATE extension_versions
		SET status = 'active', installed_by = $1, installed_at = now(), manifest_key = $2, signing_key_id = $3
		WHERE tenant_id = $4 AND id = $5
		RETURNING id, extension_id, tenant_id, version, kind, transport, manifest, requested_scopes, signature, signing_key_id, wasm_blob_key, manifest_key, status, installed_by, installed_at, created_at`,
		approverUserID, manifestKey, kid, p.TenantID, ev.ID).
		Scan(&out.ID, &out.ExtensionID, &out.TenantID, &out.Version, &out.Kind, &out.Transport, &outManifest, &outScopes, &out.Signature, &out.SigningKeyID, &out.WasmBlobKey, &out.ManifestKey, &out.Status, &out.InstalledBy, &out.InstalledAt, &out.CreatedAt)
	if err != nil {
		return domain.ExtensionVersion{}, err
	}
	out.Manifest = json.RawMessage(outManifest)
	out.RequestedScopes = outScopes

	// Update parent extension's current_version_id and latest_version
	_, err = tx.Exec(ctx, `UPDATE extensions
		SET current_version_id = $1, latest_version = GREATEST(latest_version, $2), status = 'enabled', updated_at = now()
		WHERE tenant_id = $3 AND id = $4`,
		out.ID, out.Version, p.TenantID, extensionID)
	if err != nil {
		return domain.ExtensionVersion{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ExtensionVersion{}, err
	}

	_ = s.audit(ctx, p, "extension_version.publish", "extension_version", out.ID, map[string]any{"version": out.Version})
	return out, nil
}
