package connector_test

import (
	"context"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/connector"
)

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
