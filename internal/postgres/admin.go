package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/schemas"
	"github.com/google/uuid"
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
		eventType == "company.updated" ||
		eventType == "identity.alias" || eventType == "identity.merge" || eventType == "identity.unmerge" ||
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

	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return domain.PrivacyRequest{}, err
	}
	token := hex.EncodeToString(buf)
	h := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(h[:])
	slaDueAt := time.Now().UTC().Add(30 * 24 * time.Hour)

	var request domain.PrivacyRequest
	err = tx.QueryRow(ctx, `INSERT INTO privacy_requests
		(tenant_id,workspace_id,app_id,external_id,request_type,requested_by,verification_status,verification_token_hash,sla_due_at)
		VALUES($1,$2,$3,$4,$5,$6,'unverified',$7,$8)
		RETURNING id,external_id,request_type,status,verification_status,sla_due_at,created_at`,
		p.TenantID, p.WorkspaceID, p.AppID, externalID, requestType, actorID(p), tokenHash, slaDueAt).
		Scan(&request.ID, &request.ExternalID, &request.RequestType, &request.Status, &request.VerificationStatus, &request.SLADueAt, &request.CreatedAt)
	if err != nil {
		return domain.PrivacyRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.PrivacyRequest{}, err
	}
	request.VerificationToken = token
	_ = s.audit(ctx, p, "privacy."+requestType, "privacy_request", request.ID,
		map[string]any{"external_id": externalID, "verification_status": "unverified"})
	return request, nil
}

func (s *Store) GetPrivacyRequest(ctx context.Context, p domain.Principal, id string) (domain.PrivacyRequest, error) {
	var item domain.PrivacyRequest
	err := s.pool.QueryRow(ctx, `SELECT id,external_id,request_type,status,
		COALESCE(verification_status,'unverified'),sla_due_at,
		COALESCE(artifact_key,''),COALESCE(error,''),created_at,completed_at
		FROM privacy_requests WHERE id=$1 AND tenant_id=$2 AND workspace_id=$3`,
		id, p.TenantID, p.WorkspaceID).
		Scan(&item.ID, &item.ExternalID, &item.RequestType, &item.Status,
			&item.VerificationStatus, &item.SLADueAt,
			&item.ArtifactKey, &item.Error, &item.CreatedAt, &item.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PrivacyRequest{}, ErrNotFound
	}
	return item, err
}

