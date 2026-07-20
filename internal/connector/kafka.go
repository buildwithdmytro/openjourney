package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/twmb/franz-go/pkg/kgo"
)

// KafkaDriver is a bounded, manual-commit source. Its records are committed
// only through Commit, which the operation executor calls after AcceptEvents.
type KafkaDriver struct {
	mu     sync.Mutex
	client *kgo.Client
}

func NewKafkaDriver() *KafkaDriver { return &KafkaDriver{} }

func (d *KafkaDriver) Read(ctx context.Context, cfg map[string]any, _ string) ([]Row, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.client == nil {
		client, err := newKafkaClient(cfg)
		if err != nil {
			return nil, "", err
		}
		d.client = client
	}
	limit := configInt(cfg, "max_rows", 100)
	if limit < 1 || limit > 1000 {
		return nil, "", errors.New("Kafka max_rows must be between 1 and 1000")
	}
	fetches := d.client.PollFetches(ctx)
	if err := fetches.Err(); err != nil {
		return nil, "", fmt.Errorf("poll Kafka: %w", err)
	}
	rows := make([]Row, 0, limit)
	last := ""
	for _, record := range fetches.Records() {
		if len(rows) == limit {
			break
		}
		row := Row{"_kafka_record": record, "key": string(record.Key), "payload": string(record.Value),
			"topic": record.Topic, "partition": record.Partition, "offset": record.Offset}
		var decoded map[string]any
		if json.Unmarshal(record.Value, &decoded) == nil {
			for key, value := range decoded {
				row[key] = value
			}
		}
		rows = append(rows, row)
		last = record.Topic + ":" + strconv.FormatInt(record.Offset, 10)
	}
	return rows, last, nil
}

func (d *KafkaDriver) Commit(ctx context.Context, rows []Row) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.client == nil {
		return errors.New("Kafka client is not initialized")
	}
	records := make([]*kgo.Record, 0, len(rows))
	for _, row := range rows {
		record, ok := row["_kafka_record"].(*kgo.Record)
		if !ok || record == nil {
			return errors.New("Kafka row is missing its source record")
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return nil
	}
	return d.client.CommitRecords(ctx, records...)
}

func (d *KafkaDriver) Write(context.Context, map[string]any, []Row) (int, error) {
	return 0, errors.New("Kafka source driver does not support writes")
}

func newKafkaClient(cfg map[string]any) (*kgo.Client, error) {
	brokers := stringSlice(cfg["brokers"])
	if len(brokers) == 0 {
		if raw, ok := cfg["brokers"].(string); ok {
			brokers = strings.Split(raw, ",")
		}
	}
	for i := range brokers {
		brokers[i] = strings.TrimSpace(brokers[i])
	}
	topic, _ := cfg["topic"].(string)
	group, _ := cfg["group"].(string)
	if len(brokers) == 0 || topic == "" || group == "" {
		return nil, errors.New("Kafka requires brokers, topic, and group")
	}
	allow := stringSlice(cfg["endpoint_allowlist"])
	for _, broker := range brokers {
		if err := validateKafkaBroker(broker, allow); err != nil {
			return nil, err
		}
	}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, fmt.Errorf("resolve Kafka broker: %w", err)
		}
		allowed := brokerAllowlisted(address, host, allow)
		for _, ip := range ips {
			if channels.IsPrivateIP(ip) && !allowed {
				return nil, fmt.Errorf("forbidden Kafka private IP: %s", ip)
			}
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	return kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.ConsumerGroup(group), kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()), kgo.DisableAutoCommit(), kgo.Dialer(dial), kgo.FetchMaxBytes(1<<20))
}

func validateKafkaBroker(broker string, allow []string) error {
	host, _, err := net.SplitHostPort(broker)
	if err != nil || host == "" {
		return fmt.Errorf("invalid Kafka broker %q", broker)
	}
	if err := channels.IsSafeURL("http://" + broker); err != nil && !brokerAllowlisted(broker, host, allow) {
		return fmt.Errorf("Kafka broker is not SSRF-safe or allowlisted: %w", err)
	}
	return nil
}

func brokerAllowlisted(address, host string, allow []string) bool {
	for _, entry := range allow {
		entry = strings.TrimSpace(strings.TrimRight(entry, "/"))
		if entry == address || entry == host || strings.TrimPrefix(entry, "http://") == address {
			return true
		}
	}
	return false
}
