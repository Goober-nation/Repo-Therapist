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

	"repo-therapist/repository-service/internal/contracts"
	kafkaclient "repo-therapist/repository-service/internal/kafka"
	"repo-therapist/repository-service/internal/service"
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
		// A clone cut short by shutdown still reports its failure downstream.
		publishCtx, cancel := detachedOnShutdown(ctx)
		defer cancel()
		if err := producer.Publish(publishCtx, req.OrgSlug+"/"+req.RepoSlug, ready); err != nil {
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
	cloneCtx, cancel := context.WithTimeout(ctx, service.CloneTimeout())
	defer cancel()

	res, err := cloneAndUpload(cloneCtx, req.OrgSlug, req.RepoSlug, req.Token)
	if err != nil {
		log.Printf("clone/upload failed for %s/%s: %v", req.OrgSlug, req.RepoSlug, err)
		failed := contracts.RepoReady{
			OrgSlug:  req.OrgSlug,
			RepoSlug: req.RepoSlug,
			Status:   contracts.StatusFailed,
			Error:    err.Error(),
		}
		// Set once the repo id was resolved; Grade needs it to report the failure to the Aggregator.
		if res != nil {
			failed.SourcecraftUUID = res.SourcecraftUUID
		}
		return failed
	}
	return contracts.RepoReady{
		OrgSlug:         req.OrgSlug,
		RepoSlug:        req.RepoSlug,
		SourcecraftUUID: res.SourcecraftUUID,
		S3Prefix:        res.S3Prefix,
		Status:          contracts.StatusReady,
	}
}

// cloneAndUpload is service.CloneAndUpload, replaceable in tests.
var cloneAndUpload = service.CloneAndUpload

// shutdownPublishTimeout bounds the last publish made after shutdown was requested.
const shutdownPublishTimeout = 5 * time.Second

// detachedOnShutdown returns ctx unchanged while it is live. Once it is cancelled (SIGTERM), it
// returns a short context that ignores the cancellation, so a final result can still be sent.
func detachedOnShutdown(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Err() == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.WithoutCancel(ctx), shutdownPublishTimeout)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