func (s *Store) VerifyPrivacyRequest(ctx context.Context, p domain.Principal, id string, token string) (domain.PrivacyRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.PrivacyRequest{}, err
	}
	defer tx.Rollback(ctx)

	var requestType, status, vStatus, tokenHash, externalID string
	err = tx.QueryRow(ctx, `SELECT external_id, request_type, status, COALESCE(verification_status, 'unverified'), COALESCE(verification_token_hash, '')
		FROM privacy_requests WHERE id=$1 AND tenant_id=$2 AND workspace_id=$3 FOR UPDATE`,
		id, p.TenantID, p.WorkspaceID).
		Scan(&externalID, &requestType, &status, &vStatus, &tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PrivacyRequest{}, ErrNotFound
	}
	if err != nil {
		return domain.PrivacyRequest{}, err
	}

	if vStatus == "rejected" {
		return domain.PrivacyRequest{}, errors.New("cannot verify a rejected privacy request")
	}

	if token != "" && tokenHash != "" {
		h := sha256.Sum256([]byte(token))
		providedHash := hex.EncodeToString(h[:])
		if providedHash != tokenHash {
			return domain.PrivacyRequest{}, ErrUnauthorized
		}
	}

	if vStatus != "verified" {
		if _, err := tx.Exec(ctx, `UPDATE privacy_requests SET verification_status='verified' WHERE id=$1`, id); err != nil {
			return domain.PrivacyRequest{}, err
		}
		payload, _ := json.Marshal(map[string]string{"request_id": id})
		if _, err := tx.Exec(ctx, `INSERT INTO operation_jobs(tenant_id,workspace_id,job_type,payload)
			VALUES($1,$2,$3,$4)`, p.TenantID, p.WorkspaceID, "privacy."+requestType, payload); err != nil {
			return domain.PrivacyRequest{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.PrivacyRequest{}, err
	}

	_ = s.audit(ctx, p, "privacy.verify", "privacy_request", id, map[string]any{"external_id": externalID})

	return s.GetPrivacyRequest(ctx, p, id)
}

func (s *Store) RejectPrivacyRequest(ctx context.Context, p domain.Principal, id string, reason string) (domain.PrivacyRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.PrivacyRequest{}, err
	}
	defer tx.Rollback(ctx)

	var externalID, status, vStatus string
	err = tx.QueryRow(ctx, `SELECT external_id, status, COALESCE(verification_status, 'unverified')
		FROM privacy_requests WHERE id=$1 AND tenant_id=$2 AND workspace_id=$3 FOR UPDATE`,
		id, p.TenantID, p.WorkspaceID).
		Scan(&externalID, &status, &vStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PrivacyRequest{}, ErrNotFound
	}
	if err != nil {
		return domain.PrivacyRequest{}, err
	}

	if status == "completed" || status == "in_progress" {
		return domain.PrivacyRequest{}, errors.New("cannot reject a completed or in-progress privacy request")
	}

	if _, err := tx.Exec(ctx, `UPDATE privacy_requests SET verification_status='rejected', status='rejected', error=$2 WHERE id=$1`,
		id, reason); err != nil {
		return domain.PrivacyRequest{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.PrivacyRequest{}, err
	}

	_ = s.audit(ctx, p, "privacy.reject", "privacy_request", id, map[string]any{"external_id": externalID, "reason": reason})

	return s.GetPrivacyRequest(ctx, p, id)
}

func (s *Store) CreateImportRequest(ctx context.Context, p domain.Principal, kind, sourceKey string, mapping json.RawMessage, appID string) (domain.ImportRequest, error) {
	if kind != "profiles" && kind != "companies" && kind != "suppressions" {
		return domain.ImportRequest{}, errors.New("kind must be profiles, companies, or suppressions")
	}
	if sourceKey == "" || !json.Valid(mapping) {
		return domain.ImportRequest{}, errors.New("source blob and valid mapping are required")
	}
	if appID == "" {
		appID = p.AppID
	}
	var out domain.ImportRequest
	err := s.pool.QueryRow(ctx, `INSERT INTO import_requests(tenant_id,workspace_id,app_id,requested_by,kind,source_blob_key,mapping) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id,tenant_id,workspace_id,app_id,requested_by,kind,status,total_rows,imported_rows,failed_rows,created_at`, p.TenantID, p.WorkspaceID, appID, actorID(p), kind, sourceKey, mapping).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.AppID, &out.RequestedBy, &out.Kind, &out.Status, &out.TotalRows, &out.ImportedRows, &out.FailedRows, &out.CreatedAt)
	if err != nil {
		return out, err
	}
	payload, _ := json.Marshal(map[string]any{"request_id": out.ID, "tenant_id": p.TenantID, "workspace_id": p.WorkspaceID, "app_id": appID})
	if _, err = s.pool.Exec(ctx, `INSERT INTO operation_jobs(tenant_id,workspace_id,job_type,payload) VALUES($1,$2,'profiles.import',$3)`, p.TenantID, p.WorkspaceID, payload); err != nil {
		return domain.ImportRequest{}, err
	}
	return out, nil
}

func (s *Store) GetImportRequest(ctx context.Context, p domain.Principal, id string) (domain.ImportRequest, error) {
	var out domain.ImportRequest
	err := s.pool.QueryRow(ctx, `SELECT id,tenant_id,workspace_id,app_id,requested_by,kind,status,total_rows,imported_rows,failed_rows,COALESCE(result_ref,''),COALESCE(error,''),created_at,completed_at FROM import_requests WHERE id=$1 AND tenant_id=$2 AND workspace_id=$3`, id, p.TenantID, p.WorkspaceID).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.AppID, &out.RequestedBy, &out.Kind, &out.Status, &out.TotalRows, &out.ImportedRows, &out.FailedRows, &out.ResultRef, &out.Error, &out.CreatedAt, &out.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}

