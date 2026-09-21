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
	"repo-therapist/grade-service/internal/gitlog"
	"repo-therapist/grade-service/internal/scoring"
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

	// Issues/milestones are read back from the sourcecraft-wrapper-shaped JSON that
	// repository-service wrote to S3 via its scwrapper client.
	if data, err := s3Client.GetObject(ctx, prefix+"/_meta/issues.json"); err != nil {
		log.Printf("issues.json unavailable for %s: %v (Activity's issue-rate metric will be no-data)", repoID, err)
	} else {
		var issues []scoring.Issue
		if err := json.Unmarshal(data, &issues); err != nil {
			return nil, fmt.Errorf("decoding issues.json: %w", err)
		}
		inputs.Issues, inputs.IssuesAvailable = issues, true
	}

	if data, err := s3Client.GetObject(ctx, prefix+"/_meta/milestones.json"); err != nil {
		log.Printf("milestones.json unavailable for %s: %v (Activity's milestone metrics will be no-data)", repoID, err)
	} else {
		var milestones []scoring.Milestone
		if err := json.Unmarshal(data, &milestones); err != nil {
			return nil, fmt.Errorf("decoding milestones.json: %w", err)
		}
		inputs.Milestones, inputs.MilestonesAvailable = milestones, true
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
