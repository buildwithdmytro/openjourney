package publishing

import (
	"context"
	"errors"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type fixtureVersion struct {
	Manifest  string
	Publishes int
}

type fixtureBlobs struct {
	key  string
	data []byte
	puts int
}

func (b *fixtureBlobs) Put(_ context.Context, key string, data []byte, contentType string) error {
	if contentType != "application/json" {
		return errors.New("unexpected content type")
	}
	b.key, b.data, b.puts = key, append([]byte(nil), data...), b.puts+1
	return nil
}

func TestPublishFreezesAndRetriesIdempotently(t *testing.T) {
	blobs := &fixtureBlobs{}
	var stored fixtureVersion
	commit := func(_ context.Context, _ domain.Principal, _ string, _ string, manifest string) (fixtureVersion, error) {
		if stored.Manifest == manifest {
			return stored, nil
		}
		stored = fixtureVersion{Manifest: manifest, Publishes: stored.Publishes + 1}
		return stored, nil
	}
	p := domain.Principal{TenantID: "tenant-1", ActorType: "user", UserID: "user-1"}
	canonical := func(v map[string]string) ([]byte, error) { return []byte(`{"body":"pinned"}`), nil }

	one, err := Publish(context.Background(), p, "resource-1", "forms", map[string]string{"draft": "ignored"}, blobs, canonical, commit)
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	two, err := Publish(context.Background(), p, "resource-1", "forms", map[string]string{"draft": "ignored"}, blobs, canonical, commit)
	if err != nil {
		t.Fatalf("retry publish: %v", err)
	}
	if one != two || stored.Publishes != 1 {
		t.Fatalf("publish was not idempotent: first=%+v second=%+v stored=%+v", one, two, stored)
	}
	if blobs.puts != 2 || blobs.key != stored.Manifest || string(blobs.data) != `{"body":"pinned"}` {
		t.Fatalf("manifest was not frozen deterministically: %+v", blobs)
	}
}

func TestPublishRejectsAPIKeyBeforeBlobOrCommit(t *testing.T) {
	blobs := &fixtureBlobs{}
	committed := false
	_, err := Publish(context.Background(), domain.Principal{TenantID: "tenant-1", ActorType: "api_key", KeyID: "key-1"}, "resource-1", "pages", struct{}{}, blobs,
		func(struct{}) ([]byte, error) { return []byte("{}"), nil },
		func(context.Context, domain.Principal, string, string, string) (fixtureVersion, error) {
			committed = true
			return fixtureVersion{}, nil
		})
	if !errors.Is(err, ErrHumanActorRequired) {
		t.Fatalf("expected human actor error, got %v", err)
	}
	if blobs.puts != 0 || committed {
		t.Fatal("non-human publish reached blob store or commit")
	}
}
