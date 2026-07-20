package postgres

import (
	"context"
	"errors"
	"time"

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
		WHERE topic NOT LIKE 'exports.%'
		  AND (status='pending' OR (status='processing' AND locked_until < now()))
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

// ClaimExportOutboxEvent is the export relay's topic-specific leased claim.
// Keeping exports out of the general dispatcher gives each configured sink its
// own bounded delivery path while retaining the outbox lease semantics.
func (s *Store) ClaimExportOutboxEvent(ctx context.Context) (domain.OutboxEvent, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.OutboxEvent{}, false, err
	}
	defer tx.Rollback(ctx)
	var event domain.OutboxEvent
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,topic,partition_key,event_id,payload
		FROM outbox_events WHERE topic='exports.events.v1'
		AND (status='pending' OR (status='processing' AND locked_until < now()))
		AND available_at <= now() ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).
		Scan(&event.ID, &event.TenantID, &event.Topic, &event.PartitionKey, &event.EventID, &event.Payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.OutboxEvent{}, false, nil
	}
	if err != nil {
		return domain.OutboxEvent{}, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE outbox_events SET status='processing', attempts=attempts+1,
		locked_until=now()+interval '30 seconds' WHERE id=$1`, event.ID); err != nil {
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

// ReplayExportEvents re-emits accepted events in a bounded time window. The
// topic/event unique constraint makes replay safe alongside live fan-out.
func (s *Store) ReplayExportEvents(ctx context.Context, tenantID, workspaceID, appID, pipelineID string, from, to time.Time) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `WITH inserted AS (
		INSERT INTO outbox_events (tenant_id,topic,partition_key,event_id,payload)
		SELECT e.tenant_id, 'exports.events.v1', COALESCE(e.external_id,e.anonymous_id,''), e.id,
			jsonb_build_object('event_id',e.id,'tenant_id',e.tenant_id,'workspace_id',e.workspace_id,
			'app_id',e.app_id,'event_type',e.event_type,'schema_version',e.schema_version,
			'external_id',e.external_id,'anonymous_id',e.anonymous_id,'occurred_at',e.occurred_at,
			'received_at',e.received_at,'source',e.source,'source_event_id',e.source_event_id,
			'correlation_id',e.correlation_id,'causation_id',e.causation_id,'traceparent',e.traceparent,
			'data_classification',e.data_classification,'consent_context',e.consent_context,
			'payload',e.payload,'export_pipeline_ids',jsonb_build_array($4::text))
		FROM accepted_events e
		WHERE e.tenant_id=$1 AND e.workspace_id=$2 AND e.app_id=$3
		  AND ($5::timestamptz IS NULL OR e.occurred_at >= $5)
		  AND ($6::timestamptz IS NULL OR e.occurred_at < $6)
		ON CONFLICT(topic,event_id) DO NOTHING
		RETURNING id
	) SELECT count(*) FROM inserted`, tenantID, workspaceID, appID, pipelineID, nullableTime(from), nullableTime(to)).Scan(&count)
	return count, err
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
