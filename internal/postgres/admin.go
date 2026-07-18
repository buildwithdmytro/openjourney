package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/schemas"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ValidateEventSchema(ctx context.Context, p domain.Principal, event domain.Event) error {
	key := p.TenantID + ":" + p.WorkspaceID + ":" + event.Type + ":" + fmt.Sprint(event.SchemaVersion)
	s.schemaMu.RLock()
	validator, known := s.schemaCache[key], s.schemaCacheKnown[key]
	s.schemaMu.RUnlock()
	if known {
		if validator == nil {
			if isBuiltInEvent(event.Type) {
				return nil
			}
			return fmt.Errorf("event schema %s v%d is not registered", event.Type, event.SchemaVersion)
		}
		return validator.Validate(event.Payload)
	}
	var schema json.RawMessage
	err := s.pool.QueryRow(ctx, `SELECT schema FROM event_schemas
		WHERE tenant_id=$1 AND workspace_id=$2 AND event_type=$3 AND version=$4 AND status='active'`,
		p.TenantID, p.WorkspaceID, event.Type, event.SchemaVersion).Scan(&schema)
	if errors.Is(err, pgx.ErrNoRows) {
		s.schemaMu.Lock()
		s.schemaCacheKnown[key] = true
		s.schemaCache[key] = nil
		s.schemaMu.Unlock()
		if isBuiltInEvent(event.Type) {
			return nil
		}
		return fmt.Errorf("event schema %s v%d is not registered", event.Type, event.SchemaVersion)
	}
	if err != nil {
		return err
	}
	validator, err = schemas.Compile(schema)
	if err != nil {
		return err
	}
	s.schemaMu.Lock()
	s.schemaCacheKnown[key] = true
	s.schemaCache[key] = validator
	s.schemaMu.Unlock()
	return validator.Validate(event.Payload)
}

