package postgres

import (
	"context"
	"errors"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ClaimOutboxEvent(ctx context.Context) (domain.OutboxEvent, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.OutboxEvent{}, false, err
	}
	defer tx.Rollback(ctx)
	var event domain.OutboxEvent
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,topic,partition_key,event_id,payload
		FROM outbox_events
		WHERE (status='pending' OR (status='processing' AND locked_until < now()))
		  AND available_at <= now()
		ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).
		Scan(&event.ID, &event.TenantID, &event.Topic, &event.PartitionKey, &event.EventID, &event.Payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.OutboxEvent{}, false, nil
	}
	if err != nil {
		return domain.OutboxEvent{}, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE outbox_events SET status='processing',
		attempts=attempts+1,locked_until=now()+interval '30 seconds' WHERE id=$1`, event.ID); err != nil {
		return domain.OutboxEvent{}, false, err
	}
	return event, true, tx.Commit(ctx)
}

func (s *Store) CompleteOutboxEvent(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE outbox_events SET status='published',published_at=now(),
		locked_until=NULL,last_error=NULL WHERE id=$1`, id)
	return err
}

func (s *Store) FailOutboxEvent(ctx context.Context, id string, publishErr error) error {
	message := publishErr.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	_, err := s.pool.Exec(ctx, `UPDATE outbox_events SET
		status=CASE WHEN attempts >= 10 THEN 'dead' ELSE 'pending' END,
		available_at=now()+(LEAST(attempts,10)*interval '5 seconds'),
		locked_until=NULL,last_error=$2 WHERE id=$1`, id, message)
	return err
}
