package postgres

import (
	"context"
	"encoding/json"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateInAppMessage(ctx context.Context, tenantID, workspaceID, appID, profileID string, msg domain.InAppMessage) (domain.InAppMessage, error) {
	var out domain.InAppMessage
	err := s.pool.QueryRow(ctx, `
		INSERT INTO inapp_messages (tenant_id, workspace_id, app_id, profile_id, template_id, campaign_id, journey_run_id, delivery_attempt_id, message_type, content, rank, categories, start_at, expires_at, idempotency_key, status, delivered_at, displayed_at, clicked_at, dismissed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		ON CONFLICT (tenant_id, profile_id, idempotency_key) DO UPDATE SET updated_at = now()
		RETURNING id, tenant_id, workspace_id, app_id, profile_id, template_id, campaign_id, journey_run_id, delivery_attempt_id, message_type, content, rank, categories, start_at, expires_at, idempotency_key, status, delivered_at, displayed_at, clicked_at, dismissed_at, created_at, updated_at
	`, tenantID, workspaceID, appID, profileID, msg.TemplateID, msg.CampaignID, msg.JourneyRunID, msg.DeliveryAttemptID, msg.MessageType, msg.Content, msg.Rank, msg.Categories, msg.StartAt, msg.ExpiresAt, msg.IdempotencyKey, msg.Status, msg.DeliveredAt, msg.DisplayedAt, msg.ClickedAt, msg.DismissedAt).Scan(
		&out.ID, &out.TenantID, &out.WorkspaceID, &out.AppID, &out.ProfileID, &out.TemplateID, &out.CampaignID, &out.JourneyRunID, &out.DeliveryAttemptID, &out.MessageType, &out.Content, &out.Rank, &out.Categories, &out.StartAt, &out.ExpiresAt, &out.IdempotencyKey, &out.Status, &out.DeliveredAt, &out.DisplayedAt, &out.ClickedAt, &out.DismissedAt, &out.CreatedAt, &out.UpdatedAt,
	)
	return out, err
}

func (s *Store) GetInAppMessage(ctx context.Context, tenantID, msgID string) (domain.InAppMessage, error) {
	var out domain.InAppMessage
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, workspace_id, app_id, profile_id, template_id, campaign_id, journey_run_id, delivery_attempt_id, message_type, content, rank, categories, start_at, expires_at, idempotency_key, status, delivered_at, displayed_at, clicked_at, dismissed_at, created_at, updated_at
		FROM inapp_messages
		WHERE id = $1 AND tenant_id = $2
	`, msgID, tenantID).Scan(
		&out.ID, &out.TenantID, &out.WorkspaceID, &out.AppID, &out.ProfileID, &out.TemplateID, &out.CampaignID, &out.JourneyRunID, &out.DeliveryAttemptID, &out.MessageType, &out.Content, &out.Rank, &out.Categories, &out.StartAt, &out.ExpiresAt, &out.IdempotencyKey, &out.Status, &out.DeliveredAt, &out.DisplayedAt, &out.ClickedAt, &out.DismissedAt, &out.CreatedAt, &out.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return out, ports.ErrNotFound
	}
	return out, err
}

func (s *Store) ListInboxForProfile(ctx context.Context, tenantID, appID, profileID string, limit int) ([]domain.InAppMessage, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, workspace_id, app_id, profile_id, template_id, campaign_id, journey_run_id, delivery_attempt_id, message_type, content, rank, categories, start_at, expires_at, idempotency_key, status, delivered_at, displayed_at, clicked_at, dismissed_at, created_at, updated_at
		FROM inapp_messages
		WHERE tenant_id = $1 AND app_id = $2 AND profile_id = $3 AND dismissed_at IS NULL
		  AND start_at <= now() AND (expires_at IS NULL OR expires_at > now())
		ORDER BY rank DESC
		LIMIT $4
	`, tenantID, appID, profileID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.InAppMessage
	for rows.Next() {
		var item domain.InAppMessage
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.WorkspaceID, &item.AppID, &item.ProfileID, &item.TemplateID, &item.CampaignID, &item.JourneyRunID, &item.DeliveryAttemptID, &item.MessageType, &item.Content, &item.Rank, &item.Categories, &item.StartAt, &item.ExpiresAt, &item.IdempotencyKey, &item.Status, &item.DeliveredAt, &item.DisplayedAt, &item.ClickedAt, &item.DismissedAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListInAppMessages(ctx context.Context, p domain.Principal, appID string) ([]domain.InAppMessage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, workspace_id, app_id, profile_id, template_id, campaign_id, journey_run_id, delivery_attempt_id, message_type, content, rank, categories, start_at, expires_at, idempotency_key, status, delivered_at, displayed_at, clicked_at, dismissed_at, created_at, updated_at
		FROM inapp_messages
		WHERE tenant_id = $1 AND workspace_id = $2 AND app_id = $3
		ORDER BY created_at DESC
	`, p.TenantID, p.WorkspaceID, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.InAppMessage
	for rows.Next() {
		var item domain.InAppMessage
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.WorkspaceID, &item.AppID, &item.ProfileID, &item.TemplateID, &item.CampaignID, &item.JourneyRunID, &item.DeliveryAttemptID, &item.MessageType, &item.Content, &item.Rank, &item.Categories, &item.StartAt, &item.ExpiresAt, &item.IdempotencyKey, &item.Status, &item.DeliveredAt, &item.DisplayedAt, &item.ClickedAt, &item.DismissedAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetProfileIDBySubject(ctx context.Context, tenantID, appID, subject string) (string, error) {
	var profileID string
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM profiles
		WHERE tenant_id = $1 AND app_id = $2 AND (external_id = $3 OR anonymous_id = $3)
		LIMIT 1
	`, tenantID, appID, subject).Scan(&profileID)
	if err == pgx.ErrNoRows {
		return "", ports.ErrNotFound
	}
	return profileID, err
}

func (s *Store) ExpireInAppMessages(ctx context.Context, tenantID string, limit int) error {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id FROM inapp_messages
		WHERE tenant_id = $1 AND expires_at IS NOT NULL AND expires_at <= now()
		  AND status != 'expired' AND status != 'dismissed'
		LIMIT $2
	`, tenantID, limit)
	if err != nil {
		return err
	}
	defer rows.Close()

	var events []domain.Event
	for rows.Next() {
		var msgID string
		if err := rows.Scan(&msgID, &tenantID); err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]string{"message_id": msgID})
		if err != nil {
			return err
		}
		events = append(events, domain.Event{
			Type:    "message.expire",
			Payload: payload,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(events) > 0 {
		_, err = s.AcceptEvents(ctx, domain.Principal{
			TenantID:  tenantID,
			ActorType: "service",
		}, events)
	}
	return err
}
