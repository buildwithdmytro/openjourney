package connector

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/buildwithdmytro/openjourney/internal/channels"
)

type clickHouseQueryer interface {
	Query(context.Context, string, ...any) (driver.Rows, error)
}

type ClickHouseDriver struct{ conn clickHouseQueryer }

func NewClickHouseDriver() *ClickHouseDriver { return &ClickHouseDriver{} }

// NewClickHouseDriverWithConn makes the bounded query and cursor behavior
// testable without requiring a warehouse in the unit-test environment.
func NewClickHouseDriverWithConn(conn interface {
	Query(context.Context, string, ...any) (driver.Rows, error)
}) *ClickHouseDriver {
	return &ClickHouseDriver{conn: conn}
}

var clickHouseIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (d *ClickHouseDriver) Read(ctx context.Context, cfg map[string]any, cursor string) ([]Row, string, error) {
	conn := d.conn
	if conn == nil {
		var err error
		conn, err = openClickHouse(ctx, cfg)
		if err != nil {
			return nil, cursor, err
		}
	}
	query, _ := cfg["query"].(string)
	watermark, _ := cfg["watermark_column"].(string)
	if !isSafeClickHouseIdentifier(watermark) {
		return nil, cursor, errors.New("ClickHouse watermark_column is invalid")
	}
	query = strings.TrimSpace(query)
	if !strings.HasPrefix(strings.ToUpper(query), "SELECT ") || strings.Contains(query, ";") {
		return nil, cursor, errors.New("ClickHouse query must be a single SELECT")
	}
	limit := configInt(cfg, "max_rows", 1000)
	if limit <= 0 || limit > 10000 {
		return nil, cursor, errors.New("ClickHouse max_rows must be between 1 and 10000")
	}
	quoted := "`" + watermark + "`"
	sql := "SELECT * FROM (" + query + ") AS source"
	args := []any{}
	if cursor != "" {
		sql += " WHERE source." + quoted + " > ?"
		args = append(args, cursor)
	}
	sql += " ORDER BY source." + quoted + " ASC LIMIT ?"
	args = append(args, limit)
	rows, err := conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, cursor, fmt.Errorf("query ClickHouse: %w", err)
	}
	defer rows.Close()
	columns := rows.Columns()
	out := make([]Row, 0, limit)
	next := cursor
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, cursor, fmt.Errorf("scan ClickHouse row: %w", err)
		}
		row := Row{}
		for i, name := range columns {
			row[name] = values[i]
		}
		if value, ok := row[watermark]; ok && value != nil {
			next = fmt.Sprint(value)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, cursor, fmt.Errorf("read ClickHouse rows: %w", err)
	}
	return out, next, nil
}

func (d *ClickHouseDriver) Write(context.Context, map[string]any, []Row) (int, error) {
	return 0, errors.New("ClickHouse source driver does not support writes")
}

func isSafeClickHouseIdentifier(value string) bool { return clickHouseIdentifier.MatchString(value) }

func openClickHouse(ctx context.Context, cfg map[string]any) (clickHouseQueryer, error) {
	address, _ := cfg["endpoint"].(string)
	if address == "" {
		address, _ = cfg["address"].(string)
	}
	database, _ := cfg["database"].(string)
	usernameRef, _ := cfg["username_ref"].(string)
	passwordRef, _ := cfg["password_ref"].(string)
	username, _ := cfg["username"].(string)
	password, _ := cfg["password"].(string)
	if address == "" || database == "" || usernameRef == "" || passwordRef == "" || username == "" || password == "" {
		return nil, errors.New("ClickHouse requires endpoint, database, username_ref, and password_ref")
	}
	host := address
	if parsed, err := url.Parse(address); err == nil && parsed.Host != "" {
		host = parsed.Host
	}
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		hostname = host
	}
	allow := stringSlice(cfg["endpoint_allowlist"])
	allowed := false
	for _, entry := range allow {
		entryHost := strings.TrimRight(entry, "/")
		if parsed, parseErr := url.Parse(entryHost); parseErr == nil && parsed.Host != "" {
			entryHost = parsed.Host
		}
		if entryHost == host || entryHost == hostname {
			allowed = true
		}
	}
	if err := channels.IsSafeURL("http://" + host); err != nil && !allowed {
		return nil, fmt.Errorf("ClickHouse endpoint is not SSRF-safe or allowlisted: %w", err)
	}
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{host}, Auth: clickhouse.Auth{Database: database, Username: username, Password: password},
		DialTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		DialContext: guardedClickHouseDial(host, allowed),
	})
	if err != nil {
		return nil, err
	}
	if err := conn.Ping(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("validate ClickHouse endpoint: %w", err)
	}
	return conn, nil
}

func guardedClickHouseDial(host string, allowPrivate bool) func(context.Context, string) (net.Conn, error) {
	return func(ctx context.Context, _ string) (net.Conn, error) {
		name, port, err := net.SplitHostPort(host)
		if err != nil {
			name, port = host, "9000"
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", name)
		if err != nil || len(ips) == 0 {
			return nil, fmt.Errorf("resolve ClickHouse host: %w", err)
		}
		for _, ip := range ips {
			if channels.IsPrivateIP(ip) && !allowPrivate {
				return nil, fmt.Errorf("forbidden ClickHouse private IP: %s", ip)
			}
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ips[0].String(), port))
	}
}
