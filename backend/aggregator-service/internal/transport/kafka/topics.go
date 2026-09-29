package kafka

import (
	"context"
	"errors"
	"fmt"

	"github.com/segmentio/kafka-go"
)

// EnsureTopics creates every topic in topics that doesn't exist yet. kafka-go writers do not
// auto-create topics, and a consumer waiting on a missing topic never triggers its creation, so
// a topic only this service publishes to (llm.recommendations.requested) would otherwise never
// appear and every request on it would fail.
func EnsureTopics(ctx context.Context, brokers []string, topics []string) error {
	if len(brokers) == 0 {
		return fmt.Errorf("no brokers given")
	}

	conn, err := kafka.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		return fmt.Errorf("dial %s: %w", brokers[0], err)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("find controller broker: %w", err)
	}

	controllerConn, err := kafka.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		return fmt.Errorf("dial controller %s:%d: %w", controller.Host, controller.Port, err)
	}
	defer controllerConn.Close()

	configs := make([]kafka.TopicConfig, len(topics))
	for i, topic := range topics {
		configs[i] = kafka.TopicConfig{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}
	}

	if err := controllerConn.CreateTopics(configs...); err != nil && !errors.Is(err, kafka.TopicAlreadyExists) {
		return fmt.Errorf("create topics %v: %w", topics, err)
	}

	return nil
}
