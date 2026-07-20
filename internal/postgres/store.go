package postgres

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/schemas"
	"github.com/buildwithdmytro/openjourney/internal/telemetry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

//go:embed migrations/*.sql
var migrations embed.FS

var ErrUnauthorized = errors.New("unauthorized")
var ErrNotFound = ports.ErrNotFound
var ErrQuotaExceeded = errors.New("quota exceeded")
var ErrIdempotencyConflict = errors.New("idempotency key reused with different event")

type Store struct {
	pool             *pgxpool.Pool
	blobs            ports.BlobStore
	schemaMu         sync.RWMutex
	schemaCache      map[string]*schemas.Validator
	schemaCacheKnown map[string]bool
	chConn           clickhouse.Conn
	trustedKeysMu    sync.RWMutex
	trustedKeys      map[string]any
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{
		pool: pool, schemaCache: map[string]*schemas.Validator{},
		schemaCacheKnown: map[string]bool{},
		trustedKeys:      map[string]any{},
	}, nil
}

func (s *Store) SetTrustedPublisherKeys(keys map[string]any) {
	s.trustedKeysMu.Lock()
	defer s.trustedKeysMu.Unlock()
	s.trustedKeys = keys
}

func (s *Store) Close() { s.pool.Close() }

// SetBlobStore supplies the immutable object store used for reversible identity
// merge snapshots. Identity state is still changed only by ProjectEvent.
func (s *Store) SetBlobStore(blobs ports.BlobStore) { s.blobs = blobs }

func (s *Store) Ready(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext('openjourney:migrations'))"); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock(hashtext('openjourney:migrations'))")
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var applied bool
		if err := conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", entry.Name()).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		content, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES($1)", entry.Name()); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) EnsureDevelopmentTenant(ctx context.Context, rawKey string) error {
	if rawKey == "" {
		return nil
	}
	hash := sha256.Sum256([]byte(rawKey))
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM api_keys WHERE key_hash=$1)", hash[:]).Scan(&exists); err != nil || exists {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var tenantID, workspaceID, appID string
	if err := tx.QueryRow(ctx, "INSERT INTO tenants(name) VALUES('Development') RETURNING id").Scan(&tenantID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, "INSERT INTO workspaces(tenant_id,name) VALUES($1,'Default') RETURNING id", tenantID).Scan(&workspaceID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, "INSERT INTO applications(tenant_id,workspace_id,name) VALUES($1,$2,'Web') RETURNING id", tenantID, workspaceID).Scan(&appID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO api_keys(tenant_id,workspace_id,app_id,name,key_hash)
		VALUES($1,$2,$3,'Development key',$4)`, tenantID, workspaceID, appID, hash[:])
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO tenant_quotas(tenant_id) VALUES($1)
		ON CONFLICT(tenant_id) DO NOTHING`, tenantID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if err := s.seedDevelopmentRole(ctx, tenantID); err != nil {
		return err
	}
	return s.seedDevelopmentAIPrompts(ctx, tenantID, workspaceID)
}

func (s *Store) Authenticate(ctx context.Context, rawKey string) (domain.Principal, error) {
	hash := sha256.Sum256([]byte(rawKey))
	var p domain.Principal
	err := s.pool.QueryRow(ctx, `UPDATE api_keys SET last_used_at=now()
		WHERE key_hash=$1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())
		RETURNING tenant_id, workspace_id, app_id, id, scopes`, hash[:]).
		Scan(&p.TenantID, &p.WorkspaceID, &p.AppID, &p.KeyID, &p.Scopes)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.authenticateSession(ctx, hash[:])
	}
	p.ActorType = "api_key"
	return p, err
}

func (s *Store) authenticateSession(ctx context.Context, tokenHash []byte) (domain.Principal, error) {
	var p domain.Principal
	err := s.pool.QueryRow(ctx, `WITH matched AS (
			UPDATE user_sessions SET last_used_at=now()
			WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at > now()
			RETURNING tenant_id,workspace_id,app_id,user_id
		)
		SELECT m.tenant_id,m.workspace_id,m.app_id,m.user_id,
			COALESCE(array_agg(DISTINCT permission) FILTER (WHERE permission IS NOT NULL),'{}')
		FROM matched m
		JOIN users u ON u.id=m.user_id AND u.tenant_id=m.tenant_id AND u.disabled_at IS NULL
		JOIN role_bindings b ON b.user_id=u.id AND b.tenant_id=u.tenant_id
			AND (b.workspace_id IS NULL OR b.workspace_id=m.workspace_id)
		JOIN roles r ON r.id=b.role_id AND r.tenant_id=u.tenant_id
		LEFT JOIN LATERAL unnest(r.permissions) permission ON true
		GROUP BY m.tenant_id,m.workspace_id,m.app_id,m.user_id`, tokenHash).
		Scan(&p.TenantID, &p.WorkspaceID, &p.AppID, &p.UserID, &p.Scopes)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Principal{}, ErrUnauthorized
	}
	p.ActorType = "user"
	return p, err
}

