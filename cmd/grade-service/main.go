// grade-service — first working slice (per backlog/phase-2-grade-service.md): given a
// sourcecraft_uuid, read that repo's _meta/*.json exports from S3 (never clones itself, per the
// architecture diagram) and compute the Activity category score. Manually triggered via CLI flag
// — no Kafka/Aggregator integration yet.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"repo-therapist/internal/scoring"
	"repo-therapist/internal/sourcecraft"
	"repo-therapist/internal/storage"

	"repo-therapist/internal/gitlog"
)

func main() {
	repoID := flag.String("repo-id", "", "SourceCraft repo UUID (required)")
	configPath := flag.String("config", "config/grading.yaml", "path to grading policy config")
	flag.Parse()

	if *repoID == "" {
		fmt.Fprintln(os.Stderr, "usage: grade-service --repo-id=<sourcecraft_uuid> [--config=config/grading.yaml]")
		os.Exit(2)
	}

	if err := run(*repoID, *configPath); err != nil {
		log.Fatalf("grade-service: %v", err)
	}
}

func run(repoID, configPath string) error {
	cfg, err := scoring.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s3Client, err := storage.NewClient(ctx, storage.Config{
		Endpoint:  envOrDefault("S3_ENDPOINT", "localhost:9000"),
		AccessKey: envOrDefault("S3_ACCESS_KEY", "repotherapist"),
		SecretKey: envOrDefault("S3_SECRET_KEY", "repotherapist123"),
		Bucket:    envOrDefault("S3_BUCKET", "repo-content"),
		UseSSL:    false,
	})
	if err != nil {
		return fmt.Errorf("connecting to S3: %w", err)
	}

	prefix := "repos/" + repoID
	inputs := scoring.ActivityInputs{}

	if data, err := s3Client.GetObject(ctx, prefix+"/_meta/commits.json"); err != nil {
		log.Printf("commits.json unavailable for %s: %v (Activity's commit-sourced metrics will be no-data)", repoID, err)
	} else {
		var commits []gitlog.Commit
		if err := json.Unmarshal(data, &commits); err != nil {
			return fmt.Errorf("decoding commits.json: %w", err)
		}
		inputs.Commits, inputs.CommitsAvailable = commits, true
	}

	if data, err := s3Client.GetObject(ctx, prefix+"/_meta/pulls.json"); err != nil {
		log.Printf("pulls.json unavailable for %s: %v (Activity's MR-rate metric will be no-data)", repoID, err)
	} else {
		var pulls []sourcecraft.PullRequest
		if err := json.Unmarshal(data, &pulls); err != nil {
			return fmt.Errorf("decoding pulls.json: %w", err)
		}
		inputs.Pulls, inputs.PullsAvailable = pulls, true
	}

	if data, err := s3Client.GetObject(ctx, prefix+"/_meta/releases.json"); err != nil {
		log.Printf("releases.json unavailable for %s: %v (Activity's release metrics will be no-data)", repoID, err)
	} else {
		var releases []sourcecraft.Release
		if err := json.Unmarshal(data, &releases); err != nil {
			return fmt.Errorf("decoding releases.json: %w", err)
		}
		inputs.Releases, inputs.ReleasesAvailable = releases, true
	}

	activityCfg, ok := cfg.Categories["activity"]
	if !ok {
		return fmt.Errorf("config has no \"activity\" category")
	}
	result := scoring.ComputeCategory("activity", activityCfg, inputs, time.Now())

	out, err := json.MarshalIndent(map[string]any{
		"sourcecraft_uuid": repoID,
		"category":         result,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling result: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
