// grading-request publishes a grading.requests message the same way the Aggregator does, then
// prints the grading.results message for that repo. For manual testing of the full Kafka flow.
//
// grading.results carries only the repo's UUID, so the tool first follows repo.ready (which has
// org/repo and the UUID) and then waits for the result with that UUID; messages for other
// requests are skipped. A failed clone (repo.ready status=failed) or a result with an error
// exits non-zero right away instead of waiting out --wait.
//
//	grading-request --org=<org> --repo=<repo> [--visibility=public|private] [--wait=10m]
//
// --visibility=private stores SOURCECRAFT_PAT from the environment in Redis under a new job id
// (REDIS_ADDRESS/REDIS_PASSWORD, as the Aggregator does); the Kafka message carries only the id.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	kafkago "github.com/segmentio/kafka-go"

	"repo-therapist/grade-service/internal/app"
	"repo-therapist/grade-service/internal/contracts"
	kafkaclient "repo-therapist/grade-service/internal/kafka"
	"repo-therapist/grade-service/internal/tokenstore"
)

func main() {
	org := flag.String("org", "", "org slug")
	repo := flag.String("repo", "", "repo slug")
	visibility := flag.String("visibility", "public", "public (no token) or private (SOURCECRAFT_PAT stored in Redis as the user token)")
	// 10m covers the default clone limit (CLONE_TIMEOUT); failures exit early, so it only bounds
	// a stuck pipeline. The batch script passes --wait=15m.
	wait := flag.Duration("wait", 10*time.Minute, "how long to wait for this repo's grading.results message; 0 = publish only")
	flag.Parse()

	if *org == "" || *repo == "" || (*visibility != "public" && *visibility != "private") {
		fmt.Fprintln(os.Stderr, "usage: grading-request --org=<org> --repo=<repo> [--visibility=public|private] [--wait=10m]")
		os.Exit(2)
	}
	var token string
	if *visibility == "private" {
		token = os.Getenv("SOURCECRAFT_PAT")
		if token == "" {
			log.Fatal("grading-request: --visibility=private needs SOURCECRAFT_PAT set")
		}
	}
	if err := run(*org, *repo, *visibility, token, *wait); err != nil {
		log.Fatalf("grading-request: %v", err)
	}
}

func run(org, repo, visibility, token string, wait time.Duration) error {
	brokers := []string{app.EnvOrDefault("KAFKA_BROKERS", "localhost:9092")}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second+wait)
	defer cancel()

	if err := kafkaclient.EnsureTopics(ctx, brokers, contracts.AllTopics, 1); err != nil {
		return fmt.Errorf("provisioning topics: %w", err)
	}

	// Both readers are positioned at the end before publishing, so only messages produced after
	// this request are read.
	var readyReader, resultReader *kafkago.Reader
	if wait > 0 {
		readyReader = kafkago.NewReader(kafkago.ReaderConfig{Brokers: brokers, Topic: contracts.TopicRepoReady})
		defer readyReader.Close()
		resultReader = kafkago.NewReader(kafkago.ReaderConfig{Brokers: brokers, Topic: contracts.TopicGradingResults})
		defer resultReader.Close()
		for _, r := range []*kafkago.Reader{readyReader, resultReader} {
			if err := r.SetOffset(kafkago.LastOffset); err != nil {
				return fmt.Errorf("seeking %s: %w", r.Config().Topic, err)
			}
		}
	}

	producer := kafkaclient.NewProducer(brokers, contracts.TopicGradingRequests)
	defer producer.Close()

	jobID := uuid.NewString()
	if token != "" {
		tokens := tokenstore.NewFromEnv()
		defer tokens.Close()
		if err := tokens.PutToken(ctx, jobID, token, tokenstore.TTL); err != nil {
			return err
		}
	}

	req := contracts.GradingRequestedEvent{JobID: jobID, OrgSlug: org, RepoSlug: repo, Visibility: visibility, RequestedAt: time.Now()}
	if err := producer.Publish(ctx, org+"/"+repo, req); err != nil {
		return fmt.Errorf("publishing %s: %w", contracts.TopicGradingRequests, err)
	}
	log.Printf("published %s grading request for %s/%s to %s (job %s, token stored: %t)", visibility, org, repo, contracts.TopicGradingRequests, jobID, token != "")

	if wait == 0 {
		return nil
	}

	log.Printf("waiting up to %s for %s/%s's result", wait, org, repo)
	readCtx, readCancel := context.WithTimeout(ctx, wait)
	defer readCancel()

	// Phase 1: repo.ready for this org/repo gives the UUID, or tells us the clone failed.
	var ready contracts.RepoReady
	skipped := 0
	for {
		msg, err := readyReader.ReadMessage(readCtx)
		if err != nil {
			return fmt.Errorf("waiting for %s for %s/%s (skipped %d other messages): %w", contracts.TopicRepoReady, org, repo, skipped, err)
		}
		r, ok := matchRepoReady(msg.Value, org, repo)
		if !ok {
			skipped++
			continue
		}
		ready = r
		break
	}
	if ready.Status != contracts.StatusReady {
		return fmt.Errorf("repository-service could not fetch %s/%s: status=%s error=%q", org, repo, ready.Status, ready.Error)
	}
	log.Printf("%s/%s cloned (sourcecraft_uuid=%s), waiting for its grading result", org, repo, ready.SourcecraftUUID)

	// Phase 2: the grading result with that UUID and visibility.
	skipped = 0
	for {
		msg, err := resultReader.ReadMessage(readCtx)
		if err != nil {
			return fmt.Errorf("waiting for %s for %s (skipped %d other messages): %w", contracts.TopicGradingResults, ready.SourcecraftUUID, skipped, err)
		}
		result, ok := matchResult(msg.Value, ready.SourcecraftUUID, visibility)
		if !ok {
			skipped++
			continue
		}
		if skipped > 0 {
			log.Printf("skipped %d results for other requests", skipped)
		}
		var pretty json.RawMessage = msg.Value
		out, err := json.MarshalIndent(pretty, "", "  ")
		if err != nil {
			return fmt.Errorf("formatting result: %w", err)
		}
		fmt.Println(string(out))
		if result.Error != "" {
			return fmt.Errorf("grading %s/%s failed: %s", org, repo, result.Error)
		}
		return nil
	}
}