func (s *Store) AcceptEvents(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var maxBatch, perMinute int
	if err := tx.QueryRow(ctx, `SELECT max_batch_size,events_per_minute FROM tenant_quotas WHERE tenant_id=$1`,
		p.TenantID).Scan(&maxBatch, &perMinute); err != nil {
		return nil, err
	}
	if len(events) > maxBatch {
		return nil, fmt.Errorf("%w: maximum batch size is %d", ErrQuotaExceeded, maxBatch)
	}
	ids := make([]string, 0, len(events))
	for _, event := range events {
		event.OccurredAt = event.OccurredAt.UTC().Truncate(time.Microsecond)
		if event.Source == "" {
			event.Source = "api"
		}
		if event.DataClassification == "" {
			event.DataClassification = "internal"
		}
		if len(event.ConsentContext) == 0 {
			event.ConsentContext = json.RawMessage(`{}`)
		}
		var id string
		var receivedAt time.Time
		query := `INSERT INTO accepted_events
			(tenant_id,workspace_id,app_id,event_type,schema_version,external_id,anonymous_id,idempotency_key,
			 occurred_at,source,source_event_id,correlation_id,causation_id,traceparent,data_classification,consent_context,payload)
			VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),$8,$9,$10,NULLIF($11,''),
			       NULLIF($12,''),NULLIF($13,''),NULLIF($14,''),$15,$16,$17)
			ON CONFLICT (tenant_id,app_id,idempotency_key)
			DO NOTHING
			RETURNING id,received_at`
		err := tx.QueryRow(ctx, query, p.TenantID, p.WorkspaceID, p.AppID, event.Type, event.SchemaVersion,
			event.ExternalID, event.AnonymousID, event.IdempotencyKey, event.OccurredAt, event.Source,
			event.SourceEventID, event.CorrelationID, event.CausationID, event.Traceparent,
			event.DataClassification, event.ConsentContext, event.Payload).Scan(&id, &receivedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			var existingType, existingExternal, existingAnonymous string
			var existingVersion int
			var existingOccurred time.Time
			var existingPayload json.RawMessage
			if err := tx.QueryRow(ctx, `SELECT id,received_at,event_type,schema_version,COALESCE(external_id,''),
				COALESCE(anonymous_id,''),occurred_at,payload FROM accepted_events
				WHERE tenant_id=$1 AND app_id=$2 AND idempotency_key=$3`,
				p.TenantID, p.AppID, event.IdempotencyKey).
				Scan(&id, &receivedAt, &existingType, &existingVersion, &existingExternal, &existingAnonymous,
					&existingOccurred, &existingPayload); err != nil {
				return nil, err
			}
			var incomingValue, existingValue any
			_ = json.Unmarshal(event.Payload, &incomingValue)
			_ = json.Unmarshal(existingPayload, &existingValue)
			if existingType != event.Type || existingVersion != event.SchemaVersion ||
				existingExternal != event.ExternalID || existingAnonymous != event.AnonymousID ||
				!existingOccurred.Equal(event.OccurredAt) || !reflect.DeepEqual(existingValue, incomingValue) {
				return nil, ErrIdempotencyConflict
			}
		} else if err != nil {
			return nil, err
		}
		partitionKey := event.ExternalID
		if partitionKey == "" {
			partitionKey = event.AnonymousID
		}
		if _, err := tx.Exec(ctx, `INSERT INTO projection_jobs(event_id,tenant_id,partition_key) VALUES($1,$2,$3)
			ON CONFLICT(event_id) DO NOTHING`, id, p.TenantID, partitionKey); err != nil {
			return nil, err
		}
		envelope, _ := json.Marshal(map[string]any{
			"event_id": id, "tenant_id": p.TenantID, "workspace_id": p.WorkspaceID, "app_id": p.AppID,
			"event_type": event.Type, "schema_version": event.SchemaVersion, "external_id": event.ExternalID,
			"anonymous_id": event.AnonymousID, "occurred_at": event.OccurredAt, "received_at": receivedAt,
			"source": event.Source, "source_event_id": event.SourceEventID, "correlation_id": event.CorrelationID,
			"causation_id": event.CausationID, "traceparent": event.Traceparent,
			"data_classification": event.DataClassification, "consent_context": event.ConsentContext,
			"payload": event.Payload,
		})
		if _, err := tx.Exec(ctx, `INSERT INTO outbox_events
			(tenant_id,topic,partition_key,event_id,payload) VALUES($1,'events.accepted.v1',$2,$3,$4)
			ON CONFLICT(topic,event_id) DO NOTHING`, p.TenantID, partitionKey, id, envelope); err != nil {
			return nil, err
		}
		var exportPipelines []string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id::text ORDER BY id), '{}')
			FROM connector_pipelines
			WHERE tenant_id=$1 AND workspace_id=$2 AND app_id=$3
			  AND direction='export' AND status='enabled'`, p.TenantID, p.WorkspaceID, p.AppID).Scan(&exportPipelines); err != nil {
			return nil, err
		}
		if len(exportPipelines) > 0 {
			var exportEnvelope map[string]any
			if err := json.Unmarshal(envelope, &exportEnvelope); err != nil {
				return nil, err
			}
			exportEnvelope["export_pipeline_ids"] = exportPipelines
			exportPayload, err := json.Marshal(exportEnvelope)
			if err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO outbox_events
				(tenant_id,topic,partition_key,event_id,payload) VALUES($1,'exports.events.v1',$2,$3,$4)
				ON CONFLICT(topic,event_id) DO NOTHING`, p.TenantID, partitionKey, id, exportPayload); err != nil {
				return nil, err
			}
		}
		// Connector jobs are created in the same transaction as the accepted event
		// and outbox record. The payload predicate makes replays idempotent per
		// connector/event pair, including duplicate API submissions.
		if _, err := tx.Exec(ctx, `
			INSERT INTO operation_jobs (tenant_id, workspace_id, job_type, payload)
			SELECT $1, $2, 'connector.run', jsonb_build_object(
				'tenant_id', $1, 'workspace_id', $2, 'extension_id', e.id::text,
				'event_id', $3, 'event_type', $4, 'event', $5::jsonb)
			FROM extensions e
			JOIN extension_versions ev ON ev.id = e.current_version_id
			JOIN extension_subscriptions es ON es.extension_id = e.id AND es.tenant_id = e.tenant_id
			WHERE e.tenant_id = $1 AND e.workspace_id = $2 AND e.status = 'enabled'
			  AND ev.kind = 'connector' AND ev.status = 'active'
			  AND es.event_type = 'events.accepted.v1'
			  AND NOT EXISTS (
				SELECT 1 FROM operation_jobs existing
				WHERE existing.job_type = 'connector.run'
				  AND existing.payload->>'extension_id' = e.id::text
				  AND existing.payload->>'event_id' = $3)
		`, p.TenantID, p.WorkspaceID, id, event.Type, envelope); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	var windowCount int
	if err := tx.QueryRow(ctx, `INSERT INTO quota_windows(tenant_id,window_start,event_count)
		VALUES($1,date_trunc('minute',now()),$2)
		ON CONFLICT(tenant_id,window_start) DO UPDATE
		SET event_count=quota_windows.event_count+EXCLUDED.event_count
		RETURNING event_count`, p.TenantID, len(events)).Scan(&windowCount); err != nil {
		return nil, err
	}
	if windowCount > perMinute {
		return nil, fmt.Errorf("%w: events per minute", ErrQuotaExceeded)
	}
	metadata, _ := json.Marshal(map[string]int{"count": len(events)})
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
		(tenant_id,workspace_id,app_id,actor_type,actor_id,action,resource_type,metadata)
		VALUES($1,$2,$3,$4,$5,'events.accept','event_batch',$6)`,
		p.TenantID, p.WorkspaceID, p.AppID, actorType(p), actorID(p), metadata)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *Store) GetProfile(ctx context.Context, p domain.Principal, externalID string) (domain.Profile, []domain.Consent, error) {
	var profile domain.Profile
	err := s.pool.QueryRow(ctx, `SELECT id,COALESCE(external_id,''),COALESCE(anonymous_id,''),attributes,version,updated_at
		FROM profiles WHERE tenant_id=$1 AND app_id=$2 AND external_id=$3`,
		p.TenantID, p.AppID, externalID).
		Scan(&profile.ID, &profile.ExternalID, &profile.AnonymousID, &profile.Attributes, &profile.Version, &profile.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Profile{}, nil, ErrNotFound
	}
	if err != nil {
		return domain.Profile{}, nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (channel,topic)
		profile_id,channel,topic,state,occurred_at
		FROM consent_ledger WHERE tenant_id=$1 AND profile_id=$2
		ORDER BY channel,topic,occurred_at DESC,created_at DESC`, p.TenantID, profile.ID)
	if err != nil {
		return domain.Profile{}, nil, err
	}
	defer rows.Close()
	var consents []domain.Consent
	for rows.Next() {
		var consent domain.Consent
		if err := rows.Scan(&consent.ProfileID, &consent.Channel, &consent.Topic, &consent.State, &consent.OccurredAt); err != nil {
			return domain.Profile{}, nil, err
		}
		consents = append(consents, consent)
	}
	return profile, consents, rows.Err()
}

