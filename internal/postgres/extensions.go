package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/extension"
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
	if ext.Status == "enabled" && (p.ActorType != "user" || p.UserID == "") {
		return domain.Extension{}, errors.New("human_approval_required: enabling an extension requires an authenticated user")
	}
	if ext.Status == "enabled" {
		var transport string
		var config []byte
		err := s.pool.QueryRow(ctx, `SELECT ev.transport, ec.config
			FROM extensions e JOIN extension_versions ev ON ev.id=e.current_version_id
			LEFT JOIN extension_configs ec ON ec.extension_id=e.id
			WHERE e.tenant_id=$1 AND e.workspace_id=$2 AND e.id=$3`, p.TenantID, p.WorkspaceID, ext.ID).Scan(&transport, &config)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Extension{}, ErrNotFound
		}
		if err != nil {
			return domain.Extension{}, err
		}
		if err := extension.ValidateRemoteHTTPConfig(transport, config); err != nil {
			return domain.Extension{}, err
		}
	}
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
	if ev.Transport == "remote_http" {
		var config []byte
		err := tx.QueryRow(ctx, `SELECT config FROM extension_configs WHERE tenant_id=$1 AND workspace_id=(SELECT workspace_id FROM extensions WHERE tenant_id=$1 AND id=$2) AND extension_id=$2`, p.TenantID, extensionID).Scan(&config)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ExtensionVersion{}, errors.New("remote_http extension requires an HMAC configuration before publish")
		}
		if err != nil {
			return domain.ExtensionVersion{}, err
		}
		if err := extension.ValidateRemoteHTTPConfig(ev.Transport, config); err != nil {
			return domain.ExtensionVersion{}, err
		}
	}

	// Verify JWS signature using go-jose
	object, err := jose.ParseSigned(ev.Signature, []jose.SignatureAlgorithm{
		jose.RS256, jose.RS384, jose.RS512,
		jose.ES256, jose.ES384, jose.ES512,
		jose.EdDSA,
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

func (s *Store) UpsertExtensionConfig(ctx context.Context, p domain.Principal, cfg domain.ExtensionConfig) (domain.ExtensionConfig, error) {
	if cfg.ExtensionID == "" {
		return domain.ExtensionConfig{}, errors.New("extension ID is required")
	}

	// Ensure the parent extension exists and belongs to the workspace/tenant
	var extExists bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM extensions WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3)",
		p.TenantID, p.WorkspaceID, cfg.ExtensionID).Scan(&extExists)
	if err != nil {
		return domain.ExtensionConfig{}, err
	}
	if !extExists {
		return domain.ExtensionConfig{}, fmt.Errorf("extension not found: %s", cfg.ExtensionID)
	}

	var out domain.ExtensionConfig
	var outConfig []byte
	var outAllowlist []string

	err = s.pool.QueryRow(ctx, `
		INSERT INTO extension_configs (
			extension_id, tenant_id, workspace_id, config, endpoint_allowlist, 
			timeout_ms, max_memory_mb, monthly_budget_cents, rate_per_min, status, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, COALESCE(NULLIF($10, ''), 'active'), now())
		ON CONFLICT (extension_id) DO UPDATE SET
			config = EXCLUDED.config,
			endpoint_allowlist = EXCLUDED.endpoint_allowlist,
			timeout_ms = EXCLUDED.timeout_ms,
			max_memory_mb = EXCLUDED.max_memory_mb,
			monthly_budget_cents = EXCLUDED.monthly_budget_cents,
			rate_per_min = EXCLUDED.rate_per_min,
			status = COALESCE(NULLIF(EXCLUDED.status, ''), extension_configs.status),
			updated_at = now()
		RETURNING extension_id, tenant_id, workspace_id, config, endpoint_allowlist, timeout_ms, max_memory_mb, monthly_budget_cents, rate_per_min, status, updated_at`,
		cfg.ExtensionID, p.TenantID, p.WorkspaceID, cfg.Config, cfg.EndpointAllowlist,
		cfg.TimeoutMs, cfg.MaxMemoryMb, cfg.MonthlyBudgetCents, cfg.RatePerMin, cfg.Status).
		Scan(&out.ExtensionID, &out.TenantID, &out.WorkspaceID, &outConfig, &outAllowlist,
			&out.TimeoutMs, &out.MaxMemoryMb, &out.MonthlyBudgetCents, &out.RatePerMin, &out.Status, &out.UpdatedAt)
	if err != nil {
		return domain.ExtensionConfig{}, err
	}
	out.Config = json.RawMessage(outConfig)
	out.EndpointAllowlist = outAllowlist

	_ = s.audit(ctx, p, "extension_config.upsert", "extension_config", out.ExtensionID, nil)
	return out, nil
}