func (s *Store) GetImportJob(ctx context.Context, id string) (domain.ImportRequest, string, json.RawMessage, error) {
	var out domain.ImportRequest
	var key string
	var mapping json.RawMessage
	err := s.pool.QueryRow(ctx, `SELECT id,tenant_id,workspace_id,app_id,requested_by,kind,status,total_rows,imported_rows,failed_rows,source_blob_key,mapping,created_at,completed_at FROM import_requests WHERE id=$1`, id).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.AppID, &out.RequestedBy, &out.Kind, &out.Status, &out.TotalRows, &out.ImportedRows, &out.FailedRows, &key, &mapping, &out.CreatedAt, &out.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, "", nil, ErrNotFound
	}
	return out, key, mapping, err
}

func (s *Store) MarkImportProcessing(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE import_requests SET status='processing' WHERE id=$1 AND status='pending'`, id)
	return err
}
func (s *Store) CompleteImport(ctx context.Context, id, resultRef string, total, imported, failed int) error {
	_, err := s.pool.Exec(ctx, `UPDATE import_requests SET status='complete',result_ref=$2,total_rows=$3,imported_rows=$4,failed_rows=$5,completed_at=now() WHERE id=$1`, id, resultRef, total, imported, failed)
	return err
}
func (s *Store) FailImport(ctx context.Context, id, message string) error {
	_, err := s.pool.Exec(ctx, `UPDATE import_requests SET status='failed',error=$2,completed_at=now() WHERE id=$1`, id, message)
	return err
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

func ComputeAuditRowHash(prevHash, id, tenantID, workspaceID, appID, actorType, actorID, action, resourceType, resourceID string, metadata []byte, occurredAt time.Time, seq int64) string {
	canonical := fmt.Sprintf("%s|%d|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s",
		prevHash,
		seq,
		id,
		tenantID,
		workspaceID,
		appID,
		actorType,
		actorID,
		action,
		resourceType,
		resourceID,
		string(metadata),
		occurredAt.UTC().Format(time.RFC3339Nano),
	)
	h := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(h[:])
}

func (s *Store) ListAuditEvents(ctx context.Context, p domain.Principal, limit int) ([]domain.AuditEvent, error) {
	return s.ListAuditEventsFiltered(ctx, p, domain.AuditFilter{Limit: limit})
}

func (s *Store) ListAuditEventsFiltered(ctx context.Context, p domain.Principal, filter domain.AuditFilter) ([]domain.AuditEvent, error) {
	limit := filter.Limit
	if limit < 1 || limit > 500 {
		limit = 100
	}
	var sb strings.Builder
	sb.WriteString(`SELECT id,actor_type,actor_id,action,resource_type,
		COALESCE(resource_id,''),metadata,occurred_at,COALESCE(seq,0),COALESCE(prev_hash,''),COALESCE(row_hash,'') FROM audit_events
		WHERE tenant_id=$1 AND workspace_id=$2`)
	args := []any{p.TenantID, p.WorkspaceID}
	paramIdx := 3

	if filter.ActorID != "" {
		sb.WriteString(fmt.Sprintf(" AND actor_id=$%d", paramIdx))
		args = append(args, filter.ActorID)
		paramIdx++
	}
	if filter.ResourceType != "" {
		sb.WriteString(fmt.Sprintf(" AND resource_type=$%d", paramIdx))
		args = append(args, filter.ResourceType)
		paramIdx++
	}
	if filter.Action != "" {
		sb.WriteString(fmt.Sprintf(" AND action=$%d", paramIdx))
		args = append(args, filter.Action)
		paramIdx++
	}
	if filter.StartTime != nil {
		sb.WriteString(fmt.Sprintf(" AND occurred_at >= $%d", paramIdx))
		args = append(args, *filter.StartTime)
		paramIdx++
	}
	if filter.EndTime != nil {
		sb.WriteString(fmt.Sprintf(" AND occurred_at <= $%d", paramIdx))
		args = append(args, *filter.EndTime)
		paramIdx++
	}

	sb.WriteString(fmt.Sprintf(" ORDER BY occurred_at DESC, id DESC LIMIT $%d", paramIdx))
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.AuditEvent
	for rows.Next() {
		var item domain.AuditEvent
		if err := rows.Scan(&item.ID, &item.ActorType, &item.ActorID, &item.Action,
			&item.ResourceType, &item.ResourceID, &item.Metadata, &item.OccurredAt,
			&item.Seq, &item.PrevHash, &item.RowHash); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) VerifyAuditChain(ctx context.Context, p domain.Principal) (domain.AuditVerificationResult, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, workspace_id, COALESCE(app_id::text, ''), actor_type, actor_id, action, resource_type, COALESCE(resource_id, ''), metadata, occurred_at, seq, prev_hash, row_hash
		FROM audit_events WHERE tenant_id = $1 ORDER BY seq ASC`, p.TenantID)
	if err != nil {
		return domain.AuditVerificationResult{}, err
	}
	defer rows.Close()

	var total int64
	expectedPrevHash := ""

	for rows.Next() {
		total++
		var (
			id, tenantID, workspaceID, appID, actType, actID, act, resType, resID, prevHash, rowHash string
			metadata                                                                                 []byte
			occurredAt                                                                               time.Time
			seq                                                                                      int64
		)
		if err := rows.Scan(&id, &tenantID, &workspaceID, &appID, &actType, &actID, &act, &resType, &resID, &metadata, &occurredAt, &seq, &prevHash, &rowHash); err != nil {
			return domain.AuditVerificationResult{}, err
		}

		if prevHash != expectedPrevHash {
			return domain.AuditVerificationResult{
				Status:         "tampered",
				Intact:         false,
				TotalEvents:    total,
				FirstBrokenSeq: &seq,
				FirstBrokenID:  id,
				Reason:         fmt.Sprintf("prev_hash mismatch at seq %d", seq),
			}, nil
		}

		computedHash := ComputeAuditRowHash(prevHash, id, tenantID, workspaceID, appID, actType, actID, act, resType, resID, metadata, occurredAt, seq)
		if rowHash != computedHash {
			return domain.AuditVerificationResult{
				Status:         "tampered",
				Intact:         false,
				TotalEvents:    total,
				FirstBrokenSeq: &seq,
				FirstBrokenID:  id,
				Reason:         fmt.Sprintf("row_hash mismatch at seq %d", seq),
			}, nil
		}

		expectedPrevHash = rowHash
	}

	if err := rows.Err(); err != nil {
		return domain.AuditVerificationResult{}, err
	}

	return domain.AuditVerificationResult{
		Status:      "ok",
		Intact:      true,
		TotalEvents: total,
	}, nil
}

