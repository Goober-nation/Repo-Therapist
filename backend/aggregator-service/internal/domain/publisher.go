package domain

import (
	"context"
)

type GradingPublisher interface {
	Publish(ctx context.Context, event GradingRequestedEvent) error
}

type LLMRecommendationsPublisher interface {
	Publish(ctx context.Context, event LLMRecommendationsRequestedEvent) error
}
