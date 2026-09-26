// grading-request publishes a grading.requests message the same way the Aggregator does, then
// prints the next grading.results message. For manual testing of the full Kafka flow.
//
//	grading-request --org=<org> --repo=<repo> [--visibility=public|private] [--wait=2m]
//
// --visibility=private sends SOURCECRAFT_PAT from the environment as the user token.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"repo-therapist/grade-service/internal/app"
	"repo-therapist/grade-service/internal/contracts"
	kafkaclient "repo-therapist/grade-service/internal/kafka"
)

func main() {
	org := flag.String("org", "", "org slug")
	repo := flag.String("repo", "", "repo slug")
	visibility := flag.String("visibility", "public", "public (no token) or private (SOURCECRAFT_PAT as token)")
	wait := flag.Duration("wait", 2*time.Minute, "how long to wait for a grading.results message; 0 = publish only")
	flag.Parse()

	if *org == "" || *repo == "" || (*visibility != "public" && *visibility != "private") {
		fmt.Fprintln(os.Stderr, "usage: grading-request --org=<org> --repo=<repo> [--visibility=public|private] [--wait=2m]")
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

	// Positioned at the end before publishing, so only results produced after this request are read.
	var reader *kafkago.Reader
	if wait > 0 {
		reader = kafkago.NewReader(kafkago.ReaderConfig{Brokers: brokers, Topic: contracts.TopicGradingResults})
		defer reader.Close()
		if err := reader.SetOffset(kafkago.LastOffset); err != nil {
			return fmt.Errorf("seeking %s: %w", contracts.TopicGradingResults, err)
		}
	}

	producer := kafkaclient.NewProducer(brokers, contracts.TopicGradingRequests)
	defer producer.Close()

	req := contracts.GradingRequestedEvent{OrgSlug: org, RepoSlug: repo, Visibility: visibility, Token: token, RequestedAt: time.Now()}
	if err := producer.Publish(ctx, org+"/"+repo, req); err != nil {
		return fmt.Errorf("publishing %s: %w", contracts.TopicGradingRequests, err)
	}
	log.Printf("published %s grading request for %s/%s to %s (token: %t)", visibility, org, repo, contracts.TopicGradingRequests, token != "")

	if reader == nil {
		return nil
	}

	log.Printf("waiting up to %s for %s (failed jobs produce no result)", wait, contracts.TopicGradingResults)
	readCtx, readCancel := context.WithTimeout(ctx, wait)
	defer readCancel()
	msg, err := reader.ReadMessage(readCtx)
	if err != nil {
		return fmt.Errorf("reading %s: %w", contracts.TopicGradingResults, err)
	}

	var pretty json.RawMessage = msg.Value
	out, err := json.MarshalIndent(pretty, "", "  ")
	if err != nil {
		return fmt.Errorf("formatting result: %w", err)
	}
	fmt.Println(string(out))
	return nil
}