func (s *Store) GetProfileByID(ctx context.Context, tenantID, appID, profileID string) (domain.Profile, error) {
	var profile domain.Profile
	err := s.pool.QueryRow(ctx, `SELECT id,COALESCE(external_id,''),COALESCE(anonymous_id,''),attributes,version,updated_at
		FROM profiles WHERE tenant_id=$1 AND app_id=$2 AND id=$3`,
		tenantID, appID, profileID).
		Scan(&profile.ID, &profile.ExternalID, &profile.AnonymousID, &profile.Attributes, &profile.Version, &profile.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Profile{}, ErrNotFound
	}
	return profile, err
}

func (s *Store) ClaimProjectionJob(ctx context.Context) (domain.AcceptedEvent, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AcceptedEvent{}, false, err
	}
	defer tx.Rollback(ctx)
	var event domain.AcceptedEvent
	query := `SELECT e.id,e.tenant_id,e.workspace_id,e.app_id,e.event_type,e.schema_version,
		COALESCE(e.external_id,''),COALESCE(e.anonymous_id,''),e.idempotency_key,e.occurred_at,e.received_at,
		e.source,COALESCE(e.source_event_id,''),COALESCE(e.correlation_id,''),COALESCE(e.causation_id,''),
		COALESCE(e.traceparent,''),e.data_classification,e.consent_context,e.payload
		FROM projection_jobs j JOIN accepted_events e ON e.id=j.event_id
		WHERE (j.status='pending' OR (j.status='processing' AND j.locked_until < now()))
		  AND j.available_at <= now()
		  AND NOT EXISTS (
			SELECT 1 FROM projection_jobs earlier
			JOIN accepted_events earlier_event ON earlier_event.id=earlier.event_id
			WHERE earlier.tenant_id=j.tenant_id
			  AND earlier.sequence < j.sequence AND earlier.status NOT IN ('done','dead')
			  AND (
			    earlier.partition_key=j.partition_key
			    OR (e.external_id IS NOT NULL AND earlier_event.external_id=e.external_id)
			    OR (e.anonymous_id IS NOT NULL AND earlier_event.anonymous_id=e.anonymous_id)
			  )
		  )
		ORDER BY j.sequence
		FOR UPDATE OF j SKIP LOCKED LIMIT 1`
	err = tx.QueryRow(ctx, query).Scan(&event.ID, &event.Principal.TenantID, &event.Principal.WorkspaceID,
		&event.Principal.AppID, &event.Type, &event.SchemaVersion, &event.ExternalID, &event.AnonymousID,
		&event.IdempotencyKey, &event.OccurredAt, &event.ReceivedAt, &event.Source, &event.SourceEventID,
		&event.CorrelationID, &event.CausationID, &event.Traceparent, &event.DataClassification,
		&event.ConsentContext, &event.Payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AcceptedEvent{}, false, nil
	}
	if err != nil {
		return domain.AcceptedEvent{}, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE projection_jobs SET status='processing',attempts=attempts+1,
		locked_until=now()+interval '30 seconds' WHERE event_id=$1`, event.ID); err != nil {
		return domain.AcceptedEvent{}, false, err
	}
	return event, true, tx.Commit(ctx)
}

func (s *Store) ProjectEvent(ctx context.Context, event domain.AcceptedEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var profileID string
	var conversionAttributed bool
	var conversionSourceType, conversionVariant string
	if event.Type != "message.bounced" && event.Type != "message.complained" && event.Type != "identity.unmerge" {
		var err error
		profileID, err = s.resolveIdentity(ctx, tx, event)
		if err == nil && profileID == "" {
			profileID, err = ensureProfile(ctx, tx, event)
		}
		if err != nil {
			return err
		}
	}
	switch event.Type {
	case "company.updated":
		var body struct {
			Company struct {
				ExternalID string          `json:"external_id"`
				Name       string          `json:"name"`
				Attributes json.RawMessage `json:"attributes"`
			} `json:"company"`
			Members []struct {
				ProfileExternalID string `json:"profile_external_id"`
				Role              string `json:"role"`
			} `json:"members"`
		}
		if err := json.Unmarshal(event.Payload, &body); err != nil || body.Company.Name == "" {
			return errors.New("company.updated payload requires company name")
		}
		attrs := body.Company.Attributes
		if len(attrs) == 0 {
			attrs = json.RawMessage(`{}`)
		}
		var companyID string
		if err := tx.QueryRow(ctx, `INSERT INTO companies(tenant_id,workspace_id,app_id,external_id,name,attributes) VALUES($1,$2,$3,NULLIF($4,''),$5,$6) ON CONFLICT(tenant_id,app_id,external_id) DO UPDATE SET name=EXCLUDED.name,attributes=companies.attributes || EXCLUDED.attributes,version=companies.version+1,updated_at=now() RETURNING id`, event.Principal.TenantID, event.Principal.WorkspaceID, event.Principal.AppID, body.Company.ExternalID, body.Company.Name, attrs).Scan(&companyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM company_members WHERE tenant_id=$1 AND company_id=$2`, event.Principal.TenantID, companyID); err != nil {
			return err
		}
		for _, member := range body.Members {
			if member.ProfileExternalID == "" {
				continue
			}
			var profileID string
			if err := tx.QueryRow(ctx, `SELECT id FROM profiles WHERE tenant_id=$1 AND workspace_id=$2 AND app_id=$3 AND external_id=$4`, event.Principal.TenantID, event.Principal.WorkspaceID, event.Principal.AppID, member.ProfileExternalID).Scan(&profileID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO company_members(tenant_id,company_id,profile_id,role) VALUES($1,$2,$3,$4) ON CONFLICT(company_id,profile_id) DO UPDATE SET role=EXCLUDED.role`, event.Principal.TenantID, companyID, profileID, member.Role); err != nil {
				return err
			}
		}
	case "profile.updated":
		var body struct {
			Attributes map[string]any `json:"attributes"`
		}
		if err := json.Unmarshal(event.Payload, &body); err != nil || body.Attributes == nil {
			return errors.New("profile.updated payload requires an attributes object")
		}
		attributes, _ := json.Marshal(body.Attributes)
		if _, err := tx.Exec(ctx, `UPDATE profiles SET attributes=attributes || $1::jsonb,
			version=version+1,updated_at=now() WHERE id=$2`, attributes, profileID); err != nil {
			return err
		}
	case "consent.changed":
		var body struct {
			Channel  string         `json:"channel"`
			Topic    string         `json:"topic"`
			State    string         `json:"state"`
			Evidence map[string]any `json:"evidence"`
		}
		if err := json.Unmarshal(event.Payload, &body); err != nil {
			return err
		}
		if body.Channel == "" || (body.State != "subscribed" && body.State != "unsubscribed") {
			return errors.New("consent.changed requires channel and subscribed/unsubscribed state")
		}
		if body.Topic == "" {
			body.Topic = "marketing"
		}
		evidence, _ := json.Marshal(body.Evidence)
		_, err = tx.Exec(ctx, `INSERT INTO consent_ledger
			(tenant_id,workspace_id,app_id,profile_id,source_event_id,channel,topic,state,occurred_at,evidence)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT(source_event_id,channel,topic) DO NOTHING`,
			event.Principal.TenantID, event.Principal.WorkspaceID, event.Principal.AppID, profileID,
			event.ID, strings.ToLower(body.Channel), body.Topic, body.State, event.OccurredAt, evidence)
		if err != nil {
			return err
		}

		// Handle suppressions insertion/deletion for unsubscribe/subscribe
		var endpoint string
		var attributesJSON []byte
		err = tx.QueryRow(ctx, `SELECT attributes FROM profiles WHERE id=$1`, profileID).Scan(&attributesJSON)
		if err == nil && len(attributesJSON) > 0 {
			var attrs map[string]any
			if json.Unmarshal(attributesJSON, &attrs) == nil {
				if body.Channel == "sms" {
					endpoint, _ = attrs["phone"].(string)
				} else if body.Channel == "email" {
					endpoint, _ = attrs["email"].(string)
				}
			}
		}
		if endpoint != "" {
			if body.State == "unsubscribed" {
				tag, err := tx.Exec(ctx, `INSERT INTO suppressions
					(tenant_id, channel, endpoint, reason, source_event_id)
					VALUES($1, $2, $3, $4, $5)
					ON CONFLICT(tenant_id, channel, endpoint) DO NOTHING`,
					event.Principal.TenantID, strings.ToLower(body.Channel), strings.ToLower(endpoint), "unsubscribe", event.ID)
				if err != nil {
					return err
				}
				if err == nil && tag.RowsAffected() > 0 && strings.ToLower(body.Channel) == "sms" {
					telemetry.SMSOptOuts.Add(ctx, 1)
				}
			} else if body.State == "subscribed" {
				_, err = tx.Exec(ctx, `DELETE FROM suppressions
					WHERE tenant_id=$1 AND channel=$2 AND endpoint=$3 AND reason=$4`,
					event.Principal.TenantID, strings.ToLower(body.Channel), strings.ToLower(endpoint), "unsubscribe")
				if err != nil {
					return err
				}
			}
		}
	case "message.bounced", "message.complained":
		var body struct {
			Channel    string `json:"channel"`
			Endpoint   string `json:"endpoint"`
			BounceType string `json:"bounce_type"`
		}
		if err := json.Unmarshal(event.Payload, &body); err != nil {
			return err
		}
		if body.Endpoint == "" {
			return errors.New("bounce/complaint event payload requires an endpoint")
		}
		channel := body.Channel
		if channel == "" {
			channel = "email"
		}
		reason := "bounce"
		if event.Type == "message.complained" {
			reason = "complaint"
			telemetry.Complaints.Add(ctx, 1, otelmetric.WithAttributes(
				attribute.String("channel", channel),
			))
		} else {
			telemetry.Bounces.Add(ctx, 1, otelmetric.WithAttributes(
				attribute.String("channel", channel),
				attribute.String("bounce_type", body.BounceType),
			))
		}
		_, err = tx.Exec(ctx, `INSERT INTO suppressions
			(tenant_id, channel, endpoint, reason, source_event_id)
			VALUES($1, $2, $3, $4, $5)
			ON CONFLICT(tenant_id, channel, endpoint) DO NOTHING`,
			event.Principal.TenantID, strings.ToLower(channel), strings.ToLower(body.Endpoint), reason, event.ID)
		if err != nil {
			return err
		}
	case "identity.alias":
		var body struct {
			Namespace  string            `json:"namespace"`
			Value      string            `json:"value"`
			Identities map[string]string `json:"identities"`
		}
		if err := json.Unmarshal(event.Payload, &body); err != nil {
			return err
		}
		aliases := body.Identities
		if len(aliases) == 0 {
			aliases = map[string]string{body.Namespace: body.Value}
		}
		for namespace, value := range aliases {
			namespace = strings.TrimSpace(namespace)
			value = normalizeIdentityValue(namespace, value)
			if namespace == "" || value == "" {
				return errors.New("identity.alias contains an empty namespace or value")
			}
			if _, err = tx.Exec(ctx, `INSERT INTO identity_aliases
				(tenant_id,app_id,namespace,value,profile_id,source_event_id)
				VALUES($1,$2,$3,$4,$5,$6)
				ON CONFLICT(tenant_id,app_id,namespace,value)
				DO UPDATE SET profile_id=EXCLUDED.profile_id,source_event_id=EXCLUDED.source_event_id`,
				event.Principal.TenantID, event.Principal.AppID, namespace, value, profileID, event.ID); err != nil {
				return err
			}
		}
	case "identity.merge":
		var body struct {
			SourceExternalID string `json:"source_external_id"`
		}
		if err := json.Unmarshal(event.Payload, &body); err != nil {
			return err
		}
		var sourceID string
		err := tx.QueryRow(ctx, `SELECT id FROM profiles
			WHERE tenant_id=$1 AND app_id=$2 AND external_id=$3 FOR UPDATE`,
			event.Principal.TenantID, event.Principal.AppID, body.SourceExternalID).Scan(&sourceID)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return err
		}
		if sourceID != profileID {
			if err := s.mergeProfiles(ctx, tx, event, sourceID, profileID); err != nil {
				return err
			}
		}
	case "identity.unmerge":
		if err := s.unmergeProfile(ctx, tx, event); err != nil {
			return err
		}
	case "message.impression":
		var body struct {
			MessageID string `json:"message_id"`
		}
		if err := json.Unmarshal(event.Payload, &body); err != nil {
			return err
		}
		if body.MessageID == "" {
			return errors.New("message.impression payload requires message_id")
		}
		_, err = tx.Exec(ctx, `UPDATE inapp_messages
			SET displayed_at=now(), status='displayed', updated_at=now()
			WHERE id=$1 AND tenant_id=$2 AND dismissed_at IS NULL
			  AND displayed_at IS NULL AND clicked_at IS NULL`,
			body.MessageID, event.Principal.TenantID)
		if err != nil {
			return err
		}
	case "message.clicked":
		var body struct {
			MessageID string `json:"message_id"`
		}
		if err := json.Unmarshal(event.Payload, &body); err != nil {
			return err
		}
		if body.MessageID == "" {
			return errors.New("message.clicked payload requires message_id")
		}
		_, err = tx.Exec(ctx, `UPDATE inapp_messages
			SET clicked_at=now(), status='clicked', updated_at=now()
			WHERE id=$1 AND tenant_id=$2 AND clicked_at IS NULL`,
			body.MessageID, event.Principal.TenantID)
		if err != nil {
			return err
		}
	case "message.dismissed":
		var body struct {
			MessageID string `json:"message_id"`
		}
		if err := json.Unmarshal(event.Payload, &body); err != nil {
			return err
		}
		if body.MessageID == "" {
			return errors.New("message.dismissed payload requires message_id")
		}
		_, err = tx.Exec(ctx, `UPDATE inapp_messages
			SET dismissed_at=now(), status='dismissed', updated_at=now()
			WHERE id=$1 AND tenant_id=$2 AND dismissed_at IS NULL`,
			body.MessageID, event.Principal.TenantID)
		if err != nil {
			return err
		}
	case "message.expire":
		var body struct {
			MessageID string `json:"message_id"`
		}
		if err := json.Unmarshal(event.Payload, &body); err != nil {
			return err
		}
		if body.MessageID == "" {
			return errors.New("message.expire payload requires message_id")
		}
		_, err = tx.Exec(ctx, `UPDATE inapp_messages
			SET status='expired', updated_at=now()
			WHERE id=$1 AND tenant_id=$2 AND status != 'expired'`,
			body.MessageID, event.Principal.TenantID)
		if err != nil {
			return err
		}
	}
	if err := s.projectEngagementFact(ctx, tx, event, profileID); err != nil {
		return fmt.Errorf("project engagement fact: %w", err)
	}
	if profileID != "" {
		conversionAttributed, conversionSourceType, conversionVariant, err = s.projectConversionFact(ctx, tx, event, profileID)
		if err != nil {
			return fmt.Errorf("project conversion fact: %w", err)
		}
	}
	if profileID != "" {
		if err := s.enrollEventTriggered(ctx, tx, event, profileID); err != nil {
			return fmt.Errorf("enroll event triggered: %w", err)
		}
	}
	if event.ExternalID != "" {
		if err := s.resolveWaitingRuns(ctx, tx, event); err != nil {
			return fmt.Errorf("resolve waiting runs: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE projection_jobs SET status='done',locked_until=NULL,
		completed_at=now(),last_error=NULL WHERE event_id=$1`, event.ID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if conversionAttributed {
		telemetry.RecordConversionAttributed(ctx, conversionSourceType, conversionVariant)
	}
	return nil
}

// unmergeProfile restores the source profile and its identity edges from the
// immutable merge snapshot. It is deliberately projector-only: the command is
// accepted as an event and all profile/identity mutations stay in this tx.
func (s *Store) unmergeProfile(ctx context.Context, tx pgx.Tx, event domain.AcceptedEvent) error {
	var body struct {
		MergeID         string `json:"merge_id"`
		SourceProfileID string `json:"source_profile_id"`
	}
	if err := json.Unmarshal(event.Payload, &body); err != nil {
		return err
	}
	var mergeID, sourceID, targetID, reversalRef string
	var undoneAt *time.Time
	query := `SELECT id,source_profile_id,target_profile_id,reversal_ref,undone_at
		FROM identity_merges WHERE tenant_id=$1 AND app_id=$2 AND `
	args := []any{event.Principal.TenantID, event.Principal.AppID}
	if body.MergeID != "" {
		query += "id=$3 FOR UPDATE"
		args = append(args, body.MergeID)
	} else {
		query += "source_profile_id=$3 AND undone_at IS NULL ORDER BY merged_at DESC LIMIT 1 FOR UPDATE"
		args = append(args, body.SourceProfileID)
	}
	if err := tx.QueryRow(ctx, query, args...).Scan(&mergeID, &sourceID, &targetID, &reversalRef, &undoneAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // idempotent replay of an unknown/already-retired command
		}
		return err
	}
	if undoneAt != nil {
		return nil
	}
	if reversalRef == "" || s.blobs == nil {
		return errors.New("identity merge has no reversible snapshot")
	}
	data, err := s.blobs.Get(ctx, reversalRef)
	if err != nil {
		return err
	}
	var snapshot identityMergeSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("decode identity merge snapshot: %w", err)
	}
	if snapshot.Profile.ID != sourceID {
		return errors.New("identity merge snapshot source mismatch")
	}
	if _, err := tx.Exec(ctx, `UPDATE profiles SET external_id=$1,anonymous_id=$2,
		attributes=$3,version=$4,merged_into=NULL,updated_at=$5 WHERE id=$6 AND merged_into=$7`,
		nullableString(snapshot.Profile.ExternalID), nullableString(snapshot.Profile.AnonymousID),
		snapshot.Profile.Attributes, snapshot.Profile.Version, snapshot.Profile.UpdatedAt, sourceID, targetID); err != nil {
		return err
	}
	for _, alias := range snapshot.Aliases {
		if _, err := tx.Exec(ctx, `UPDATE identity_aliases SET profile_id=$1
			WHERE tenant_id=$2 AND app_id=$3 AND namespace=$4 AND value=$5 AND profile_id=$6`,
			sourceID, event.Principal.TenantID, event.Principal.AppID, alias.Namespace, alias.Value, targetID); err != nil {
			return err
		}
	}
	for _, consent := range snapshot.Consents {
		if _, err := tx.Exec(ctx, `UPDATE consent_ledger SET profile_id=$1
			WHERE tenant_id=$2 AND app_id=$3 AND source_event_id=$4`, sourceID,
			event.Principal.TenantID, event.Principal.AppID, consent.SourceEventID); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE identity_merges SET undone_at=now() WHERE id=$1`, mergeID)
	return err
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func ensureProfile(ctx context.Context, tx pgx.Tx, event domain.AcceptedEvent) (string, error) {
	var id string
	if event.ExternalID != "" {
		if event.AnonymousID != "" {
			var anonymousProfileID string
			err := tx.QueryRow(ctx, `SELECT id FROM profiles
				WHERE tenant_id=$1 AND app_id=$2 AND anonymous_id=$3 AND merged_into IS NULL FOR UPDATE`,
				event.Principal.TenantID, event.Principal.AppID, event.AnonymousID).Scan(&anonymousProfileID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return "", err
			}
			if err == nil {
				var externalProfileID string
				extErr := tx.QueryRow(ctx, `SELECT id FROM profiles
					WHERE tenant_id=$1 AND app_id=$2 AND external_id=$3 FOR UPDATE`,
					event.Principal.TenantID, event.Principal.AppID, event.ExternalID).Scan(&externalProfileID)
				if errors.Is(extErr, pgx.ErrNoRows) {
					_, updateErr := tx.Exec(ctx, `UPDATE profiles SET external_id=$1,updated_at=now()
						WHERE id=$2`, event.ExternalID, anonymousProfileID)
					return anonymousProfileID, updateErr
				}
				if extErr != nil {
					return "", extErr
				}
				if externalProfileID != anonymousProfileID {
					if _, err := tx.Exec(ctx, `UPDATE profiles target SET
						attributes=anonymous.attributes || target.attributes,
						anonymous_id=COALESCE(target.anonymous_id,anonymous.anonymous_id),
						version=target.version+1,updated_at=now()
						FROM profiles anonymous WHERE target.id=$1 AND anonymous.id=$2`,
						externalProfileID, anonymousProfileID); err != nil {
						return "", err
					}
					if _, err := tx.Exec(ctx, "UPDATE consent_ledger SET profile_id=$1 WHERE profile_id=$2",
						externalProfileID, anonymousProfileID); err != nil {
						return "", err
					}
					if _, err := tx.Exec(ctx, "UPDATE identity_aliases SET profile_id=$1 WHERE profile_id=$2",
						externalProfileID, anonymousProfileID); err != nil {
						return "", err
					}
					if _, err := tx.Exec(ctx, `UPDATE profiles SET merged_into=$1,
						external_id=NULL,version=version+1,updated_at=now() WHERE id=$2`,
						externalProfileID, anonymousProfileID); err != nil {
						return "", err
					}
				}
				return externalProfileID, nil
			}
		}
		err := tx.QueryRow(ctx, `INSERT INTO profiles(tenant_id,workspace_id,app_id,external_id,anonymous_id)
			VALUES($1,$2,$3,$4,NULLIF($5,''))
			ON CONFLICT(tenant_id,app_id,external_id) WHERE external_id IS NOT NULL
			DO UPDATE SET updated_at=profiles.updated_at RETURNING id`,
			event.Principal.TenantID, event.Principal.WorkspaceID, event.Principal.AppID, event.ExternalID, event.AnonymousID).Scan(&id)
		return id, err
	}
	err := tx.QueryRow(ctx, `INSERT INTO profiles(tenant_id,workspace_id,app_id,anonymous_id)
		VALUES($1,$2,$3,$4)
		ON CONFLICT(tenant_id,app_id,anonymous_id) WHERE anonymous_id IS NOT NULL
		DO UPDATE SET updated_at=profiles.updated_at RETURNING id`,
		event.Principal.TenantID, event.Principal.WorkspaceID, event.Principal.AppID, event.AnonymousID).Scan(&id)
	return id, err
}

// resolveIdentity finds existing live profiles from namespaced keys before
// ensureProfile creates a new subject. Any conflict mutation remains inside
// ProjectEvent's transaction.
func (s *Store) resolveIdentity(ctx context.Context, tx pgx.Tx, event domain.AcceptedEvent) (string, error) {
	keys := identityKeys(event)
	if len(keys) == 0 {
		return "", nil
	}
	type candidate struct {
		profileID string
		priority  int
		namespace string
	}
	candidates := make([]candidate, 0, len(keys))
	seen := make(map[string]bool)
	for _, key := range keys {
		var c candidate
		err := tx.QueryRow(ctx, `SELECT ia.profile_id, n.priority, ia.namespace
			FROM identity_aliases ia
			JOIN identity_namespaces n ON n.tenant_id=ia.tenant_id
				AND n.app_id=ia.app_id AND n.namespace=ia.namespace
			JOIN profiles p ON p.id=ia.profile_id AND p.merged_into IS NULL
			WHERE ia.tenant_id=$1 AND ia.app_id=$2 AND ia.namespace=$3 AND ia.value=$4
			ORDER BY n.priority ASC, ia.profile_id ASC
			LIMIT 1 FOR UPDATE OF p`, event.Principal.TenantID, event.Principal.AppID,
			key.namespace, key.value).Scan(&c.profileID, &c.priority, &c.namespace)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !seen[c.profileID] {
			seen[c.profileID] = true
			candidates = append(candidates, c)
		}
	}
	if len(candidates) == 0 {
		return "", nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		return candidates[i].profileID < candidates[j].profileID
	})
	winner := candidates[0].profileID
	for _, loser := range candidates[1:] {
		if err := s.mergeProfiles(ctx, tx, event, loser.profileID, winner); err != nil {
			return "", err
		}
	}
	return winner, nil
}

type identityMergeSnapshot struct {
	Profile struct {
		ID          string          `json:"id"`
		ExternalID  *string         `json:"external_id"`
		AnonymousID *string         `json:"anonymous_id"`
		Attributes  json.RawMessage `json:"attributes"`
		Version     int64           `json:"version"`
		UpdatedAt   time.Time       `json:"updated_at"`
	} `json:"profile"`
	Aliases []struct {
		Namespace     string  `json:"namespace"`
		Value         string  `json:"value"`
		SourceEventID *string `json:"source_event_id"`
	} `json:"aliases"`
	Consents []struct {
		Channel       string          `json:"channel"`
		Topic         string          `json:"topic"`
		State         string          `json:"state"`
		OccurredAt    time.Time       `json:"occurred_at"`
		Evidence      json.RawMessage `json:"evidence"`
		SourceEventID string          `json:"source_event_id"`
	} `json:"consents"`
}

func (s *Store) mergeProfiles(ctx context.Context, tx pgx.Tx, event domain.AcceptedEvent, sourceID, targetID string) error {
	if sourceID == targetID {
		return nil
	}
	var snapshot identityMergeSnapshot
	if err := tx.QueryRow(ctx, `SELECT id, external_id, anonymous_id, attributes, version, updated_at
		FROM profiles WHERE id=$1 AND tenant_id=$2 AND app_id=$3 FOR UPDATE`, sourceID,
		event.Principal.TenantID, event.Principal.AppID).Scan(&snapshot.Profile.ID,
		&snapshot.Profile.ExternalID, &snapshot.Profile.AnonymousID, &snapshot.Profile.Attributes,
		&snapshot.Profile.Version, &snapshot.Profile.UpdatedAt); err != nil {
		return err
	}
	aliasRows, err := tx.Query(ctx, `SELECT namespace, value, source_event_id FROM identity_aliases
		WHERE tenant_id=$1 AND app_id=$2 AND profile_id=$3 ORDER BY namespace, value`,
		event.Principal.TenantID, event.Principal.AppID, sourceID)
	if err != nil {
		return err
	}
	for aliasRows.Next() {
		var namespace, value string
		var sourceEventID *string
		if err := aliasRows.Scan(&namespace, &value, &sourceEventID); err != nil {
			aliasRows.Close()
			return err
		}
		snapshot.Aliases = append(snapshot.Aliases, struct {
			Namespace     string  `json:"namespace"`
			Value         string  `json:"value"`
			SourceEventID *string `json:"source_event_id"`
		}{namespace, value, sourceEventID})
	}
	if err := aliasRows.Err(); err != nil {
		aliasRows.Close()
		return err
	}
	aliasRows.Close()
	consentRows, err := tx.Query(ctx, `SELECT channel, topic, state, occurred_at, evidence, source_event_id
		FROM consent_ledger WHERE tenant_id=$1 AND app_id=$2 AND profile_id=$3 ORDER BY channel, topic, occurred_at, source_event_id`,
		event.Principal.TenantID, event.Principal.AppID, sourceID)
	if err != nil {
		return err
	}
	for consentRows.Next() {
		var channel, topic, state, sourceEventID string
		var occurredAt time.Time
		var evidence json.RawMessage
		if err := consentRows.Scan(&channel, &topic, &state, &occurredAt, &evidence, &sourceEventID); err != nil {
			consentRows.Close()
			return err
		}
		snapshot.Consents = append(snapshot.Consents, struct {
			Channel       string          `json:"channel"`
			Topic         string          `json:"topic"`
			State         string          `json:"state"`
			OccurredAt    time.Time       `json:"occurred_at"`
			Evidence      json.RawMessage `json:"evidence"`
			SourceEventID string          `json:"source_event_id"`
		}{channel, topic, state, occurredAt, evidence, sourceEventID})
	}
	if err := consentRows.Err(); err != nil {
		consentRows.Close()
		return err
	}
	consentRows.Close()
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("identity-merges/%s/%s/%s/%s.json", event.Principal.TenantID, event.Principal.AppID, event.ID, sourceID)
	if s.blobs == nil {
		return errors.New("identity merge snapshot blob store is not configured")
	}
	if err := s.blobs.Put(ctx, key, data, "application/json"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE profiles target SET
		attributes=source.attributes || target.attributes,
		version=target.version+1,updated_at=now()
		FROM profiles source WHERE target.id=$1 AND source.id=$2`, targetID, sourceID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE consent_ledger SET profile_id=$1
		WHERE profile_id=$2`, targetID, sourceID); err != nil {
		return err
	}
	// Avoid unique alias conflicts while retaining the source aliases in the snapshot.
	if _, err := tx.Exec(ctx, `DELETE FROM identity_aliases source
		WHERE source.profile_id=$1 AND EXISTS (
			SELECT 1 FROM identity_aliases target
			WHERE target.profile_id=$2 AND target.namespace=source.namespace AND target.value=source.value)`, sourceID, targetID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE identity_aliases SET profile_id=$1
		WHERE profile_id=$2`, targetID, sourceID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE profiles SET merged_into=$1, external_id=NULL, anonymous_id=NULL,
		version=version+1,updated_at=now() WHERE id=$2`, targetID, sourceID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO identity_merges
		(tenant_id,app_id,source_profile_id,target_profile_id,source_event_id,policy_version,
		 winner_policy,reversible,reversal_ref,actor_user_id,actor_type)
		VALUES($1,$2,$3,$4,$5,'v1', 'v1', true, $6, NULLIF($7,'')::uuid, $8)
		ON CONFLICT (source_event_id, source_profile_id) DO NOTHING`, event.Principal.TenantID, event.Principal.AppID,
		sourceID, targetID, event.ID, key, event.Principal.UserID, event.Principal.ActorType)
	return err
}

