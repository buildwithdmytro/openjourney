package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateSendingIdentity(ctx context.Context, p domain.Principal, iden domain.SendingIdentity) (domain.SendingIdentity, error) {
	if iden.Channel == "" {
		return domain.SendingIdentity{}, errors.New("channel is required")
	}
	if iden.Provider == "" {
		iden.Provider = "ses"
	}
	if len(iden.Config) == 0 {
		iden.Config = []byte("{}")
	}
	if iden.MaxSendRate <= 0 {
		iden.MaxSendRate = 14
	}
	var out domain.SendingIdentity
	err := s.pool.QueryRow(ctx, `INSERT INTO sending_identities (tenant_id, workspace_id, channel, from_address, from_name, reply_to, provider, config, max_send_rate, verified)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, tenant_id, workspace_id, channel, from_address, from_name, reply_to, provider, config, max_send_rate, verified, created_at`,
		p.TenantID, p.WorkspaceID, iden.Channel, iden.FromAddress, iden.FromName, iden.ReplyTo, iden.Provider, iden.Config, iden.MaxSendRate, iden.Verified).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Channel, &out.FromAddress, &out.FromName, &out.ReplyTo, &out.Provider, &out.Config, &out.MaxSendRate, &out.Verified, &out.CreatedAt)
	if err != nil {
		return domain.SendingIdentity{}, err
	}
	_ = s.audit(ctx, p, "sending_identity.create", "sending_identity", out.ID, map[string]any{"channel": out.Channel})
	return out, nil
}

func (s *Store) GetSendingIdentity(ctx context.Context, p domain.Principal, id string) (domain.SendingIdentity, error) {
	var out domain.SendingIdentity
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, channel, from_address, from_name, reply_to, provider, config, max_send_rate, verified, created_at
		FROM sending_identities WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, id).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Channel, &out.FromAddress, &out.FromName, &out.ReplyTo, &out.Provider, &out.Config, &out.MaxSendRate, &out.Verified, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SendingIdentity{}, ErrNotFound
	}
	return out, err
}

func (s *Store) ListSendingIdentities(ctx context.Context, p domain.Principal) ([]domain.SendingIdentity, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, workspace_id, channel, from_address, from_name, reply_to, provider, config, max_send_rate, verified, created_at
		FROM sending_identities WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY created_at DESC`,
		p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SendingIdentity
	for rows.Next() {
		var iden domain.SendingIdentity
		err := rows.Scan(&iden.ID, &iden.TenantID, &iden.WorkspaceID, &iden.Channel, &iden.FromAddress, &iden.FromName, &iden.ReplyTo, &iden.Provider, &iden.Config, &iden.MaxSendRate, &iden.Verified, &iden.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, iden)
	}
	return out, rows.Err()
}

func (s *Store) GetSendingIdentityByProviderConfig(ctx context.Context, provider string, configKey string, configVal string) (domain.SendingIdentity, error) {
	var out domain.SendingIdentity
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, channel, from_address, from_name, reply_to, provider, config, max_send_rate, verified, created_at
		FROM sending_identities WHERE provider=$1 AND config->>$2 = $3 LIMIT 1`,
		provider, configKey, configVal).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Channel, &out.FromAddress, &out.FromName, &out.ReplyTo, &out.Provider, &out.Config, &out.MaxSendRate, &out.Verified, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SendingIdentity{}, ErrNotFound
	}
	return out, err
}


