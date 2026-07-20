package connector_test

import (
	"context"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/buildwithdmytro/openjourney/internal/connector"
)

type clickHouseRows struct {
	columns []string
	rows    [][]any
	index   int
}

func (r *clickHouseRows) Next() bool { return r.index < len(r.rows) }
func (r *clickHouseRows) Scan(dest ...any) error {
	row := r.rows[r.index]
	for i := range dest {
		*(dest[i].(*any)) = row[i]
	}
	r.index++
	return nil
}
func (r *clickHouseRows) ScanStruct(any) error             { return nil }
func (r *clickHouseRows) ColumnTypes() []driver.ColumnType { return nil }
func (r *clickHouseRows) Totals(...any) error              { return nil }
func (r *clickHouseRows) Columns() []string                { return r.columns }
func (r *clickHouseRows) Close() error                     { return nil }
func (r *clickHouseRows) Err() error                       { return nil }

type clickHouseConn struct {
	query string
	args  []any
}

func (c *clickHouseConn) Query(_ context.Context, query string, args ...any) (driver.Rows, error) {
	c.query, c.args = query, args
	return &clickHouseRows{
		columns: []string{"updated_at", "email"},
		rows:    [][]any{{"2026-01-02T00:00:00Z", "a@example.com"}, {"2026-01-03T00:00:00Z", "b@example.com"}},
	}, nil
}

func TestClickHouseDriverUsesParameterizedWatermarkAndResumes(t *testing.T) {
	fake := &clickHouseConn{}
	driver := connector.NewClickHouseDriverWithConn(fake)
	cfg := map[string]any{
		"query":            "SELECT updated_at, email FROM contacts",
		"watermark_column": "updated_at",
		"max_rows":         2,
	}
	rows, cursor, err := driver.Read(context.Background(), cfg, "")
	if err != nil || len(rows) != 2 || cursor != "2026-01-03T00:00:00Z" {
		t.Fatalf("first read = %#v, %q, %v", rows, cursor, err)
	}
	if len(fake.args) != 1 || fake.args[0] != 2 {
		t.Fatalf("first query args = %#v, want parameterized limit", fake.args)
	}
	if rows[0]["email"] != "a@example.com" {
		t.Fatalf("row mapping = %#v", rows[0])
	}
	_, _, err = driver.Read(context.Background(), cfg, cursor)
	if err != nil || len(fake.args) != 2 || fake.args[0] != cursor || fake.args[1] != 2 {
		t.Fatalf("resume args = %#v, err=%v; want watermark and limit parameters", fake.args, err)
	}
	if !contains(fake.query, "source.`updated_at` > ?") || !contains(fake.query, "LIMIT ?") {
		t.Fatalf("query is not bounded/parameterized: %s", fake.query)
	}
}

func TestClickHouseDriverRejectsUnsafeQueryAndWatermark(t *testing.T) {
	driver := connector.NewClickHouseDriverWithConn(&clickHouseConn{})
	for name, cfg := range map[string]map[string]any{
		"write query":   {"query": "DELETE FROM contacts", "watermark_column": "updated_at"},
		"unsafe column": {"query": "SELECT * FROM contacts", "watermark_column": "updated_at;DROP"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := driver.Read(context.Background(), cfg, ""); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func contains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
