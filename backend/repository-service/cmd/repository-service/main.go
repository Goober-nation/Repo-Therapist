// repository-service (per backlog/phase-1-repository-service.md and
// backlog/phase-3-kafka-integration.md): given an org/repo slug, fetch the repo's metadata from
// SourceCraft, git clone its default branch, and upload the tree + git history + issues/milestones
// to S3/MinIO. Two trigger modes: one-shot CLI flags (--org/--repo, for direct manual testing) or
// --listen (consumes repo.grade.requested from Kafka, publishes repo.ready when done).
// repo.grade.requested is published by grade-service, relaying grading.requests.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"repo-therapist/repository-service/internal/contracts"
	"repo-therapist/runtimeconfig"
	"repo-therapist/runtimeconfig/kafkasync"

	"repo-therapist/repository-service/internal/service"
	"repo-therapist/repository-service/internal/transport"
)

func main() {
	// Settings (config/app.yaml + env + admin overrides) must load before anything reads them.
	store, err := runtimeconfig.Start(context.Background(), "repository")
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	// Admin overrides (config.updated) are read up to date before the service starts work.
	kafkasync.Run(context.Background(), store, "repository")
	contracts.LoadTopics()
	org := flag.String("org", "", "SourceCraft org slug (one-shot mode)")
	repo := flag.String("repo", "", "SourceCraft repo slug (one-shot mode)")
	listen := flag.Bool("listen", false, "run as a Kafka consumer instead of one-shot mode")
	flag.Parse()

	for _, key := range []string{"S3_ACCESS_KEY", "S3_SECRET_KEY"} {
		if runtimeconfig.Getenv(key) == "" {
			log.Fatalf("repository-service: %s is required", key)
		}
	}

	switch {
	case *listen:
		if err := transport.RunListen(); err != nil {
			log.Fatalf("repository-service: %v", err)
		}
	case *org != "" && *repo != "":
		ctx, cancel := context.WithTimeout(context.Background(), service.CloneTimeout())
		defer cancel()
		result, err := service.CloneAndUpload(ctx, *org, *repo, "", "")
		if err != nil {
			log.Fatalf("repository-service: %v", err)
		}
		log.Printf("done: uploaded %d files + %d commits + %d issues + %d milestones + %d todo markers for %s/%s (sourcecraft_uuid=%s) to key prefix %q",
			result.FileCount, result.CommitCount, result.IssueCount, result.MilestoneCount, result.TodoMarkerCount, *org, *repo, result.SourcecraftUUID, result.S3Prefix)
	default:
		fmt.Fprintln(os.Stderr, "usage: repository-service --org=<org_slug> --repo=<repo_slug>  OR  repository-service --listen")
		os.Exit(2)
	}
}
