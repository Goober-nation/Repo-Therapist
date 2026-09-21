// Package app holds grade-service's core orchestration: given a sourcecraft_uuid, pull that
// repo's S3 _meta/*.json exports and compute its Activity category score. Used by every mode
// (one-shot, --listen) in cmd/grade-service.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/scoring"
	"repo-therapist/grade-service/internal/gitlog"
	"repo-therapist/tmp-sourcecraft"
	"repo-therapist/grade-service/internal/storage"
)

// Grade does the actual work: pull this repo's S3 exports, compute the Activity score.
func Grade(ctx context.Context, repoID string, cfg *config.Config) (map[string]any, error) {
	s3Client, err := storage.NewClient(ctx, storage.Config{
		Endpoint:  EnvOrDefault("S3_ENDPOINT", "localhost:9000"),
		AccessKey: EnvOrDefault("S3_ACCESS_KEY", "repotherapist"),
		SecretKey: EnvOrDefault("S3_SECRET_KEY", "repotherapist123"),
		Bucket:    EnvOrDefault("S3_BUCKET", "repo-content"),
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

	// Pulls/releases are read back from the SourceCraft-shaped JSON that repository-service wrote
	// to S3. repository-service currently fetches them via direct SourceCraft API calls — a
	// placeholder for the eventual sourcecraft-wrapper-service — but the shape on disk (and thus
	// here) won't change when that swap happens.
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

func PrintResult(out map[string]any) {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		log.Printf("marshaling result: %v", err)
		return
	}
	fmt.Println(string(data))
}

func EnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
