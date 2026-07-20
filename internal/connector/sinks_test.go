package connector

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type sinkObject struct {
	bucket, key string
	body        []byte
}

func (o *sinkObject) Put(_ context.Context, bucket, key string, body io.Reader) error {
	o.bucket, o.key = bucket, key
	o.body, _ = io.ReadAll(body)
	return nil
}

func sinkConfig() map[string]any {
	return map[string]any{"endpoint": "https://objects.example.test", "endpoint_allowlist": []any{"objects.example.test"}, "bucket": "exports", "access_key_ref": "S3_ACCESS_KEY", "secret_key_ref": "S3_SECRET_KEY", "access_key": "access", "secret_key": "secret"}
}

func TestS3SinkRedeliveryOverwritesContentAddressedObject(t *testing.T) {
	object := &sinkObject{}
	sink := NewS3SinkWithWriter(object)
	rows := []Row{{"external_id": "p-1", "email": "one@example.test"}}
	first, err := sink.Write(context.Background(), sinkConfig(), rows)
	if err != nil || first != 1 {
		t.Fatalf("first S3 write = %d, %v", first, err)
	}
	key := object.key
	second, err := sink.Write(context.Background(), sinkConfig(), rows)
	if err != nil || second != 1 {
		t.Fatalf("redelivery S3 write = %d, %v", second, err)
	}
	if object.key != key || !bytes.Contains(object.body, []byte(`"external_id":"p-1"`)) {
		t.Fatal("redelivery did not overwrite the same deterministic object")
	}
}

type fakeClickHouseSink struct{ calls int }

func (f *fakeClickHouseSink) Exec(_ context.Context, query string, args ...any) error {
	f.calls++
	if !strings.Contains(query, "INSERT INTO audience_members") || len(args) != 2 {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func TestClickHouseSinkUpsertKeyDeduplicatesRedelivery(t *testing.T) {
	fake := &fakeClickHouseSink{}
	sink := NewClickHouseSinkWithConn(fake)
	cfg := map[string]any{"table": "audience_members", "upsert_key": "external_id"}
	rows := []Row{{"external_id": "p-1", "value": "x"}}
	if n, err := sink.Write(context.Background(), cfg, rows); err != nil || n != 1 {
		t.Fatalf("first ClickHouse write = %d, %v", n, err)
	}
	if n, err := sink.Write(context.Background(), cfg, rows); err != nil || n != 0 {
		t.Fatalf("redelivery ClickHouse write = %d, %v", n, err)
	}
	if fake.calls != 1 {
		t.Fatalf("expected one INSERT, got %d", fake.calls)
	}
}

func TestWebhookSinkSignsAndDeduplicatesRedelivery(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Idempotency-Key") == "" || r.Header.Get("X-Signature") == "" {
			t.Error("webhook headers missing")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	sink := NewWebhookSink()
	rows := []Row{{"external_id": "p-1"}}
	cfg := map[string]any{"endpoint": server.URL, "endpoint_allowlist": []any{"127.0.0.1"}, "hmac_secret_ref": "WEBHOOK_SECRET", "hmac_secret": "secret"}
	if n, err := sink.Write(context.Background(), cfg, rows); err != nil || n != 1 {
		t.Fatalf("first webhook write = %d, %v", n, err)
	}
	if n, err := sink.Write(context.Background(), cfg, rows); err != nil || n != 0 {
		t.Fatalf("redelivery webhook write = %d, %v", n, err)
	}
	if calls != 1 {
		t.Fatalf("expected one HTTP delivery, got %d", calls)
	}
}

func TestWebhookSinkRejectsRawSecretWithoutReference(t *testing.T) {
	sink := NewWebhookSink()
	if _, err := sink.Write(context.Background(), map[string]any{"endpoint": "https://hooks.example.test", "hmac_secret": "raw"}, []Row{{"id": "1"}}); err == nil {
		t.Fatal("raw webhook secret without *_ref must be rejected")
	}
}