func (s *Store) ListEventSchemas(ctx context.Context, p domain.Principal) ([]domain.EventSchema, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,event_type,version,schema,status,compatibility,created_at
		FROM event_schemas WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY event_type,version`,
		p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.EventSchema
	for rows.Next() {
		var item domain.EventSchema
		if err := rows.Scan(&item.ID, &item.EventType, &item.Version, &item.Schema, &item.Status,
			&item.Compatibility, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) CreateEventSchema(ctx context.Context, p domain.Principal, input domain.EventSchema) (domain.EventSchema, error) {
	if input.EventType == "" || input.Version < 1 {
		return domain.EventSchema{}, errors.New("event_type and positive version are required")
	}
	if input.Compatibility == "" {
		input.Compatibility = "backward"
	}
	if input.Compatibility != "none" && input.Compatibility != "backward" {
		return domain.EventSchema{}, errors.New("compatibility must be none or backward")
	}
	// Compiling validates the schema itself without incorrectly requiring an empty object to satisfy it.
	if err := schemas.ValidateDefinition(input.Schema); err != nil {
		return domain.EventSchema{}, err
	}
	var previous json.RawMessage
	err := s.pool.QueryRow(ctx, `SELECT schema FROM event_schemas
		WHERE tenant_id=$1 AND workspace_id=$2 AND event_type=$3
		ORDER BY version DESC LIMIT 1`, p.TenantID, p.WorkspaceID, input.EventType).Scan(&previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.EventSchema{}, err
	}
	if err == nil && input.Compatibility == "backward" {
		if err := schemas.BackwardCompatible(previous, input.Schema); err != nil {
			return domain.EventSchema{}, fmt.Errorf("incompatible schema: %w", err)
		}
	}
	actor := actorID(p)
	err = s.pool.QueryRow(ctx, `INSERT INTO event_schemas
		(tenant_id,workspace_id,event_type,version,schema,compatibility,created_by)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		RETURNING id,event_type,version,schema,status,compatibility,created_at`,
		p.TenantID, p.WorkspaceID, input.EventType, input.Version, input.Schema, input.Compatibility, actor).
		Scan(&input.ID, &input.EventType, &input.Version, &input.Schema, &input.Status, &input.Compatibility, &input.CreatedAt)
	if err != nil {
		return domain.EventSchema{}, err
	}
	validator, _ := schemas.Compile(input.Schema)
	key := p.TenantID + ":" + p.WorkspaceID + ":" + input.EventType + ":" + fmt.Sprint(input.Version)
	s.schemaMu.Lock()
	s.schemaCacheKnown[key] = true
	s.schemaCache[key] = validator
	s.schemaMu.Unlock()
	_ = s.audit(ctx, p, "schema.create", "event_schema", input.ID, map[string]any{
		"event_type": input.EventType, "version": input.Version,
	})
	return input, nil
}

func isBuiltInEvent(eventType string) bool {
	return eventType == "profile.updated" || eventType == "consent.changed" ||
		eventType == "form.submitted" ||
		eventType == "stage.changed" ||
		eventType == "identity.alias" || eventType == "identity.merge" ||
		eventType == "email.sent" || eventType == "email.opened" || eventType == "link.clicked" ||
		eventType == "message.sent" || eventType == "message.delivered" ||
		eventType == "message.bounced" || eventType == "message.complained" ||
		eventType == "ai.action"
}

func (s *Store) ListAPIKeys(ctx context.Context, p domain.Principal) ([]domain.APIKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,name,scopes,expires_at,revoked_at,last_used_at,created_at
		FROM api_keys WHERE tenant_id=$1 AND workspace_id=$2 AND app_id=$3 ORDER BY created_at DESC`,
		p.TenantID, p.WorkspaceID, p.AppID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.APIKey
	for rows.Next() {
		var item domain.APIKey
		if err := rows.Scan(&item.ID, &item.Name, &item.Scopes, &item.ExpiresAt, &item.RevokedAt, &item.LastUsedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) CreateAPIKey(ctx context.Context, p domain.Principal, name string, scopes []string, expiresAt *time.Time) (domain.APIKey, string, error) {
	if name == "" || len(scopes) == 0 {
		return domain.APIKey{}, "", errors.New("name and at least one scope are required")
	}
	for _, scope := range scopes {
		if _, exists := allowedPermissions[scope]; !exists {
			return domain.APIKey{}, "", errors.New("unknown scope: " + scope)
		}
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return domain.APIKey{}, "", err
	}
	raw := "oj_" + base64.RawURLEncoding.EncodeToString(random)
	hash := sha256.Sum256([]byte(raw))
	var item domain.APIKey
	err := s.pool.QueryRow(ctx, `INSERT INTO api_keys
		(tenant_id,workspace_id,app_id,name,key_hash,scopes,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		RETURNING id,name,scopes,expires_at,revoked_at,last_used_at,created_at`,
		p.TenantID, p.WorkspaceID, p.AppID, name, hash[:], scopes, expiresAt).
		Scan(&item.ID, &item.Name, &item.Scopes, &item.ExpiresAt, &item.RevokedAt, &item.LastUsedAt, &item.CreatedAt)
	if err != nil {
		return domain.APIKey{}, "", err
	}
	_ = s.audit(ctx, p, "api_key.create", "api_key", item.ID, map[string]any{"scopes": scopes})
	return item, raw, nil
}

func (s *Store) RevokeAPIKey(ctx context.Context, p domain.Principal, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE api_keys SET revoked_at=now()
		WHERE id=$1 AND tenant_id=$2 AND workspace_id=$3 AND app_id=$4 AND revoked_at IS NULL`,
		id, p.TenantID, p.WorkspaceID, p.AppID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return s.audit(ctx, p, "api_key.revoke", "api_key", id, nil)
}

func (s *Store) CreatePrivacyRequest(ctx context.Context, p domain.Principal, externalID, requestType string) (domain.PrivacyRequest, error) {
	if externalID == "" || (requestType != "export" && requestType != "delete") {
		return domain.PrivacyRequest{}, errors.New("external_id and export/delete request_type are required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.PrivacyRequest{}, err
	}
	defer tx.Rollback(ctx)
	var request domain.PrivacyRequest
	err = tx.QueryRow(ctx, `INSERT INTO privacy_requests
		(tenant_id,workspace_id,app_id,external_id,request_type,requested_by)
		VALUES($1,$2,$3,$4,$5,$6)
		RETURNING id,external_id,request_type,status,created_at`,
		p.TenantID, p.WorkspaceID, p.AppID, externalID, requestType, actorID(p)).
		Scan(&request.ID, &request.ExternalID, &request.RequestType, &request.Status, &request.CreatedAt)
	if err != nil {
		return domain.PrivacyRequest{}, err
	}
	payload, _ := json.Marshal(map[string]string{"request_id": request.ID})
	if _, err := tx.Exec(ctx, `INSERT INTO operation_jobs(tenant_id,workspace_id,job_type,payload)
		VALUES($1,$2,$3,$4)`, p.TenantID, p.WorkspaceID, "privacy."+requestType, payload); err != nil {
		return domain.PrivacyRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.PrivacyRequest{}, err
	}
	_ = s.audit(ctx, p, "privacy."+requestType, "privacy_request", request.ID,
		map[string]any{"external_id": externalID})
	return request, nil
}

func (s *Store) GetPrivacyRequest(ctx context.Context, p domain.Principal, id string) (domain.PrivacyRequest, error) {
	var item domain.PrivacyRequest
	err := s.pool.QueryRow(ctx, `SELECT id,external_id,request_type,status,
		COALESCE(artifact_key,''),COALESCE(error,''),created_at,completed_at
		FROM privacy_requests WHERE id=$1 AND tenant_id=$2 AND workspace_id=$3`,
		id, p.TenantID, p.WorkspaceID).
		Scan(&item.ID, &item.ExternalID, &item.RequestType, &item.Status,
			&item.ArtifactKey, &item.Error, &item.CreatedAt, &item.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PrivacyRequest{}, ErrNotFound
	}
	return item, err
}

func (s *Store) CreateAIGenerationRequest(ctx context.Context, p domain.Principal, taskType string, input json.RawMessage) (domain.AIGenerationRequest, error) {
	if strings.TrimSpace(taskType) == "" {
		return domain.AIGenerationRequest{}, errors.New("task_type is required")
	}
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	if !json.Valid(input) {
		return domain.AIGenerationRequest{}, errors.New("input must be valid JSON")
	}
	requestedBy := actorID(p)
	if requestedBy == "" {
		return domain.AIGenerationRequest{}, errors.New("a requesting user or API key is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AIGenerationRequest{}, err
	}
	defer tx.Rollback(ctx)
	var request domain.AIGenerationRequest
	err = tx.QueryRow(ctx, `INSERT INTO ai_generation_requests
		(tenant_id,workspace_id,requested_by,task_type)
		VALUES($1,$2,$3,$4)
		RETURNING id,tenant_id,workspace_id,requested_by,task_type,status,created_at`,
		p.TenantID, p.WorkspaceID, requestedBy, taskType).
		Scan(&request.ID, &request.TenantID, &request.WorkspaceID, &request.RequestedBy,
			&request.TaskType, &request.Status, &request.CreatedAt)
	if err != nil {
		return domain.AIGenerationRequest{}, err
	}
	payload, err := json.Marshal(struct {
		RequestID string          `json:"request_id"`
		TaskType  string          `json:"task_type"`
		Input     json.RawMessage `json:"input"`
		Scopes    []string        `json:"scopes"`
	}{request.ID, taskType, input, p.Scopes})
	if err != nil {
		return domain.AIGenerationRequest{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO operation_jobs(tenant_id,workspace_id,job_type,payload)
		VALUES($1,$2,'ai.generate',$3)`, p.TenantID, p.WorkspaceID, payload); err != nil {
		return domain.AIGenerationRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AIGenerationRequest{}, err
	}
	return request, nil
}

func (s *Store) GetAIGenerationJob(ctx context.Context, id string) (domain.AIGenerationJob, error) {
	var job domain.AIGenerationJob
	err := s.pool.QueryRow(ctx, `SELECT id,tenant_id,workspace_id,requested_by
		FROM ai_generation_requests WHERE id=$1`, id).
		Scan(&job.ID, &job.TenantID, &job.WorkspaceID, &job.RequestedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AIGenerationJob{}, ErrNotFound
	}
	return job, err
}

func (s *Store) MarkAIGenerationProcessing(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE ai_generation_requests SET status='processing'
		WHERE id=$1 AND status='pending'`, id)
	return err
}

func (s *Store) CompleteAIGeneration(ctx context.Context, id, resultRef string) error {
	_, err := s.pool.Exec(ctx, `UPDATE ai_generation_requests SET status='complete',result_ref=$2,
		completed_at=now() WHERE id=$1`, id, resultRef)
	return err
}

func (s *Store) FailAIGeneration(ctx context.Context, id, message string) error {
	if len(message) > 1000 {
		message = message[:1000]
	}
	_, err := s.pool.Exec(ctx, `UPDATE ai_generation_requests SET status='failed',error=$2,
		completed_at=now() WHERE id=$1`, id, message)
	return err
}

func (s *Store) GetAIGenerationRequest(ctx context.Context, p domain.Principal, id string) (domain.AIGenerationRequest, error) {
	var request domain.AIGenerationRequest
	err := s.pool.QueryRow(ctx, `SELECT id,tenant_id,workspace_id,requested_by,task_type,status,
		COALESCE(result_ref,''),COALESCE(error,''),created_at,completed_at
		FROM ai_generation_requests WHERE id=$1 AND tenant_id=$2 AND workspace_id=$3`,
		id, p.TenantID, p.WorkspaceID).Scan(&request.ID, &request.TenantID, &request.WorkspaceID,
		&request.RequestedBy, &request.TaskType, &request.Status, &request.ResultRef, &request.Error,
		&request.CreatedAt, &request.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AIGenerationRequest{}, ErrNotFound
	}
	return request, err
}

func (s *Store) QueueStatus(ctx context.Context, p domain.Principal) ([]domain.QueueStatus, error) {
	rows, err := s.pool.Query(ctx, `SELECT queue,
		COUNT(*) FILTER (WHERE status='pending'),
		COUNT(*) FILTER (WHERE status='processing'),
		COUNT(*) FILTER (WHERE status='dead')
		FROM (
			SELECT 'projection' AS queue,status FROM projection_jobs WHERE tenant_id=$1
			UNION ALL SELECT 'outbox',status FROM outbox_events WHERE tenant_id=$1
			UNION ALL SELECT 'operations',status FROM operation_jobs WHERE tenant_id=$1
		) q GROUP BY queue ORDER BY queue`, p.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.QueueStatus
	for rows.Next() {
		var item domain.QueueStatus
		if err := rows.Scan(&item.Queue, &item.Pending, &item.Processing, &item.Dead); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ListDeadLetters(ctx context.Context, p domain.Principal, queue string, limit int) ([]domain.DeadLetterItem, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	switch queue {
	case "", "projection", "outbox", "operations":
	default:
		return nil, errors.New("queue must be projection, outbox, or operations")
	}
	query := `SELECT queue,id,subject_id,kind,attempts,last_error,payload,created_at FROM (
		SELECT 'projection' AS queue,j.event_id::text AS id,COALESCE(e.external_id,e.anonymous_id,'') AS subject_id,
			e.event_type AS kind,j.attempts,COALESCE(j.last_error,'') AS last_error,e.payload,j.created_at
		FROM projection_jobs j JOIN accepted_events e ON e.id=j.event_id
		WHERE j.tenant_id=$1 AND j.status='dead'
		UNION ALL
		SELECT 'outbox' AS queue,o.id::text AS id,o.event_id::text AS subject_id,o.topic AS kind,
			o.attempts,COALESCE(o.last_error,'') AS last_error,o.payload,o.created_at
		FROM outbox_events o
		WHERE o.tenant_id=$1 AND o.status='dead'
		UNION ALL
		SELECT 'operations' AS queue,j.id::text AS id,'' AS subject_id,j.job_type AS kind,
			j.attempts,COALESCE(j.last_error,'') AS last_error,j.payload,j.created_at
		FROM operation_jobs j
		WHERE j.tenant_id=$1 AND j.status='dead'
	) dead
	WHERE ($2='' OR queue=$2)
	ORDER BY created_at DESC,id DESC LIMIT $3`
	rows, err := s.pool.Query(ctx, query, p.TenantID, queue, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.DeadLetterItem
	for rows.Next() {
		var item domain.DeadLetterItem
		if err := rows.Scan(&item.Queue, &item.ID, &item.SubjectID, &item.Kind,
			&item.Attempts, &item.LastError, &item.Payload, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) RetryDeadLetter(ctx context.Context, p domain.Principal, queue, id string) error {
	tag, err := s.updateDeadLetter(ctx, p, queue, id, true)
	if err != nil {
		return err
	}
	if tag != 1 {
		return ErrNotFound
	}
	return s.audit(ctx, p, "dlq.retry", "dead_letter", queue+":"+id, nil)
}

func (s *Store) DiscardDeadLetter(ctx context.Context, p domain.Principal, queue, id string) error {
	tag, err := s.updateDeadLetter(ctx, p, queue, id, false)
	if err != nil {
		return err
	}
	if tag != 1 {
		return ErrNotFound
	}
	return s.audit(ctx, p, "dlq.discard", "dead_letter", queue+":"+id, nil)
}

func (s *Store) updateDeadLetter(ctx context.Context, p domain.Principal, queue, id string, retry bool) (int64, error) {
	var query string
	var args []any
	switch queue {
	case "projection":
		if retry {
			query = `UPDATE projection_jobs SET status='pending',attempts=0,available_at=now(),
				locked_until=NULL,last_error=NULL
				WHERE event_id=$1 AND tenant_id=$2 AND status='dead'`
		} else {
			query = `UPDATE projection_jobs SET status='done',completed_at=now(),locked_until=NULL,
				last_error='discarded from dead letter queue'
				WHERE event_id=$1 AND tenant_id=$2 AND status='dead'`
		}
		args = []any{id, p.TenantID}
	case "outbox":
		if retry {
			query = `UPDATE outbox_events SET status='pending',attempts=0,available_at=now(),
				locked_until=NULL,last_error=NULL
				WHERE id=$1 AND tenant_id=$2 AND status='dead'`
		} else {
			query = `UPDATE outbox_events SET status='published',published_at=now(),locked_until=NULL,
				last_error='discarded from dead letter queue'
				WHERE id=$1 AND tenant_id=$2 AND status='dead'`
		}
		args = []any{id, p.TenantID}
	case "operations":
		if retry {
			query = `UPDATE operation_jobs SET status='pending',attempts=0,available_at=now(),
				locked_until=NULL,last_error=NULL
				WHERE id=$1 AND tenant_id=$2 AND workspace_id=$3 AND status='dead'`
		} else {
			query = `UPDATE operation_jobs SET status='done',completed_at=now(),locked_until=NULL,
				last_error='discarded from dead letter queue'
				WHERE id=$1 AND tenant_id=$2 AND workspace_id=$3 AND status='dead'`
		}
		args = []any{id, p.TenantID, p.WorkspaceID}
	default:
		return 0, errors.New("queue must be projection, outbox, or operations")
	}
	tag, err := s.pool.Exec(ctx, query, args...)
	return tag.RowsAffected(), err
}

func (s *Store) ListAuditEvents(ctx context.Context, p domain.Principal, limit int) ([]domain.AuditEvent, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT id,actor_type,actor_id,action,resource_type,
		COALESCE(resource_id,''),metadata,occurred_at FROM audit_events
		WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY occurred_at DESC,id DESC LIMIT $3`,
		p.TenantID, p.WorkspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.AuditEvent
	for rows.Next() {
		var item domain.AuditEvent
		if err := rows.Scan(&item.ID, &item.ActorType, &item.ActorID, &item.Action,
			&item.ResourceType, &item.ResourceID, &item.Metadata, &item.OccurredAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) audit(ctx context.Context, p domain.Principal, action, resourceType, resourceID string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	data, _ := json.Marshal(metadata)
	_, err := s.pool.Exec(ctx, `INSERT INTO audit_events
		(tenant_id,workspace_id,app_id,actor_type,actor_id,action,resource_type,resource_id,metadata)
		VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8,$9)`,
		p.TenantID, p.WorkspaceID, p.AppID, actorType(p), actorID(p), action, resourceType, resourceID, data)
	return err
}

func actorID(p domain.Principal) string {
	if p.UserID != "" {
		return p.UserID
	}
	return p.KeyID
}

func actorType(p domain.Principal) string {
	if p.ActorType != "" {
		return p.ActorType
	}
	if p.UserID != "" {
		return "user"
	}
	return "api_key"
}
