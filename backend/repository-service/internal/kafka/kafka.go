// Package kafka is a thin wrapper around segmentio/kafka-go — just enough to publish/consume
// JSON messages by topic, nothing more. Consumer groups are used from day one (even though this
// phase runs a single instance of each service) so horizontal scaling later is "run more
// instances," not a protocol change. See backlog/phase-3-kafka-integration.md.
package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	kafkago "github.com/segmentio/kafka-go"
)

// EnsureTopics creates every topic in topics if it doesn't already exist, with numPartitions
// partitions each. Call this before creating any Consumer — a consumer group that joins before
// its topic exists can get stuck with a permanent 0-partition assignment and no automatic
// recovery (a real bug hit and documented in backlog/phase-3-kafka-integration.md); pre-creating
// the topic removes the race entirely instead of relying on auto-create-topics timing.
func EnsureTopics(ctx context.Context, brokers []string, topics []string, numPartitions int) error {
	if len(brokers) == 0 {
		return fmt.Errorf("no brokers given")
	}

	conn, err := kafkago.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		return fmt.Errorf("dialing %s: %w", brokers[0], err)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("finding controller broker: %w", err)
	}
	controllerConn, err := kafkago.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		return fmt.Errorf("dialing controller %s:%d: %w", controller.Host, controller.Port, err)
	}
	defer controllerConn.Close()

	configs := make([]kafkago.TopicConfig, len(topics))
	for i, t := range topics {
		configs[i] = kafkago.TopicConfig{Topic: t, NumPartitions: numPartitions, ReplicationFactor: 1}
	}

	if err := controllerConn.CreateTopics(configs...); err != nil && !errors.Is(err, kafkago.TopicAlreadyExists) {
		return fmt.Errorf("creating topics %v: %w", topics, err)
	}
	return nil
}

type Producer struct {
	writer *kafkago.Writer
}

func NewProducer(brokers []string, topic string) *Producer {
	return &Producer{
		writer: &kafkago.Writer{
			Addr:                   kafkago.TCP(brokers...),
			Topic:                  topic,
			Balancer:               &kafkago.LeastBytes{},
			AllowAutoTopicCreation: true,
		},
	}
}

func (p *Producer) Publish(ctx context.Context, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshaling message for topic %s: %w", p.writer.Topic, err)
	}
	msg := kafkago.Message{Value: data}
	if key != "" {
		msg.Key = []byte(key)
	}
	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("publishing to topic %s: %w", p.writer.Topic, err)
	}
	return nil
}

func (p *Producer) Close() error {
	return p.writer.Close()
}

type Consumer struct {
	reader *kafkago.Reader
}

func NewConsumer(brokers []string, topic, groupID string) *Consumer {
	return &Consumer{
		reader: kafkago.NewReader(kafkago.ReaderConfig{
			Brokers: brokers,
			Topic:   topic,
			GroupID: groupID,
		}),
	}
}

// Run blocks, calling handler for each message in order. Offsets are committed only after handler
// returns nil — a crash mid-handler means that message gets redelivered on restart (at-least-once,
// not exactly-once; handlers should tolerate reprocessing). Returns when ctx is cancelled or
// handler returns a non-nil error, which stops the loop rather than being swallowed.
func (c *Consumer) Run(ctx context.Context, handler func(ctx context.Context, value []byte) error) error {
	for {
		msg, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("fetching message from topic %s: %w", c.reader.Config().Topic, err)
		}

		if err := handler(ctx, msg.Value); err != nil {
			return fmt.Errorf("handling message from topic %s: %w", c.reader.Config().Topic, err)
		}

		if err := c.reader.CommitMessages(ctx, msg); err != nil {
			return fmt.Errorf("committing offset on topic %s: %w", c.reader.Config().Topic, err)
		}
	}
}

func (c *Consumer) Close() error {
	return c.reader.Close()
}