type identityKey struct {
	namespace string
	value     string
}

func identityKeys(event domain.AcceptedEvent) []identityKey {
	var body map[string]any
	if err := json.Unmarshal(event.Payload, &body); err != nil {
		return nil
	}
	values := make(map[string]string)
	for _, container := range []string{"identities", "identity"} {
		if nested, ok := body[container].(map[string]any); ok {
			for namespace, value := range nested {
				if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
					values[namespace] = normalizeIdentityValue(namespace, text)
				}
			}
		}
	}
	for _, namespace := range []string{"email", "phone", "user_id"} {
		if text, ok := body[namespace].(string); ok && strings.TrimSpace(text) != "" {
			values[namespace] = normalizeIdentityValue(namespace, text)
		}
	}
	if event.ExternalID != "" {
		values["user_id"] = normalizeIdentityValue("user_id", event.ExternalID)
	}
	namespaces := make([]string, 0, len(values))
	for namespace := range values {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)
	keys := make([]identityKey, 0, len(namespaces))
	for _, namespace := range namespaces {
		keys = append(keys, identityKey{namespace: namespace, value: values[namespace]})
	}
	return keys
}

func normalizeIdentityValue(namespace, value string) string {
	value = strings.TrimSpace(value)
	if namespace == "email" || namespace == "phone" {
		return strings.ToLower(value)
	}
	return value
}

func (s *Store) FailProjectionJob(ctx context.Context, eventID string, jobErr error) error {
	message := jobErr.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	tag, err := s.pool.Exec(ctx, `UPDATE projection_jobs SET
		status=CASE WHEN attempts >= 10 THEN 'dead' ELSE 'pending' END,
		available_at=now()+(LEAST(attempts,10) * interval '5 seconds'),
		locked_until=NULL,last_error=$2 WHERE event_id=$1`, eventID, message)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("projection job %s not found", eventID)
	}
	return nil
}

func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}
