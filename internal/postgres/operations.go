package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ClaimOperationJob(ctx context.Context) (domain.OperationJob, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.OperationJob{}, false, err
	}
	defer tx.Rollback(ctx)
	var job domain.OperationJob
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,workspace_id,job_type,payload
		FROM operation_jobs
		WHERE (status='pending' OR (status='processing' AND locked_until < now()))
		  AND available_at <= now()
		ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).
		Scan(&job.ID, &job.TenantID, &job.WorkspaceID, &job.Type, &job.Payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.OperationJob{}, false, nil
	}
	if err != nil {
		return domain.OperationJob{}, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE operation_jobs SET status='processing',attempts=attempts+1,
		locked_until=now()+interval '5 minutes' WHERE id=$1`, job.ID); err != nil {
		return domain.OperationJob{}, false, err
	}
	return job, true, tx.Commit(ctx)
}

func (s *Store) CompleteOperationJob(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE operation_jobs SET status='done',completed_at=now(),
		locked_until=NULL,last_error=NULL WHERE id=$1`, id)
	return err
}

func (s *Store) FailOperationJob(ctx context.Context, id string, operationErr error) error {
	message := operationErr.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	var payload json.RawMessage
	var jobType string
	err = tx.QueryRow(ctx, `UPDATE operation_jobs SET
		status=CASE WHEN attempts >= 10 THEN 'dead' ELSE 'pending' END,
		available_at=now()+(LEAST(attempts,10)*interval '30 seconds'),
		locked_until=NULL,last_error=$2 WHERE id=$1 RETURNING status,job_type,payload`, id, message).
		Scan(&status, &jobType, &payload)
	if err != nil {
		return err
	}
	var terminal domain.TerminalOperationError
	if errors.As(operationErr, &terminal) && terminal.TerminalOperation() {
		if _, err := tx.Exec(ctx, `UPDATE operation_jobs SET status='dead',available_at=now(),
			locked_until=NULL,last_error=$2 WHERE id=$1`, id, message); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if status == "dead" {
		var input struct {
			RequestID string `json:"request_id"`
		}
		if json.Unmarshal(payload, &input) == nil && input.RequestID != "" {
			// Connector run records are append-only. Their executor owns the
			// terminal connector_runs row; do not fall through to one of the
			// request-table updates below when a connector job exhausts retries.
			if jobType == "warehouse.sync" || jobType == "reverse_etl.run" || jobType == "export.replay" {
				return tx.Commit(ctx)
			}
			if jobType == "profiles.import" {
				if _, err := tx.Exec(ctx, `UPDATE import_requests SET status='failed',error=$2,completed_at=now() WHERE id=$1`, input.RequestID, message); err != nil {
					return err
				}
			}
			if jobType == "ai.generate" {
				if _, err := tx.Exec(ctx, `UPDATE ai_generation_requests SET status='failed',error=$2,
					completed_at=now() WHERE id=$1`, input.RequestID, message); err != nil {
					return err
				}
			}
			if jobType == "scores.compute" {
				if _, err := tx.Exec(ctx, `UPDATE scoring_requests SET status='failed',error=$2,
					completed_at=now() WHERE id=$1`, input.RequestID, message); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE privacy_requests SET status='failed',error=$2,
				completed_at=now() WHERE id=$1`, input.RequestID, message); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ExportPrivacyData(ctx context.Context, requestID string) (domain.PrivacyData, error) {
	var data domain.PrivacyData
	var appID, externalID string
	err := s.pool.QueryRow(ctx, `SELECT tenant_id,app_id,external_id FROM privacy_requests
		WHERE id=$1 AND request_type='export'`, requestID).Scan(&data.TenantID, &appID, &externalID)
	if err != nil {
		return domain.PrivacyData{}, err
	}
	data.RequestID = requestID
	data.ExportedAt = time.Now().UTC()
	err = s.pool.QueryRow(ctx, `SELECT id,COALESCE(external_id,''),COALESCE(anonymous_id,''),
		attributes,version,updated_at FROM profiles
		WHERE tenant_id=$1 AND app_id=$2 AND external_id=$3`,
		data.TenantID, appID, externalID).
		Scan(&data.Profile.ID, &data.Profile.ExternalID, &data.Profile.AnonymousID,
			&data.Profile.Attributes, &data.Profile.Version, &data.Profile.UpdatedAt)
	if err != nil {
		return domain.PrivacyData{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT profile_id,channel,topic,state,occurred_at
		FROM consent_ledger WHERE tenant_id=$1 AND profile_id=$2 ORDER BY occurred_at`,
		data.TenantID, data.Profile.ID)
	if err != nil {
		return domain.PrivacyData{}, err
	}
	for rows.Next() {
		var consent domain.Consent
		if err := rows.Scan(&consent.ProfileID, &consent.Channel, &consent.Topic, &consent.State, &consent.OccurredAt); err != nil {
			rows.Close()
			return domain.PrivacyData{}, err
		}
		data.Consents = append(data.Consents, consent)
	}
	rows.Close()
	var events []json.RawMessage
	eventRows, err := s.pool.Query(ctx, `SELECT jsonb_build_object(
		'event_id',id,'event_type',event_type,'schema_version',schema_version,
		'occurred_at',occurred_at,'received_at',received_at,'payload',payload)
		FROM accepted_events WHERE tenant_id=$1 AND app_id=$2 AND external_id=$3 ORDER BY occurred_at`,
		data.TenantID, appID, externalID)
	if err != nil {
		return domain.PrivacyData{}, err
	}
	defer eventRows.Close()
	for eventRows.Next() {
		var event json.RawMessage
		if err := eventRows.Scan(&event); err != nil {
			return domain.PrivacyData{}, err
		}
		events = append(events, event)
	}
	data.Events, _ = json.Marshal(events)
	return data, eventRows.Err()
}

func (s *Store) CompletePrivacyExport(ctx context.Context, requestID, artifactKey string) error {
	_, err := s.pool.Exec(ctx, `UPDATE privacy_requests SET status='complete',artifact_key=$2,
		completed_at=now() WHERE id=$1`, requestID, artifactKey)
	return err
}

func (s *Store) DeletePrivacyData(ctx context.Context, requestID string) ([]string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var tenantID, workspaceID, appID, externalID string
	if err := tx.QueryRow(ctx, `SELECT tenant_id,app_id,external_id FROM privacy_requests
		WHERE id=$1 AND request_type='delete' FOR UPDATE`, requestID).
		Scan(&tenantID, &appID, &externalID); err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, "SELECT workspace_id FROM applications WHERE id=$1", appID).Scan(&workspaceID); err != nil {
		return nil, err
	}
	var profileID string
	err = tx.QueryRow(ctx, `SELECT id FROM profiles WHERE tenant_id=$1 AND app_id=$2 AND external_id=$3`,
		tenantID, appID, externalID).Scan(&profileID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id,occurred_at FROM accepted_events
		WHERE tenant_id=$1 AND app_id=$2 AND external_id=$3`, tenantID, appID, externalID)
	if err != nil {
		return nil, err
	}
	var eventIDs []string
	for rows.Next() {
		var id string
		var occurredAt time.Time
		if err := rows.Scan(&id, &occurredAt); err != nil {
			rows.Close()
			return nil, err
		}
		eventIDs = append(eventIDs, "events/"+tenantID+"/"+occurredAt.UTC().Format("2006/01/02")+"/"+id+".json")
	}
	rows.Close()
	if profileID != "" {
		// Enable erasure mode for the identity_merges trigger
		if _, err := tx.Exec(ctx, "SET LOCAL openjourney.erasure='on'"); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM identity_merges WHERE target_profile_id=$1 OR source_profile_id=$1", profileID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM consent_ledger WHERE profile_id=$1", profileID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM identity_aliases WHERE profile_id=$1", profileID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM profiles WHERE id=$1", profileID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM outbox_events WHERE event_id IN (
		SELECT id FROM accepted_events WHERE tenant_id=$1 AND app_id=$2 AND external_id=$3
	)`, tenantID, appID, externalID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM accepted_events
		WHERE tenant_id=$1 AND app_id=$2 AND external_id=$3`, tenantID, appID, externalID); err != nil {
		return nil, err
	}
	var subjectHash string
	if err := tx.QueryRow(ctx, "SELECT encode(digest($1,'sha256'),'hex')", externalID).Scan(&subjectHash); err != nil {
		return nil, err
	}
	tombstonePayload, _ := json.Marshal(map[string]any{
		"subject_hash": subjectHash, "object_keys": eventIDs, "privacy_request_id": requestID,
	})
	var tombstoneID string
	if err := tx.QueryRow(ctx, `INSERT INTO accepted_events
		(tenant_id,workspace_id,app_id,event_type,schema_version,idempotency_key,occurred_at,
		 source,data_classification,payload)
		VALUES($1,$2,$3,'privacy.deleted',1,$4,now(),'system','restricted',$5)
		RETURNING id`, tenantID, workspaceID, appID, "privacy-delete-"+requestID, tombstonePayload).Scan(&tombstoneID); err != nil {
		return nil, err
	}
	envelope, _ := json.Marshal(map[string]any{
		"event_id": tombstoneID, "tenant_id": tenantID, "workspace_id": workspaceID, "app_id": appID,
		"event_type": "privacy.deleted", "schema_version": 1, "occurred_at": time.Now().UTC(),
		"received_at": time.Now().UTC(), "source": "system", "data_classification": "restricted",
		"payload": json.RawMessage(tombstonePayload),
	})
	if _, err := tx.Exec(ctx, `INSERT INTO outbox_events
		(tenant_id,topic,partition_key,event_id,payload)
		VALUES($1,'events.accepted.v1',$2,$3,$4)`, tenantID, subjectHash, tombstoneID, envelope); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE privacy_requests SET status='complete',completed_at=now()
		WHERE id=$1`, requestID); err != nil {
		return nil, err
	}
	return eventIDs, tx.Commit(ctx)
}

func (s *Store) EnforceRetention(ctx context.Context, tenantID string) (domain.RetentionReport, error) {
	var retentionDays int
	if err := s.pool.QueryRow(ctx, `SELECT retention_days FROM tenant_quotas WHERE tenant_id=$1`, tenantID).
		Scan(&retentionDays); err != nil {
		return domain.RetentionReport{}, err
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
	var deletedEvents int64
	if err := s.pool.QueryRow(ctx, `WITH expired AS MATERIALIZED (
			SELECT id FROM accepted_events WHERE tenant_id=$1 AND received_at < $2
		), deleted_outbox AS (
			DELETE FROM outbox_events WHERE event_id IN (SELECT id FROM expired)
		), deleted_events AS (
			DELETE FROM accepted_events WHERE id IN (SELECT id FROM expired) RETURNING 1
		)
		SELECT count(*) FROM deleted_events`, tenantID, cutoff).Scan(&deletedEvents); err != nil {
		return domain.RetentionReport{}, err
	}
	return domain.RetentionReport{
		TenantID: tenantID, RetentionDays: retentionDays, Cutoff: cutoff, DeletedEvents: deletedEvents,
	}, nil
}
