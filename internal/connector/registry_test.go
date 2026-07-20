package connector_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/connector"
)

type fixtureObjects map[string]string

func (f fixtureObjects) List(context.Context, string) ([]string, error) {
	return []string{"z.jsonl", "a.csv"}, nil
}
func (f fixtureObjects) Open(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f[key])), nil
}

func TestDefaultRegistryResolvesFakeAndFallsBack(t *testing.T) {
	reg := connector.DefaultRegistry()
	fake := reg.For("fake")
	if fake == nil || reg.For("unknown") != fake {
		t.Fatal("fake must be the default fallback")
	}
	rows, cursor, err := fake.Read(context.Background(), nil, "cursor-1")
	if err != nil || len(rows) != 0 || cursor != "cursor-1" {
		t.Fatalf("read = %#v, %q, %v", rows, cursor, err)
	}
}

func TestS3DriverReadsDeterministicallyAndResumesCursor(t *testing.T) {
	driver := connector.NewS3DriverWithClient(fixtureObjects{
		"a.csv":   "email,name\na@example.com,A\nb@example.com,B\n",
		"z.jsonl": `{"email":"z@example.com","name":"Z"}` + "\n",
	})
	cfg := map[string]any{"prefix": "imports/", "max_rows": 2}
	rows, cursor, err := driver.Read(context.Background(), cfg, "")
	if err != nil || len(rows) != 2 || cursor != "z.jsonl:0" {
		t.Fatalf("first read = %#v, %q, %v", rows, cursor, err)
	}
	rows, cursor, err = driver.Read(context.Background(), cfg, cursor)
	if err != nil || len(rows) != 1 || rows[0]["email"] != "z@example.com" || cursor != "z.jsonl:1" {
		t.Fatalf("resumed read = %#v, %q, %v", rows, cursor, err)
	}
	rows, next, err := driver.Read(context.Background(), cfg, cursor)
	if err != nil || len(rows) != 0 || next != cursor {
		t.Fatalf("completed read = %#v, %q, %v", rows, next, err)
	}
}
