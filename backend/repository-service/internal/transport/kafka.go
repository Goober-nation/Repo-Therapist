// Package transport wires repository-service's Kafka topics: consumes repo.grade.requested,
// publishes repo.ready for each one, around the core service.CloneAndUpload work.
package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"repo-therapist/repository-service/internal/service"
	"repo-therapist/shared/contracts"
	kafkaclient "repo-therapist/shared/kafka"
)

// RunListen consumes repo.grade.requested and publishes repo.ready for each one — a thin loop
// around service.CloneAndUpload, which is exactly what the one-shot CLI path already calls and
// already has verified working end to end.
func RunListen() error {
	brokers := []string{envOrDefault("KAFKA_BROKERS", "localhost:9092")}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	provisionCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err := kafkaclient.EnsureTopics(provisionCtx, brokers, contracts.AllTopics, 1)
	cancel()
	if err != nil {
		return fmt.Errorf("provisioning topics: %w", err)
	}

	consumer := kafkaclient.NewConsumer(brokers, contracts.TopicGradeRequested, "repository-service")
	defer consumer.Close()
	producer := kafkaclient.NewProducer(brokers, contracts.TopicRepoReady)
	defer producer.Close()

	log.Printf("repository-service listening on %s, publishing to %s", contracts.TopicGradeRequested, contracts.TopicRepoReady)
	return consumer.Run(ctx, func(ctx context.Context, value []byte) error {
		var req contracts.GradeRequested
		if err := json.Unmarshal(value, &req); err != nil {
			log.Printf("skipping unparseable %s message: %v", contracts.TopicGradeRequested, err)
			return nil // don't kill the whole consumer loop over one bad message
		}

		log.Printf("received grade request for %s/%s", req.OrgSlug, req.RepoSlug)
		ready := handleRequest(ctx, req)
		if err := producer.Publish(ctx, req.OrgSlug+"/"+req.RepoSlug, ready); err != nil {
			return fmt.Errorf("publishing %s: %w", contracts.TopicRepoReady, err)
		}
		log.Printf("published %s for %s/%s: status=%s", contracts.TopicRepoReady, req.OrgSlug, req.RepoSlug, ready.Status)
		return nil
	})
}

// handleRequest runs service.CloneAndUpload and turns either outcome into a RepoReady message —
// clone failures are reported via status="failed", not swallowed or left for the consumer to
// guess at.
func handleRequest(ctx context.Context, req contracts.GradeRequested) contracts.RepoReady {
	cloneCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	res, err := service.CloneAndUpload(cloneCtx, req.OrgSlug, req.RepoSlug)
	if err != nil {
		log.Printf("clone/upload failed for %s/%s: %v", req.OrgSlug, req.RepoSlug, err)
		return contracts.RepoReady{
			OrgSlug:  req.OrgSlug,
			RepoSlug: req.RepoSlug,
			Status:   contracts.StatusFailed,
			Error:    err.Error(),
			ReadyAt:  time.Now(),
		}
	}
	return contracts.RepoReady{
		OrgSlug:         req.OrgSlug,
		RepoSlug:        req.RepoSlug,
		SourcecraftUUID: res.SourcecraftUUID,
		S3Prefix:        res.S3Prefix,
		Status:          contracts.StatusReady,
		CommitCount:     res.CommitCount,
		PullCount:       res.PullCount,
		ReleaseCount:    res.ReleaseCount,
		ReadyAt:         time.Now(),
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
