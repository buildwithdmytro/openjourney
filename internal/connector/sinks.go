package connector

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/minio/minio-go/v7"
)

type objectWriter interface {
	Put(context.Context, string, string, io.Reader) error
}

type minioObjectWriter struct{ client *minio.Client }

func (w minioObjectWriter) Put(ctx context.Context, bucket, key string, body io.Reader) error {
	_, err := w.client.PutObject(ctx, bucket, key, body, -1, minio.PutObjectOptions{ContentType: "application/x-ndjson"})
	return err
}

// S3Sink writes a content-addressed JSONL object. Replaying the same batch
// overwrites the same key, making retries idempotent at the object store.
type S3Sink struct{ writer objectWriter }

func NewS3Sink() *S3Sink { return &S3Sink{} }
func NewS3SinkWithWriter(w interface {
	Put(context.Context, string, string, io.Reader) error
}) *S3Sink {
	return &S3Sink{writer: w}
}

func (s *S3Sink) Read(context.Context, map[string]any, string) ([]Row, string, error) {
	return nil, "", errors.New("s3 sink does not support reads")
}

func (s *S3Sink) Write(ctx context.Context, cfg map[string]any, rows []Row) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	endpoint, bucket, err := validateS3SinkConfig(cfg)
	if err != nil {
		return 0, err
	}
	writer := s.writer
	if writer == nil {
		objects, err := newMinioObjects(ctx, cfg)
		if err != nil {
			return 0, err
		}
		writer = minioObjectWriter{client: objects.(minioObjects).client}
	}
	data := bytes.NewBuffer(nil)
	enc := json.NewEncoder(data)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			return 0, fmt.Errorf("encode S3 sink row: %w", err)
		}
	}
	hash := sha256.Sum256(data.Bytes())
	prefix, _ := cfg["object_prefix"].(string)
	key := strings.TrimRight(prefix, "/") + "/" + hex.EncodeToString(hash[:]) + ".jsonl"
	if configured, ok := cfg["object_key"].(string); ok && configured != "" {
		key = configured
	}
	if err := writer.Put(ctx, bucket, key, bytes.NewReader(data.Bytes())); err != nil {
		return 0, fmt.Errorf("write S3 sink object: %w", err)
	}
	_ = endpoint // endpoint is validated even when an injected writer is used.
	return len(rows), nil
}

func validateS3SinkConfig(cfg map[string]any) (string, string, error) {
	endpoint, _ := cfg["endpoint"].(string)
	bucket, _ := cfg["bucket"].(string)
	accessRef, _ := cfg["access_key_ref"].(string)
	secretRef, _ := cfg["secret_key_ref"].(string)
	access, accessOK := cfg["access_key"].(string)
	secret, secretOK := cfg["secret_key"].(string)
	if endpoint == "" || bucket == "" || accessRef == "" || secretRef == "" || !accessOK || !secretOK || access == "" || secret == "" {
		return "", "", errors.New("S3 sink requires endpoint, bucket, access_key_ref, and secret_key_ref")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" {
		return "", "", errors.New("S3 endpoint must be a URL")
	}
	allowed := false
	for _, host := range stringSlice(cfg["endpoint_allowlist"]) {
		if host == u.Host || host == u.Hostname() || strings.TrimRight(host, "/") == strings.TrimRight(endpoint, "/") {
			allowed = true
		}
	}
	if err := channels.IsSafeURL(endpoint); err != nil && !allowed {
		return "", "", fmt.Errorf("S3 endpoint is not SSRF-safe or allowlisted: %w", err)
	}
	return endpoint, bucket, nil
}

type clickHouseExec interface {
	Exec(context.Context, string, ...any) error
}
type clickHouseExecAdapter struct{ conn driver.Conn }

func (a clickHouseExecAdapter) Exec(ctx context.Context, query string, args ...any) error {
	return a.conn.Exec(ctx, query, args...)
}

// ClickHouseSink batches a parameterized INSERT. The token and local key set
// make redelivery of the same batch a no-op for replicated ClickHouse tables.
type ClickHouseSink struct {
	conn clickHouseExec
	mu   sync.Mutex
	seen map[string]struct{}
}

