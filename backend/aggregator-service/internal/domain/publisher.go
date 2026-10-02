package domain

import (
	"context"
	"time"
)

type GradingPublisher interface {
	Publish(ctx context.Context, event GradingRequestedEvent) error
}

type LLMRecommendationsPublisher interface {
	Publish(ctx context.Context, event LLMRecommendationsRequestedEvent) error
}

// GradingTokenStore keeps a private grading's user PAT out of Kafka: the Aggregator stores it
// under the job's id, Grade and Repository services read it from there.
type GradingTokenStore interface {
	PutToken(ctx context.Context, jobID, token string, ttl time.Duration) error
	DeleteToken(ctx context.Context, jobID string) error
}
