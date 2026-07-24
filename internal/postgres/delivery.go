package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) IsSuppressed(ctx context.Context, p domain.Principal, channel, endpoint string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM suppressions WHERE tenant_id=$1 AND channel=$2 AND endpoint=$3
	)`, p.TenantID, strings.ToLower(channel), strings.ToLower(endpoint)).Scan(&exists)
	return exists, err
}

func (s *Store) SuppressEndpoint(ctx context.Context, p domain.Principal, channel, endpoint, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if reason != "bounce" && reason != "complaint" && reason != "unsubscribe" && reason != "admin" {
		return errors.New("invalid suppression reason")
	}
	_, err = tx.Exec(ctx, `INSERT INTO suppressions (tenant_id, channel, endpoint, reason)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, channel, endpoint) DO UPDATE SET reason=EXCLUDED.reason`,
		p.TenantID, strings.ToLower(channel), strings.ToLower(endpoint), reason)
	if err != nil {
		return err
	}
	if err := s.audit(ctx, tx, p, "suppression.create", "suppression", endpoint, map[string]any{"channel": channel, "reason": reason}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Store) RemoveSuppression(ctx context.Context, p domain.Principal, channel, endpoint string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `DELETE FROM suppressions WHERE tenant_id=$1 AND channel=$2 AND endpoint=$3`,
		p.TenantID, strings.ToLower(channel), strings.ToLower(endpoint))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := s.audit(ctx, tx, p, "suppression.delete", "suppression", endpoint, map[string]any{"channel": channel}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Store) ListSuppressions(ctx context.Context, p domain.Principal) ([]domain.Suppression, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, channel, endpoint, reason, source_event_id, created_at
		FROM suppressions WHERE tenant_id=$1 ORDER BY created_at DESC`, p.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Suppression
	for rows.Next() {
		var sup domain.Suppression
		err := rows.Scan(&sup.ID, &sup.TenantID, &sup.Channel, &sup.Endpoint, &sup.Reason, &sup.SourceEventID, &sup.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, sup)
	}
	return out, nil
}

func (s *Store) LatestConsent(ctx context.Context, p domain.Principal, profileID, channel, topic string) (domain.Consent, error) {
	var out domain.Consent
	err := s.pool.QueryRow(ctx, `SELECT profile_id, channel, topic, state, occurred_at
		FROM consent_ledger
		WHERE tenant_id=$1 AND profile_id=$2 AND channel=$3 AND topic=$4
		ORDER BY occurred_at DESC LIMIT 1`,
		p.TenantID, profileID, strings.ToLower(channel), topic).
		Scan(&out.ProfileID, &out.Channel, &out.Topic, &out.State, &out.OccurredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Consent{}, ErrNotFound
	}
	return out, err
}

func (s *Store) SentCountSince(ctx context.Context, p domain.Principal, profileID string, since time.Time) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM (
		SELECT id FROM delivery_attempts
		WHERE tenant_id=$1 AND profile_id=$2 AND decision='sent' AND attempted_at>=$3
		UNION ALL
		SELECT id FROM journey_message_intents
		WHERE tenant_id=$1 AND profile_id=$2 AND decision='sent' AND updated_at>=$3
	) AS combined`,
		p.TenantID, profileID, since).Scan(&count)
	return count, err
}

func (s *Store) GetTenantFatigueQuotas(ctx context.Context, p domain.Principal) (int, int, error) {
	var maxSends24h, maxSends7d int
	err := s.pool.QueryRow(ctx, `SELECT max_sends_24h, max_sends_7d FROM tenant_quotas WHERE tenant_id=$1`, p.TenantID).Scan(&maxSends24h, &maxSends7d)
	if errors.Is(err, pgx.ErrNoRows) {
		return 5, 20, nil
	}
	if err != nil {
		return 0, 0, err
	}
	return maxSends24h, maxSends7d, nil
}

func (s *Store) GetTenantQuietHours(ctx context.Context, p domain.Principal) (*int, *int, string, error) {
	var start, end *int
	var tz string
	err := s.pool.QueryRow(ctx, `SELECT quiet_hours_start, quiet_hours_end, default_timezone FROM tenant_quotas WHERE tenant_id=$1`, p.TenantID).Scan(&start, &end, &tz)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, "UTC", nil
	}
	if err != nil {
		return nil, nil, "UTC", err
	}
	return start, end, tz, nil
}


