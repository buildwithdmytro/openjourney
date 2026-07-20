package connector

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type objectClient interface {
	List(context.Context, string) ([]string, error)
	Open(context.Context, string) (io.ReadCloser, error)
}

type minioObjects struct {
	client *minio.Client
	bucket string
}

func (m minioObjects) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for obj := range m.client.ListObjects(ctx, m.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		keys = append(keys, obj.Key)
	}
	sort.Strings(keys)
	return keys, nil
}

func (m minioObjects) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	object, err := m.client.GetObject(ctx, m.bucket, key, minio.GetObjectOptions{})
	return object, err
}

// S3Driver reads CSV and JSONL objects in lexicographic key order. The cursor
// is the next row to read, encoded as <object-key>:<zero-based-row>.
type S3Driver struct{ client objectClient }

func NewS3Driver() *S3Driver { return &S3Driver{} }
func NewS3DriverWithClient(client interface {
	List(context.Context, string) ([]string, error)
	Open(context.Context, string) (io.ReadCloser, error)
}) *S3Driver {
	return &S3Driver{client: client}
}

func (d *S3Driver) Read(ctx context.Context, cfg map[string]any, cursor string) ([]Row, string, error) {
	client := d.client
	if client == nil {
		var err error
		client, err = newMinioObjects(ctx, cfg)
		if err != nil {
			return nil, cursor, err
		}
	}
	prefix, _ := cfg["prefix"].(string)
	keys, err := client.List(ctx, prefix)
	if err != nil {
		return nil, cursor, fmt.Errorf("list S3 objects: %w", err)
	}
	sort.Strings(keys)
	startKey, startRow := cursorParts(cursor)
	limit := configInt(cfg, "max_rows", 1000)
	if limit <= 0 {
		return nil, cursor, errors.New("max_rows must be positive")
	}
	var out []Row
	last := cursor
	for _, key := range keys {
		if startKey != "" && key < startKey {
			continue
		}
		rowStart := 0
		if key == startKey {
			rowStart = startRow
		}
		if !supportedObject(key) {
			continue
		}
		reader, err := client.Open(ctx, key)
		if err != nil {
			return nil, cursor, fmt.Errorf("open S3 object %q: %w", key, err)
		}
		rows, err := parseObject(key, reader)
		reader.Close()
		if err != nil {
			return nil, cursor, fmt.Errorf("parse S3 object %q: %w", key, err)
		}
		for i := rowStart; i < len(rows); i++ {
			if len(out) == limit {
				return out, fmt.Sprintf("%s:%d", key, i), nil
			}
			out = append(out, rows[i])
			last = fmt.Sprintf("%s:%d", key, i+1)
		}
		startRow = 0
	}
	return out, last, nil
}

func (d *S3Driver) Write(context.Context, map[string]any, []Row) (int, error) {
	return 0, errors.New("s3 source driver does not support writes")
}

func supportedObject(key string) bool {
	ext := strings.ToLower(path.Ext(key))
	return ext == ".csv" || ext == ".jsonl" || ext == ".ndjson"
}

func parseObject(key string, r io.Reader) ([]Row, error) {
	if strings.ToLower(path.Ext(key)) == ".csv" {
		cr := csv.NewReader(r)
		headers, err := cr.Read()
		if err != nil {
			return nil, err
		}
		for i := range headers {
			headers[i] = strings.TrimSpace(headers[i])
		}
		var rows []Row
		for {
			values, err := cr.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			if len(values) != len(headers) {
				return nil, fmt.Errorf("row has %d fields, want %d", len(values), len(headers))
			}
			row := Row{}
			for i, value := range values {
				row[headers[i]] = value
			}
			rows = append(rows, row)
		}
		return rows, nil
	}
	var rows []Row
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for line := 1; s.Scan(); line++ {
		if strings.TrimSpace(s.Text()) == "" {
			continue
		}
		var row Row
		if err := json.Unmarshal(s.Bytes(), &row); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		rows = append(rows, row)
	}
	return rows, s.Err()
}

func cursorParts(cursor string) (string, int) {
	i := strings.LastIndexByte(cursor, ':')
	if i < 0 {
		return "", 0
	}
	n, err := strconv.Atoi(cursor[i+1:])
	if err != nil || n < 0 {
		return cursor, 0
	}
	return cursor[:i], n
}

func configInt(cfg map[string]any, key string, fallback int) int {
	switch n := cfg[key].(type) {
	case int:
		return n
	case float64:
		return int(n)
	case string:
		v, _ := strconv.Atoi(n)
		if v != 0 {
			return v
		}
	}
	return fallback
}

func newMinioObjects(ctx context.Context, cfg map[string]any) (objectClient, error) {
	endpoint, _ := cfg["endpoint"].(string)
	bucket, _ := cfg["bucket"].(string)
	accessRef, _ := cfg["access_key_ref"].(string)
	secretRef, _ := cfg["secret_key_ref"].(string)
	access, _ := cfg["access_key"].(string)
	secret, _ := cfg["secret_key"].(string)
	if endpoint == "" || bucket == "" || accessRef == "" || secretRef == "" {
		return nil, errors.New("S3 requires endpoint, bucket, access_key_ref, and secret_key_ref")
	}
	if _, ok := cfg["access_key"].(string); !ok || access == "" || secret == "" {
		return nil, errors.New("S3 secret refs did not resolve")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" {
		return nil, errors.New("S3 endpoint must be a URL")
	}
	allow := stringSlice(cfg["endpoint_allowlist"])
	allowed := false
	for _, host := range allow {
		if host == u.Host || host == u.Hostname() || strings.TrimRight(host, "/") == strings.TrimRight(endpoint, "/") {
			allowed = true
		}
	}
	if err := channels.IsSafeURL(endpoint); err != nil && !allowed {
		return nil, fmt.Errorf("S3 endpoint is not SSRF-safe or allowlisted: %w", err)
	}
	transport := guardedTransport(u.Hostname(), allowed)
	secure := u.Scheme == "https"
	client, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(access, secret, ""), Secure: secure, Transport: transport})
	if err != nil {
		return nil, err
	}
	if _, err := client.ListBuckets(ctx); err != nil {
		return nil, fmt.Errorf("validate S3 endpoint: %w", err)
	}
	return minioObjects{client: client, bucket: bucket}, nil
}

func stringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func guardedTransport(host string, allowPrivate bool) *http.Transport {
	return &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if channels.IsPrivateIP(ip) && !allowPrivate {
				return nil, fmt.Errorf("forbidden S3 private IP: %s", ip)
			}
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 15 * time.Second}
}
