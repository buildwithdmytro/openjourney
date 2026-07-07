package analytics

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/buildwithdmytro/openjourney/internal/stream"
)

type Sink struct {
	connection clickhouse.Conn
}

func Open(ctx context.Context, address, database, username, password string) (*Sink, error) {
	connection, err := clickhouse.Open(&clickhouse.Options{
		Addr:        []string{address},
		Auth:        clickhouse.Auth{Database: database, Username: username, Password: password},
		DialTimeout: 10 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	if err := connection.Ping(ctx); err != nil {
		return nil, err
	}
	if err := connection.Exec(ctx, `CREATE TABLE IF NOT EXISTS behavior_events (
		tenant_id UUID,
		event_id UUID,
		workspace_id UUID,
		app_id UUID,
		event_type LowCardinality(String),
		schema_version UInt32,
		external_id String,
		anonymous_id String,
		subject_hash FixedString(64),
		occurred_at DateTime64(3, 'UTC'),
		received_at DateTime64(3, 'UTC'),
		data_classification LowCardinality(String),
		payload String,
		raw String,
		ingested_at DateTime64(3, 'UTC') DEFAULT now64(3)
	) ENGINE=ReplacingMergeTree(ingested_at)
	ORDER BY (tenant_id,event_id)
	PARTITION BY toYYYYMM(occurred_at)
	TTL toDateTime(occurred_at) + INTERVAL 396 DAY DELETE`); err != nil {
		return nil, err
	}
	return &Sink{connection: connection}, nil
}

func (s *Sink) Close() error { return s.connection.Close() }

type envelope struct {
	TenantID           string          `json:"tenant_id"`
	EventID            string          `json:"event_id"`
	WorkspaceID        string          `json:"workspace_id"`
	AppID              string          `json:"app_id"`
	EventType          string          `json:"event_type"`
	SchemaVersion      uint32          `json:"schema_version"`
	ExternalID         string          `json:"external_id"`
	AnonymousID        string          `json:"anonymous_id"`
	OccurredAt         time.Time       `json:"occurred_at"`
	ReceivedAt         time.Time       `json:"received_at"`
	DataClassification string          `json:"data_classification"`
	Payload            json.RawMessage `json:"payload"`
}

func (s *Sink) Run(ctx context.Context, consumer *stream.Consumer) error {
	for {
		record, err := consumer.Poll(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		var event envelope
		if err := json.Unmarshal(record.Value, &event); err != nil {
			return fmt.Errorf("decode event: %w", err)
		}
		if event.EventType == "privacy.deleted" {
			var payload struct {
				SubjectHash string `json:"subject_hash"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return err
			}
			if err := s.connection.Exec(ctx, `DELETE FROM behavior_events
				WHERE tenant_id=? AND subject_hash=?`, event.TenantID, payload.SubjectHash); err != nil {
				return err
			}
			if err := consumer.Commit(ctx, record); err != nil {
				return err
			}
			continue
		}
		subject := event.ExternalID
		if subject == "" {
			subject = event.AnonymousID
		}
		subjectHash := fmt.Sprintf("%x", sha256.Sum256([]byte(subject)))
		if err := s.connection.Exec(ctx, `INSERT INTO behavior_events
			(tenant_id,event_id,workspace_id,app_id,event_type,schema_version,external_id,anonymous_id,subject_hash,
			 occurred_at,received_at,data_classification,payload,raw)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			event.TenantID, event.EventID, event.WorkspaceID, event.AppID, event.EventType,
			event.SchemaVersion, event.ExternalID, event.AnonymousID, subjectHash, event.OccurredAt, event.ReceivedAt,
			event.DataClassification, string(event.Payload), string(record.Value)); err != nil {
			return err
		}
		if err := consumer.Commit(ctx, record); err != nil {
			return err
		}
	}
}
