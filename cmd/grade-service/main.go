// grade-service (per backlog/phase-2-grade-service.md and backlog/phase-3-kafka-integration.md):
// given a sourcecraft_uuid, read that repo's _meta/*.json exports from S3 (never clones itself,
// per the architecture diagram) and compute the Activity category score. Three modes:
//
//	--repo-id=<uuid>                    one-shot, grade directly, print JSON to stdout
//	--listen                            consume repo.ready from Kafka, grade each as it arrives
//	--request-org=X --request-repo=Y    publish a repo.grade.requested message and exit — the
//	                                     manual trigger point for testing the Kafka flow
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"repo-therapist/internal/contracts"
	kafkaclient "repo-therapist/internal/kafka"
	"repo-therapist/internal/scoring"
	"repo-therapist/internal/sourcecraft"
	"repo-therapist/internal/storage"

	"repo-therapist/internal/gitlog"
)

func main() {
	repoID := flag.String("repo-id", "", "SourceCraft repo UUID (one-shot mode)")
	configPath := flag.String("config", "config/grading.yaml", "path to grading policy config")
	listen := flag.Bool("listen", false, "consume repo.ready from Kafka instead of one-shot mode")
	requestOrg := flag.String("request-org", "", "publish a grade request for this org (with --request-repo) and exit")
	requestRepo := flag.String("request-repo", "", "publish a grade request for this repo (with --request-org) and exit")
	flag.Parse()

	var err error
	switch {
	case *requestOrg != "" && *requestRepo != "":
		err = runRequest(*requestOrg, *requestRepo)
	case *listen:
		err = runListen(*configPath)
	case *repoID != "":
		err = runOnce(*repoID, *configPath)
	default:
		fmt.Fprintln(os.Stderr, "usage: grade-service --repo-id=<uuid>  OR  --listen  OR  --request-org=<org> --request-repo=<repo>")
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("grade-service: %v", err)
	}
}

// runRequest is the manual trigger point: publish repo.grade.requested and exit immediately. The
// grade itself shows up later, asynchronously, in whatever's running --listen (this process or a
// separate one) — that round trip is the thing this phase exists to prove works.
func runRequest(org, repo string) error {
	brokers := []string{envOrDefault("KAFKA_BROKERS", "localhost:9092")}

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

// runListen consumes repo.ready and grades each ready repo — failed ones are logged and skipped,
// not silently dropped without a trace.
func runListen(configPath string) error {
	cfg, err := scoring.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	brokers := []string{envOrDefault("KAFKA_BROKERS", "localhost:9092")}

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
		out, err := grade(gradeCtx, ready.SourcecraftUUID, cfg)
		cancel()
		if err != nil {
			log.Printf("grading %s/%s failed: %v", ready.OrgSlug, ready.RepoSlug, err)
			return nil // one repo's grading failure shouldn't kill the whole consumer loop
		}
		printResult(out)
		return nil
	})
}

func runOnce(repoID, configPath string) error {
	cfg, err := scoring.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	out, err := grade(ctx, repoID, cfg)
	if err != nil {
		return err
	}
	printResult(out)
	return nil
}

func printResult(out map[string]any) {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		log.Printf("marshaling result: %v", err)
		return
	}
	fmt.Println(string(data))
}

// grade does the actual work: pull this repo's S3 exports, compute the Activity score. Shared by
// all three modes — one-shot, listen, and (indirectly) request just publishes and lets whatever's
// listening call this later.
func grade(ctx context.Context, repoID string, cfg *scoring.Config) (map[string]any, error) {
	s3Client, err := storage.NewClient(ctx, storage.Config{
		Endpoint:  envOrDefault("S3_ENDPOINT", "localhost:9000"),
		AccessKey: envOrDefault("S3_ACCESS_KEY", "repotherapist"),
		SecretKey: envOrDefault("S3_SECRET_KEY", "repotherapist123"),
		Bucket:    envOrDefault("S3_BUCKET", "repo-content"),
		UseSSL:    false,
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to S3: %w", err)
	}

	prefix := "repos/" + repoID
	inputs := scoring.ActivityInputs{}

	if data, err := s3Client.GetObject(ctx, prefix+"/_meta/commits.json"); err != nil {
		log.Printf("commits.json unavailable for %s: %v (Activity's commit-sourced metrics will be no-data)", repoID, err)
	} else {
		var commits []gitlog.Commit
		if err := json.Unmarshal(data, &commits); err != nil {
			return nil, fmt.Errorf("decoding commits.json: %w", err)
		}
		inputs.Commits, inputs.CommitsAvailable = commits, true
	}

	if data, err := s3Client.GetObject(ctx, prefix+"/_meta/pulls.json"); err != nil {
		log.Printf("pulls.json unavailable for %s: %v (Activity's MR-rate metric will be no-data)", repoID, err)
	} else {
		var pulls []sourcecraft.PullRequest
		if err := json.Unmarshal(data, &pulls); err != nil {
			return nil, fmt.Errorf("decoding pulls.json: %w", err)
		}
		inputs.Pulls, inputs.PullsAvailable = pulls, true
	}

	if data, err := s3Client.GetObject(ctx, prefix+"/_meta/releases.json"); err != nil {
		log.Printf("releases.json unavailable for %s: %v (Activity's release metrics will be no-data)", repoID, err)
	} else {
		var releases []sourcecraft.Release
		if err := json.Unmarshal(data, &releases); err != nil {
			return nil, fmt.Errorf("decoding releases.json: %w", err)
		}
		inputs.Releases, inputs.ReleasesAvailable = releases, true
	}

	activityCfg, ok := cfg.Categories["activity"]
	if !ok {
		return nil, fmt.Errorf("config has no \"activity\" category")
	}
	result := scoring.ComputeCategory("activity", activityCfg, inputs, time.Now())

	return map[string]any{
		"sourcecraft_uuid": repoID,
		"category":         result,
	}, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
