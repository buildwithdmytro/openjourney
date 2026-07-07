package stream

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/twmb/franz-go/pkg/kgo"
)

type Publisher struct {
	client *kgo.Client
}

func NewPublisher(brokers string) (*Publisher, error) {
	if strings.TrimSpace(brokers) == "" {
		return nil, errors.New("Kafka brokers are required")
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(strings.Split(brokers, ",")...),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerBatchMaxBytes(1<<20),
		kgo.RecordRetries(10),
		kgo.RecordDeliveryTimeout(30*time.Second),
	)
	if err != nil {
		return nil, err
	}
	return &Publisher{client: client}, nil
}

func (p *Publisher) Publish(ctx context.Context, event domain.OutboxEvent) error {
	record := &kgo.Record{
		Topic: event.Topic, Key: []byte(event.PartitionKey), Value: event.Payload,
		Headers: []kgo.RecordHeader{
			{Key: "event_id", Value: []byte(event.EventID)},
			{Key: "tenant_id", Value: []byte(event.TenantID)},
		},
	}
	return p.client.ProduceSync(ctx, record).FirstErr()
}

func (p *Publisher) Close() { p.client.Close() }

type Consumer struct {
	client *kgo.Client
}

func NewConsumer(brokers, group string, topics ...string) (*Consumer, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(strings.Split(brokers, ",")...),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topics...),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return nil, err
	}
	return &Consumer{client: client}, nil
}

func (c *Consumer) Poll(ctx context.Context) (*kgo.Record, error) {
	for {
		fetches := c.client.PollFetches(ctx)
		if err := fetches.Err(); err != nil {
			return nil, err
		}
		records := fetches.Records()
		if len(records) > 0 {
			return records[0], nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
}

func (c *Consumer) Commit(ctx context.Context, record *kgo.Record) error {
	return c.client.CommitRecords(ctx, record)
}

func (c *Consumer) Close() { c.client.Close() }
