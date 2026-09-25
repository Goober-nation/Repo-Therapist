package kafka

import (
	"aggregator-service/internal/domain"
	"context"
	"encoding/json"
	"fmt"

	"github.com/segmentio/kafka-go"
)

type GradingPublisher struct {
	writer *kafka.Writer
}

func NewGradingPublisher(brokers []string, topic string) *GradingPublisher {
	return &GradingPublisher{
		writer: &kafka.Writer{
			Addr:     kafka.TCP(brokers...),
			Topic:    topic,
			Balancer: &kafka.LeastBytes{},
		},
	}
}

func (p *GradingPublisher) Publish(ctx context.Context, event domain.GradingRequestedEvent,
) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal grading event: %w", err)
	}

	err = p.writer.WriteMessages(
		ctx,
		kafka.Message{
			Key:   []byte(event.OrgSlug + "/" + event.RepoSlug),
			Value: payload,
		},
	)
	if err != nil {
		return fmt.Errorf("publish grading event: %w", err)
	}

	return nil
}

func (p *GradingPublisher) Close() error {
	return p.writer.Close()
}

type LLMPublisher struct {
	writer *kafka.Writer
}

func NewLLMPublisher(brokers []string, topic string) *LLMPublisher {
	return &LLMPublisher{
		writer: &kafka.Writer{
			Addr:     kafka.TCP(brokers...),
			Topic:    topic,
			Balancer: &kafka.LeastBytes{},
		},
	}
}

func (p *LLMPublisher) Publish(
	ctx context.Context,
	event domain.LLMRecommendationsRequestedEvent,
) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal llm recommendations event: %w", err)
	}

	err = p.writer.WriteMessages(
		ctx,
		kafka.Message{
			Key:   []byte(event.SourcecraftUUID.String()),
			Value: payload,
		},
	)
	if err != nil {
		return fmt.Errorf("publish llm recommendations event: %w", err)
	}

	return nil
}

func (p *LLMPublisher) Close() error {
	return p.writer.Close()
}