func (s *Store) CreateTemplate(ctx context.Context, p domain.Principal, tmpl domain.Template) (domain.Template, error) {
	if tmpl.Name == "" {
		return domain.Template{}, errors.New("name is required")
	}
	if tmpl.Channel == "" {
		return domain.Template{}, errors.New("channel is required")
	}
	var pushDataBytes []byte
	if tmpl.PushData != nil {
		pushDataBytes, _ = json.Marshal(tmpl.PushData)
	}
	var out domain.Template
	var outPushData []byte
	err := s.pool.QueryRow(ctx, `INSERT INTO templates (tenant_id, workspace_id, name, channel, subject_template, html_template, text_template, body_template, title_template, push_data, sending_identity_id, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 1)
		RETURNING id, tenant_id, workspace_id, name, channel, subject_template, html_template, text_template, body_template, title_template, push_data, sending_identity_id, version, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, tmpl.Name, tmpl.Channel, tmpl.SubjectTemplate, tmpl.HTMLTemplate, tmpl.TextTemplate, tmpl.BodyTemplate, tmpl.TitleTemplate, pushDataBytes, tmpl.SendingIdentityID).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Channel, &out.SubjectTemplate, &out.HTMLTemplate, &out.TextTemplate, &out.BodyTemplate, &out.TitleTemplate, &outPushData, &out.SendingIdentityID, &out.Version, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.Template{}, err
	}
	if len(outPushData) > 0 {
		_ = json.Unmarshal(outPushData, &out.PushData)
	}
	_ = s.audit(ctx, p, "template.create", "template", out.ID, map[string]any{"name": out.Name})
	return out, nil
}

func (s *Store) GetTemplate(ctx context.Context, p domain.Principal, id string) (domain.Template, error) {
	var out domain.Template
	var outPushData []byte
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, name, channel, subject_template, html_template, text_template, body_template, title_template, push_data, sending_identity_id, version, created_at, updated_at
		FROM templates WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, id).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Channel, &out.SubjectTemplate, &out.HTMLTemplate, &out.TextTemplate, &out.BodyTemplate, &out.TitleTemplate, &outPushData, &out.SendingIdentityID, &out.Version, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Template{}, ErrNotFound
	}
	if err != nil {
		return domain.Template{}, err
	}
	if len(outPushData) > 0 {
		_ = json.Unmarshal(outPushData, &out.PushData)
	}
	return out, nil
}

func (s *Store) UpdateTemplate(ctx context.Context, p domain.Principal, tmpl domain.Template) (domain.Template, error) {
	existing, err := s.GetTemplate(ctx, p, tmpl.ID)
	if err != nil {
		return domain.Template{}, err
	}

	version := existing.Version
	pushDataChanged := false
	existingJSON, _ := json.Marshal(existing.PushData)
	newJSON, _ := json.Marshal(tmpl.PushData)
	if string(existingJSON) != string(newJSON) {
		pushDataChanged = true
	}

	if valueOrEmpty(existing.SubjectTemplate) != valueOrEmpty(tmpl.SubjectTemplate) ||
		valueOrEmpty(existing.HTMLTemplate) != valueOrEmpty(tmpl.HTMLTemplate) ||
		valueOrEmpty(existing.TextTemplate) != valueOrEmpty(tmpl.TextTemplate) ||
		valueOrEmpty(existing.BodyTemplate) != valueOrEmpty(tmpl.BodyTemplate) ||
		valueOrEmpty(existing.TitleTemplate) != valueOrEmpty(tmpl.TitleTemplate) ||
		pushDataChanged {
		version++
	}

	var pushDataBytes []byte
	if tmpl.PushData != nil {
		pushDataBytes, _ = json.Marshal(tmpl.PushData)
	}

	var out domain.Template
	var outPushData []byte
	err = s.pool.QueryRow(ctx, `UPDATE templates
		SET name=$1, subject_template=$2, html_template=$3, text_template=$4, body_template=$5, title_template=$6, push_data=$7, sending_identity_id=$8, version=$9, updated_at=now()
		WHERE tenant_id=$10 AND workspace_id=$11 AND id=$12
		RETURNING id, tenant_id, workspace_id, name, channel, subject_template, html_template, text_template, body_template, title_template, push_data, sending_identity_id, version, created_at, updated_at`,
		tmpl.Name, tmpl.SubjectTemplate, tmpl.HTMLTemplate, tmpl.TextTemplate, tmpl.BodyTemplate, tmpl.TitleTemplate, pushDataBytes, tmpl.SendingIdentityID, version, p.TenantID, p.WorkspaceID, tmpl.ID).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Channel, &out.SubjectTemplate, &out.HTMLTemplate, &out.TextTemplate, &out.BodyTemplate, &out.TitleTemplate, &outPushData, &out.SendingIdentityID, &out.Version, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Template{}, ErrNotFound
	}
	if err != nil {
		return domain.Template{}, err
	}
	if len(outPushData) > 0 {
		_ = json.Unmarshal(outPushData, &out.PushData)
	}
	_ = s.audit(ctx, p, "template.update", "template", out.ID, map[string]any{"name": out.Name})
	return out, nil
}

func (s *Store) ListTemplates(ctx context.Context, p domain.Principal) ([]domain.Template, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, workspace_id, name, channel, subject_template, html_template, text_template, body_template, title_template, push_data, sending_identity_id, version, created_at, updated_at
		FROM templates WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY name`,
		p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Template
	for rows.Next() {
		var tmpl domain.Template
		var outPushData []byte
		err := rows.Scan(&tmpl.ID, &tmpl.TenantID, &tmpl.WorkspaceID, &tmpl.Name, &tmpl.Channel, &tmpl.SubjectTemplate, &tmpl.HTMLTemplate, &tmpl.TextTemplate, &tmpl.BodyTemplate, &tmpl.TitleTemplate, &outPushData, &tmpl.SendingIdentityID, &tmpl.Version, &tmpl.CreatedAt, &tmpl.UpdatedAt)
		if err != nil {
			return nil, err
		}
		if len(outPushData) > 0 {
			_ = json.Unmarshal(outPushData, &tmpl.PushData)
		}
		out = append(out, tmpl)
	}
	return out, rows.Err()
}

func valueOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Store) UpsertTrackedLink(ctx context.Context, tenantID string, templateID string, originalURL string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `INSERT INTO tracked_links (tenant_id, template_id, original_url)
		VALUES ($1, $2, $3)
		ON CONFLICT (template_id, original_url) DO UPDATE SET original_url = EXCLUDED.original_url
		RETURNING id`,
		tenantID, templateID, originalURL).Scan(&id)
	return id, err
}
