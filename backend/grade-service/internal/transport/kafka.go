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
	"repo-therapist/shared/contracts"
	kafkaclient "repo-therapist/shared/kafka"
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

// RunListen consumes repo.ready and grades each ready repo — failed ones are logged and skipped,
// not silently dropped without a trace.
func RunListen(configPath string) error {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
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

	consumer := kafkaclient.NewConsumer(brokers, contracts.TopicRepoReady, "grade-service")
	defer consumer.Close()

	log.Printf("grade-service listening on %s", contracts.TopicRepoReady)
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
			return nil // one repo's grading failure shouldn't kill the whole consumer loop
		}
		app.PrintResult(out)
		return nil
	})
}