func NewClickHouseSink() *ClickHouseSink { return &ClickHouseSink{seen: make(map[string]struct{})} }
func NewClickHouseSinkWithConn(conn interface {
	Exec(context.Context, string, ...any) error
}) *ClickHouseSink {
	return &ClickHouseSink{conn: conn, seen: make(map[string]struct{})}
}
func (s *ClickHouseSink) Read(context.Context, map[string]any, string) ([]Row, string, error) {
	return nil, "", errors.New("ClickHouse sink does not support reads")
}
func (s *ClickHouseSink) Write(ctx context.Context, cfg map[string]any, rows []Row) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	table, _ := cfg["table"].(string)
	if !isSafeClickHouseIdentifier(table) {
		return 0, errors.New("ClickHouse sink table is invalid")
	}
	cols := stringSlice(cfg["columns"])
	if len(cols) == 0 {
		cols = sortedRowColumns(rows[0])
	}
	for _, col := range cols {
		if !isSafeClickHouseIdentifier(col) {
			return 0, errors.New("ClickHouse sink column is invalid")
		}
	}
	args := make([]any, 0, len(rows)*len(cols))
	placeholders := make([]string, 0, len(rows))
	newRows := 0
	for _, row := range rows {
		key := canonicalRowKey(row, cfg)
		s.mu.Lock()
		_, exists := s.seen[key]
		s.mu.Unlock()
		if exists {
			continue
		}
		values := make([]string, len(cols))
		for i, col := range cols {
			values[i] = "?"
			args = append(args, row[col])
		}
		placeholders = append(placeholders, "("+strings.Join(values, ",")+")")
		newRows++
	}
	if newRows == 0 {
		return 0, nil
	}
	query := "INSERT INTO " + table + " (" + strings.Join(cols, ",") + ") VALUES " + strings.Join(placeholders, ",")
	if s.conn == nil {
		address, _ := cfg["endpoint"].(string)
		database, _ := cfg["database"].(string)
		userRef, _ := cfg["username_ref"].(string)
		passRef, _ := cfg["password_ref"].(string)
		user, _ := cfg["username"].(string)
		pass, _ := cfg["password"].(string)
		if address == "" || database == "" || userRef == "" || passRef == "" || user == "" || pass == "" {
			return 0, errors.New("ClickHouse sink requires endpoint, database, username_ref, and password_ref")
		}
		if err := validateClickHouseEndpoint(address, stringSlice(cfg["endpoint_allowlist"])); err != nil {
			return 0, err
		}
		conn, err := clickhouse.Open(&clickhouse.Options{Addr: []string{address}, Auth: clickhouse.Auth{Database: database, Username: user, Password: pass}, DialTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second})
		if err != nil {
			return 0, err
		}
		defer conn.Close()
		s.conn = clickHouseExecAdapter{conn: conn}
	}
	if err := s.conn.Exec(ctx, query, args...); err != nil {
		return 0, fmt.Errorf("write ClickHouse sink: %w", err)
	}
	for _, row := range rows {
		key := canonicalRowKey(row, cfg)
		s.mu.Lock()
		s.seen[key] = struct{}{}
		s.mu.Unlock()
	}
	return newRows, nil
}

func sortedRowColumns(row Row) []string {
	out := make([]string, 0, len(row))
	for key := range row {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
func canonicalRowKey(row Row, cfg map[string]any) string {
	key, _ := cfg["upsert_key"].(string)
	if key != "" {
		if value, ok := row[key]; ok {
			return fmt.Sprint(value)
		}
	}
	data, _ := json.Marshal(row)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func validateClickHouseEndpoint(address string, allow []string) error {
	host := address
	if parsed, err := url.Parse(address); err == nil && parsed.Host != "" {
		host = parsed.Host
	}
	if err := channels.IsSafeURL("http://" + host); err != nil {
		for _, entry := range allow {
			if strings.TrimRight(entry, "/") == host || strings.TrimRight(entry, "/") == address {
				return nil
			}
		}
		return fmt.Errorf("ClickHouse endpoint is not SSRF-safe or allowlisted: %w", err)
	}
	return nil
}

// WebhookSink POSTs one bounded, HMAC-signed row at a time. The idempotency
// key is sent to the receiver so retries can be upserted there.
type WebhookSink struct {
	client *http.Client
	mu     sync.Mutex
	seen   map[string]struct{}
}

func NewWebhookSink() *WebhookSink {
	return &WebhookSink{client: &http.Client{Timeout: 10 * time.Second}, seen: make(map[string]struct{})}
}
func (s *WebhookSink) Read(context.Context, map[string]any, string) ([]Row, string, error) {
	return nil, "", errors.New("webhook sink does not support reads")
}
func (s *WebhookSink) Write(ctx context.Context, cfg map[string]any, rows []Row) (int, error) {
	endpoint, _ := cfg["endpoint"].(string)
	secretRef, _ := cfg["hmac_secret_ref"].(string)
	secret, _ := cfg["hmac_secret"].(string)
	if endpoint == "" || secretRef == "" || secret == "" {
		return 0, errors.New("webhook sink requires endpoint and hmac_secret_ref")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Hostname() == "" {
		return 0, errors.New("webhook endpoint must be a URL")
	}
	allowed := false
	for _, host := range stringSlice(cfg["endpoint_allowlist"]) {
		if host == parsed.Host || host == parsed.Hostname() || strings.TrimRight(host, "/") == strings.TrimRight(endpoint, "/") {
			allowed = true
		}
	}
	if err := channels.IsSafeURL(endpoint); err != nil && !allowed {
		return 0, fmt.Errorf("webhook endpoint is not SSRF-safe or allowlisted: %w", err)
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: guardedTransport(parsed.Hostname(), allowed)}
	written := 0
	for _, row := range rows {
		body, err := json.Marshal(row)
		if err != nil {
			return written, err
		}
		key := canonicalRowKey(row, cfg)
		s.mu.Lock()
		_, seen := s.seen[key]
		s.mu.Unlock()
		if seen {
			continue
		}
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(body)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return written, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Idempotency-Key", key)
		req.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
		resp, err := client.Do(req)
		if err != nil {
			return written, fmt.Errorf("webhook sink request: %w", err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return written, fmt.Errorf("webhook sink returned status %d", resp.StatusCode)
		}
		s.mu.Lock()
		s.seen[key] = struct{}{}
		s.mu.Unlock()
		written++
	}
	return written, nil
}
