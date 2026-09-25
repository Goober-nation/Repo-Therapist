// Package transport wires grade-service's Kafka topics (repo.ready in, repo.grade.requested out
// for manual triggering) to the app package's grading logic.
package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	"os"

	"repo-therapist/grade-service/internal/app"
	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/contracts"
	kafkaclient "repo-therapist/grade-service/internal/kafka"
)

// RunRequest is the manual trigger point: publish repo.grade.requested and exit immediately. The
// grade itself shows up later, asynchronously, in whatever's running RunListen (this process or a
// separate one) — that round trip is the thing this phase exists to prove works.
func RunRequest(org, repo string) error {
	brokers := []string{app.EnvOrDefault("KAFKA_BROKERS", "localhost:9092")}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := kafkaclient.EnsureTopics(ctx, brokers, contracts.AllTopics, 1); err != nil {
		return fmt.Errorf("provisioning topics: %w", err)
	}

	producer := kafkaclient.NewProducer(brokers, contracts.TopicGradeRequested)
	defer producer.Close()

	req := contracts.GradeRequested{OrgSlug: org, RepoSlug: repo, RequestedAt: time.Now()}
	if err := producer.Publish(ctx, org+"/"+repo, req); err != nil {
		return fmt.Errorf("publishing %s: %w", contracts.TopicGradeRequested, err)
	}
	log.Printf("published grade request for %s/%s to %s", org, repo, contracts.TopicGradeRequested)
	return nil
}

// RunListen runs two consumers concurrently for as long as the process lives:
//   - grading.requests (the Aggregator's trigger) -> relayed as repo.grade.requested, our own
//     internal trigger to Repository service. Byte-identical shape, straight passthrough.
//   - repo.ready (Repository service's handoff) -> graded, and the result published to
//     grading.results for the Aggregator to consume.
//
// Repository service is untouched by any of this — it only ever sees repo.grade.requested/
// repo.ready, exactly as before. See agents_memory/topics/aggregator-integration.md.
func RunListen(configPath string) error {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	catalog, err := config.LoadRecommendations(app.EnvOrDefault("RECOMMENDATIONS_PATH", "config/recommendations.yaml"))
	if err != nil {
		return fmt.Errorf("loading recommendations catalog: %w", err)
	}

	brokers := []string{app.EnvOrDefault("KAFKA_BROKERS", "localhost:9092")}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	provisionCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = kafkaclient.EnsureTopics(provisionCtx, brokers, contracts.AllTopics, 1)
	cancel()
	if err != nil {
		return fmt.Errorf("provisioning topics: %w", err)
	}

	// Two independent consumer loops, one goroutine each. If either returns (error or shutdown),
	// stop() cancels the shared ctx so the other one exits too — Consumer.Run treats a cancelled
	// ctx as a clean stop (returns nil), not an error, so this doesn't mask a real first error.
	errCh := make(chan error, 2)
	go func() { errCh <- runGradingRequestsRelay(ctx, brokers) }()
	go func() { errCh <- runRepoReadyGrader(ctx, brokers, cfg, catalog) }()

	firstErr := <-errCh
	stop()
	<-errCh
	return firstErr
}

// runGradingRequestsRelay consumes the Aggregator's grading.requests and republishes each one as
// our own internal repo.grade.requested — GradingRequestedEvent and GradeRequested are
// byte-identical in shape by design, so this is a pure passthrough, no field mapping.
func runGradingRequestsRelay(ctx context.Context, brokers []string) error {
	consumer := kafkaclient.NewConsumer(brokers, contracts.TopicGradingRequests, "grade-service")
	defer consumer.Close()
	producer := kafkaclient.NewProducer(brokers, contracts.TopicGradeRequested)
	defer producer.Close()

	log.Printf("grade-service listening on %s, relaying to %s", contracts.TopicGradingRequests, contracts.TopicGradeRequested)
	return consumer.Run(ctx, func(ctx context.Context, value []byte) error {
		var req contracts.GradingRequestedEvent
		if err := json.Unmarshal(value, &req); err != nil {
			log.Printf("skipping unparseable %s message: %v", contracts.TopicGradingRequests, err)
			return nil
		}

		out := contracts.GradeRequested{OrgSlug: req.OrgSlug, RepoSlug: req.RepoSlug, RequestedAt: req.RequestedAt}
		if err := producer.Publish(ctx, req.OrgSlug+"/"+req.RepoSlug, out); err != nil {
			return fmt.Errorf("relaying to %s: %w", contracts.TopicGradeRequested, err)
		}
		log.Printf("relayed grading request for %s/%s to %s", req.OrgSlug, req.RepoSlug, contracts.TopicGradeRequested)
		return nil
	})
}

// runRepoReadyGrader consumes repo.ready, grades each ready repo, and publishes the result to
// grading.results for the Aggregator. Failed/skipped repos are logged, not silently dropped, but
// don't kill the loop — one repo's failure shouldn't stop grading for every other repo.
func runRepoReadyGrader(ctx context.Context, brokers []string, cfg *config.Config, catalog map[string]string) error {
	consumer := kafkaclient.NewConsumer(brokers, contracts.TopicRepoReady, "grade-service")
	defer consumer.Close()
	producer := kafkaclient.NewProducer(brokers, contracts.TopicGradingResults)
	defer producer.Close()

	log.Printf("grade-service listening on %s, publishing results to %s", contracts.TopicRepoReady, contracts.TopicGradingResults)
	return consumer.Run(ctx, func(ctx context.Context, value []byte) error {
		var ready contracts.RepoReady
		if err := json.Unmarshal(value, &ready); err != nil {
			log.Printf("skipping unparseable %s message: %v", contracts.TopicRepoReady, err)
			return nil
		}

		if ready.Status != contracts.StatusReady {
			log.Printf("skipping %s/%s: status=%s error=%q", ready.OrgSlug, ready.RepoSlug, ready.Status, ready.Error)
			return nil
		}

		log.Printf("grading %s/%s (sourcecraft_uuid=%s)", ready.OrgSlug, ready.RepoSlug, ready.SourcecraftUUID)
		gradeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		out, err := app.Grade(gradeCtx, ready.SourcecraftUUID, cfg)
		cancel()
		if err != nil {
			log.Printf("grading %s/%s failed: %v", ready.OrgSlug, ready.RepoSlug, err)
			return nil
		}
		app.PrintResult(out)

		result, err := app.BuildGradingResult(out, cfg, catalog, time.Now())
		if err != nil {
			log.Printf("building grading result for %s/%s failed: %v", ready.OrgSlug, ready.RepoSlug, err)
			return nil
		}
		if err := producer.Publish(ctx, ready.SourcecraftUUID, result); err != nil {
			return fmt.Errorf("publishing %s: %w", contracts.TopicGradingResults, err)
		}
		log.Printf("published grading result for %s/%s to %s", ready.OrgSlug, ready.RepoSlug, contracts.TopicGradingResults)
		return nil
	})
}