func (s *Store) audit(ctx context.Context, p domain.Principal, action, resourceType, resourceID string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	data, _ := json.Marshal(metadata)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var lockKey int64
	err = tx.QueryRow(ctx, "SELECT hashtext('audit:' || $1)", p.TenantID).Scan(&lockKey)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey); err != nil {
		return err
	}

	var lastSeq int64
	var lastHash string
	err = tx.QueryRow(ctx, `SELECT seq, row_hash FROM audit_events WHERE tenant_id = $1 ORDER BY seq DESC LIMIT 1`, p.TenantID).Scan(&lastSeq, &lastHash)
	if errors.Is(err, pgx.ErrNoRows) {
		lastSeq = 0
		lastHash = ""
	} else if err != nil {
		return err
	}

	nextSeq := lastSeq + 1
	prevHash := lastHash
	eventID := uuid.NewString()
	occurredAt := time.Now().UTC()

	rowHash := ComputeAuditRowHash(prevHash, eventID, p.TenantID, p.WorkspaceID, p.AppID, actorType(p), actorID(p), action, resourceType, resourceID, data, occurredAt, nextSeq)

	_, err = tx.Exec(ctx, `INSERT INTO audit_events
		(id, tenant_id, workspace_id, app_id, actor_type, actor_id, action, resource_type, resource_id, metadata, occurred_at, seq, prev_hash, row_hash)
		VALUES($1, $2, $3, NULLIF($4,'')::uuid, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		eventID, p.TenantID, p.WorkspaceID, p.AppID, actorType(p), actorID(p), action, resourceType, resourceID, data, occurredAt, nextSeq, prevHash, rowHash)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
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

type auditRow struct {
	id, tenantID, workspaceID, appID, actType, actID, act, resType, resID, prevHash, rowHash string
	metadata                                                                                 []byte
	occurredAt                                                                               time.Time
	seq                                                                                      int64
}

// BackfillAuditChain re-sequences and re-hashes existing audit events per tenant using ComputeAuditRowHash.
func (s *Store) BackfillAuditChain(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	// Disable trigger temporarily for backfill
	_, _ = conn.Exec(ctx, "ALTER TABLE audit_events DISABLE TRIGGER audit_events_no_update")
	defer func() {
		_, _ = conn.Exec(context.Background(), "ALTER TABLE audit_events ENABLE TRIGGER audit_events_no_update")
		_, _ = conn.Exec(context.Background(), "ALTER TABLE audit_events ALTER COLUMN seq SET NOT NULL")
		_, _ = conn.Exec(context.Background(), "CREATE UNIQUE INDEX IF NOT EXISTS audit_events_tenant_seq_idx ON audit_events (tenant_id, seq)")
	}()

	rows, err := conn.Query(ctx, "SELECT DISTINCT tenant_id FROM audit_events")
	if err != nil {
		return err
	}
	var tenantIDs []string
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err == nil {
			tenantIDs = append(tenantIDs, tid)
		}
	}
	rows.Close()

	for _, tenantID := range tenantIDs {
		evRows, err := conn.Query(ctx, `SELECT id, tenant_id, workspace_id, COALESCE(app_id::text, ''), actor_type, actor_id, action, resource_type, COALESCE(resource_id, ''), metadata, occurred_at, COALESCE(seq, 0), prev_hash, row_hash
			FROM audit_events WHERE tenant_id = $1 ORDER BY occurred_at ASC, id ASC`, tenantID)
		if err != nil {
			return err
		}

		var items []auditRow
		for evRows.Next() {
			var item auditRow
			if err := evRows.Scan(&item.id, &item.tenantID, &item.workspaceID, &item.appID, &item.actType, &item.actID, &item.act, &item.resType, &item.resID, &item.metadata, &item.occurredAt, &item.seq, &item.prevHash, &item.rowHash); err == nil {
				items = append(items, item)
			}
		}
		evRows.Close()

		var currentPrevHash string
		var seq int64 = 0

		for _, item := range items {
			seq++
			expectedPrevHash := currentPrevHash
			expectedRowHash := ComputeAuditRowHash(expectedPrevHash, item.id, item.tenantID, item.workspaceID, item.appID, item.actType, item.actID, item.act, item.resType, item.resID, item.metadata, item.occurredAt, seq)

			if item.seq != seq || item.prevHash != expectedPrevHash || item.rowHash != expectedRowHash {
				_, err := conn.Exec(ctx, `UPDATE audit_events SET seq = $1, prev_hash = $2, row_hash = $3 WHERE id = $4`, seq, expectedPrevHash, expectedRowHash, item.id)
				if err != nil {
					return fmt.Errorf("failed to backfill audit event %s: %w", item.id, err)
				}
			}
			currentPrevHash = expectedRowHash
		}
	}

	return nil
}

