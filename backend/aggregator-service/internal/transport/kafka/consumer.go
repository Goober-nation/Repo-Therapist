package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"aggregator-service/internal/domain"

	"github.com/segmentio/kafka-go"
)

type GradeConsumer struct {
	reader  *kafka.Reader
	service GradeSaver
}

type GradeSaver interface {
	SaveGradingResult(ctx context.Context, grade domain.RepoGrade, visibility string) error
}

func NewGradeConsumer(
	brokers []string,
	topic string,
	groupID string,
	service GradeSaver,
) *GradeConsumer {
	return &GradeConsumer{
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers: brokers,
			Topic:   topic,
			GroupID: groupID,
		}),
		service: service,
	}
}

func (c *GradeConsumer) Start(ctx context.Context) {
	go func() {
		for {
			msg, err := c.reader.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}

				log.Printf("grade consumer read error: %v", err)
				continue
			}

			if err := c.handleMessage(ctx, msg.Value); err != nil {
				log.Printf("grade consumer handle error: %v", err)
				continue
			}
		}
	}()
}

func (c *GradeConsumer) handleMessage(
	ctx context.Context,
	payload []byte,
) error {
	var event domain.GradingResultReceivedEvent

	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("unmarshal grading result event: %w", err)
	}

	grade := domain.RepoGrade{
		SourcecraftUUID: event.SourcecraftUUID,
		Score:           event.Score,
		CategoryScores:  event.CategoryScores,
		Advice:          event.Advice,

		MissingData: event.MissingData,
		GradedAt:    &event.GradedAt,
	}

	if err := c.service.SaveGradingResult(ctx, grade, event.Visibility); err != nil {
		return fmt.Errorf("save grading result: %w", err)
	}

	return nil
}

func (c *GradeConsumer) Close() error {
	return c.reader.Close()
}

type LLMConsumer struct {
	reader  *kafka.Reader
	service LLMRecommendationSaver
}

type LLMRecommendationSaver interface {
	SaveLLMRecommendations(
		ctx context.Context,
		event domain.LLMRecommendationsReceivedEvent,
	) error
}

func NewLLMConsumer(
	brokers []string,
	topic string,
	groupID string,
	service LLMRecommendationSaver,
) *LLMConsumer {
	return &LLMConsumer{
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers: brokers,
			Topic:   topic,
			GroupID: groupID,
		}),
		service: service,
	}
}

func (c *LLMConsumer) Start(ctx context.Context) {
	go func() {
		for {
			msg, err := c.reader.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}

				log.Printf("llm consumer read error: %v", err)
				continue
			}

			if err := c.handleMessage(ctx, msg.Value); err != nil {
				log.Printf("llm consumer handle error: %v", err)
				continue
			}
		}
	}()
}

func (c *LLMConsumer) handleMessage(
	ctx context.Context,
	payload []byte,
) error {
	var event domain.LLMRecommendationsReceivedEvent

	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("unmarshal llm recommendations event: %w", err)
	}

	if event.Visibility != "public" &&
		event.Visibility != "private" &&
		event.Visibility != "internal" {
		return fmt.Errorf(
			"invalid llm visibility %q",
			event.Visibility,
		)
	}

	if err := c.service.SaveLLMRecommendations(ctx, event); err != nil {
		return fmt.Errorf("save llm recommendations: %w", err)
	}

	return nil
}

func (c *LLMConsumer) Close() error {
	return c.reader.Close()
}