func (s *Store) GetExtensionConfig(ctx context.Context, p domain.Principal, extensionID string) (domain.ExtensionConfig, error) {
	var out domain.ExtensionConfig
	var outConfig []byte
	var outAllowlist []string

	err := s.pool.QueryRow(ctx, `
		SELECT extension_id, tenant_id, workspace_id, config, endpoint_allowlist, timeout_ms, max_memory_mb, monthly_budget_cents, rate_per_min, status, updated_at
		FROM extension_configs
		WHERE tenant_id = $1 AND workspace_id = $2 AND extension_id = $3`,
		p.TenantID, p.WorkspaceID, extensionID).
		Scan(&out.ExtensionID, &out.TenantID, &out.WorkspaceID, &outConfig, &outAllowlist,
			&out.TimeoutMs, &out.MaxMemoryMb, &out.MonthlyBudgetCents, &out.RatePerMin, &out.Status, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ExtensionConfig{}, ErrNotFound
	}
	if err != nil {
		return domain.ExtensionConfig{}, err
	}
	out.Config = json.RawMessage(outConfig)
	out.EndpointAllowlist = outAllowlist
	return out, nil
}

func (s *Store) DeleteExtensionConfig(ctx context.Context, p domain.Principal, extensionID string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM extension_configs
		WHERE tenant_id = $1 AND workspace_id = $2 AND extension_id = $3`,
		p.TenantID, p.WorkspaceID, extensionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	_ = s.audit(ctx, p, "extension_config.delete", "extension_config", extensionID, nil)
	return nil
}

func (s *Store) CreateExtensionGrant(ctx context.Context, p domain.Principal, grant domain.ExtensionGrant) (domain.ExtensionGrant, error) {
	if grant.ExtensionID == "" {
		return domain.ExtensionGrant{}, errors.New("extension ID is required")
	}
	if grant.Scope == "" {
		return domain.ExtensionGrant{}, errors.New("scope is required")
	}
	if grant.GrantedBy == "" {
		return domain.ExtensionGrant{}, errors.New("granted_by user ID is required")
	}

	// Ensure parent extension exists and belongs to the tenant
	var extExists bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM extensions WHERE tenant_id = $1 AND id = $2)",
		p.TenantID, grant.ExtensionID).Scan(&extExists)
	if err != nil {
		return domain.ExtensionGrant{}, err
	}
	if !extExists {
		return domain.ExtensionGrant{}, fmt.Errorf("extension not found: %s", grant.ExtensionID)
	}

	var out domain.ExtensionGrant
	err = s.pool.QueryRow(ctx, `
		INSERT INTO extension_grants (extension_id, tenant_id, scope, granted_by, granted_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (extension_id, scope) DO UPDATE SET
			granted_by = EXCLUDED.granted_by,
			granted_at = now()
		RETURNING extension_id, tenant_id, scope, granted_by, granted_at`,
		grant.ExtensionID, p.TenantID, grant.Scope, grant.GrantedBy).
		Scan(&out.ExtensionID, &out.TenantID, &out.Scope, &out.GrantedBy, &out.GrantedAt)
	if err != nil {
		return domain.ExtensionGrant{}, err
	}

	_ = s.audit(ctx, p, "extension_grant.create", "extension_grant", out.ExtensionID, map[string]any{"scope": out.Scope})
	return out, nil
}

func (s *Store) ListExtensionGrants(ctx context.Context, p domain.Principal, extensionID string) ([]domain.ExtensionGrant, error) {
	// Ensure parent extension exists and belongs to the tenant
	var extExists bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM extensions WHERE tenant_id = $1 AND id = $2)",
		p.TenantID, extensionID).Scan(&extExists)
	if err != nil {
		return nil, err
	}
	if !extExists {
		return nil, fmt.Errorf("extension not found: %s", extensionID)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT extension_id, tenant_id, scope, granted_by, granted_at
		FROM extension_grants
		WHERE tenant_id = $1 AND extension_id = $2
		ORDER BY scope`,
		p.TenantID, extensionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ExtensionGrant
	for rows.Next() {
		var item domain.ExtensionGrant
		err := rows.Scan(&item.ExtensionID, &item.TenantID, &item.Scope, &item.GrantedBy, &item.GrantedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) DeleteExtensionGrant(ctx context.Context, p domain.Principal, extensionID string, scope string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM extension_grants
		WHERE tenant_id = $1 AND extension_id = $2 AND scope = $3`,
		p.TenantID, extensionID, scope)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	_ = s.audit(ctx, p, "extension_grant.delete", "extension_grant", extensionID, map[string]any{"scope": scope})
	return nil
}

func (s *Store) RecordExtensionActivity(ctx context.Context, p domain.Principal, act domain.ExtensionActivity) (domain.ExtensionActivity, error) {
	if act.ExtensionID == "" {
		return domain.ExtensionActivity{}, errors.New("extension ID is required")
	}
	if act.PolicyDecision == "" {
		return domain.ExtensionActivity{}, errors.New("policy decision is required")
	}
	var out domain.ExtensionActivity

	query := `INSERT INTO extension_activity (tenant_id, workspace_id, extension_id, extension_version, kind, invocation, derived_scopes, input_ref, output_ref, latency_ms, cost_cents, policy_decision`
	values := []any{p.TenantID, p.WorkspaceID, act.ExtensionID, act.ExtensionVersion, act.Kind, act.Invocation, act.DerivedScopes, act.InputRef, act.OutputRef, act.LatencyMs, act.CostCents, act.PolicyDecision}
	if !act.CreatedAt.IsZero() {
		query += `, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`
		values = append(values, act.CreatedAt)
	} else {
		query += `) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	}
	query += ` RETURNING id, tenant_id, workspace_id, extension_id, extension_version, kind, invocation, derived_scopes, input_ref, output_ref, latency_ms, cost_cents, policy_decision, created_at`

	err := s.pool.QueryRow(ctx, query, values...).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.ExtensionID, &out.ExtensionVersion, &out.Kind, &out.Invocation,
			&out.DerivedScopes, &out.InputRef, &out.OutputRef, &out.LatencyMs, &out.CostCents, &out.PolicyDecision, &out.CreatedAt)
	if err != nil {
		return domain.ExtensionActivity{}, err
	}
	return out, nil
}

func (s *Store) ListExtensionActivities(ctx context.Context, p domain.Principal, extensionID string, limit int) ([]domain.ExtensionActivity, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, workspace_id, extension_id, extension_version, kind, invocation,
			derived_scopes, input_ref, output_ref, latency_ms, cost_cents, policy_decision, created_at
		FROM extension_activity
		WHERE tenant_id = $1 AND workspace_id = $2 AND extension_id = $3
		ORDER BY created_at DESC
		LIMIT $4`,
		p.TenantID, p.WorkspaceID, extensionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ExtensionActivity
	for rows.Next() {
		var item domain.ExtensionActivity
		err := rows.Scan(&item.ID, &item.TenantID, &item.WorkspaceID, &item.ExtensionID, &item.ExtensionVersion, &item.Kind, &item.Invocation,
			&item.DerivedScopes, &item.InputRef, &item.OutputRef, &item.LatencyMs, &item.CostCents, &item.PolicyDecision, &item.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetExtensionHealth(ctx context.Context, p domain.Principal, extensionID string) (domain.ExtensionHealth, error) {
	var out domain.ExtensionHealth
	err := s.pool.QueryRow(ctx, `
		SELECT extension_id, tenant_id, state, consecutive_failures, opened_at, updated_at
		FROM extension_health
		WHERE tenant_id = $1 AND extension_id = $2`,
		p.TenantID, extensionID).
		Scan(&out.ExtensionID, &out.TenantID, &out.State, &out.ConsecutiveFailures, &out.OpenedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ExtensionHealth{
			ExtensionID:         extensionID,
			TenantID:            p.TenantID,
			State:               "closed",
			ConsecutiveFailures: 0,
		}, nil
	}
	if err != nil {
		return domain.ExtensionHealth{}, err
	}
	return out, nil
}

func (s *Store) UpdateExtensionHealth(ctx context.Context, p domain.Principal, health domain.ExtensionHealth) (domain.ExtensionHealth, error) {
	if health.ExtensionID == "" {
		return domain.ExtensionHealth{}, errors.New("extension ID is required")
	}
	var out domain.ExtensionHealth
	err := s.pool.QueryRow(ctx, `
		INSERT INTO extension_health (extension_id, tenant_id, state, consecutive_failures, opened_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (extension_id) DO UPDATE SET
			state = EXCLUDED.state,
			consecutive_failures = EXCLUDED.consecutive_failures,
			opened_at = EXCLUDED.opened_at,
			updated_at = now()
		RETURNING extension_id, tenant_id, state, consecutive_failures, opened_at, updated_at`,
		health.ExtensionID, p.TenantID, health.State, health.ConsecutiveFailures, health.OpenedAt).
		Scan(&out.ExtensionID, &out.TenantID, &out.State, &out.ConsecutiveFailures, &out.OpenedAt, &out.UpdatedAt)
	if err != nil {
		return domain.ExtensionHealth{}, err
	}
	return out, nil
}

func (s *Store) UpsertExtensionSubscriptions(ctx context.Context, p domain.Principal, extensionID string, eventTypes []string) error {
	if extensionID == "" {
		return errors.New("extension ID is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `DELETE FROM extension_subscriptions WHERE tenant_id = $1 AND extension_id = $2`, p.TenantID, extensionID)
	if err != nil {
		return err
	}

	for _, et := range eventTypes {
		_, err = tx.Exec(ctx, `INSERT INTO extension_subscriptions (extension_id, tenant_id, event_type) VALUES ($1, $2, $3)`,
			extensionID, p.TenantID, et)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) ListExtensionSubscriptions(ctx context.Context, p domain.Principal, extensionID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT event_type FROM extension_subscriptions WHERE tenant_id = $1 AND extension_id = $2`, p.TenantID, extensionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var et string
		if err := rows.Scan(&et); err != nil {
			return nil, err
		}
		out = append(out, et)
	}
	return out, rows.Err()
}

func (s *Store) GetExtensionBudgetUsage(ctx context.Context, tenantID, workspaceID, extensionID, period string) (int64, error) {
	var total int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(cost_cents), 0)
		FROM extension_activity
		WHERE tenant_id = $1 AND workspace_id = $2 AND extension_id = $3
		  AND to_char(created_at, 'YYYY-MM') = $4`,
		tenantID, workspaceID, extensionID, period).Scan(&total)
	return total, err
}

func (s *Store) GetExtensionInvocationCountLastMin(ctx context.Context, tenantID, workspaceID, extensionID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(1)
		FROM extension_activity
		WHERE tenant_id = $1 AND workspace_id = $2 AND extension_id = $3
		  AND created_at >= now() - interval '1 minute'`,
		tenantID, workspaceID, extensionID).Scan(&count)
	return count, err
}

func (s *Store) ListActiveChannelProvidersSystem(ctx context.Context) ([]domain.Extension, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.tenant_id, e.workspace_id, e.name, e.publisher, e.current_version_id, e.latest_version, e.status, e.created_at, e.updated_at
		FROM extensions e
		JOIN extension_versions ev ON e.current_version_id = ev.id
		WHERE e.status = 'enabled' AND ev.kind = 'channel_provider' AND ev.status = 'active'
	`)
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

func (s *Store) ListActiveIngestionTransforms(ctx context.Context, p domain.Principal, eventType string) ([]domain.Extension, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.tenant_id, e.workspace_id, e.name, e.publisher, e.current_version_id, e.latest_version, e.status, e.created_at, e.updated_at
		FROM extensions e
		JOIN extension_versions ev ON e.current_version_id = ev.id
		JOIN extension_subscriptions es ON e.id = es.extension_id
		WHERE e.tenant_id = $1 AND e.workspace_id = $2 AND e.status = 'enabled' AND ev.kind = 'ingestion_transform' AND ev.status = 'active' AND es.event_type = $3
		ORDER BY e.name
	`, p.TenantID, p.WorkspaceID, eventType)
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
