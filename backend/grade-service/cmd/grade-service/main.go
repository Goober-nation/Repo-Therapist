// grade-service (per backlog/phase-2-grade-service.md and backlog/phase-3-kafka-integration.md):
// given a sourcecraft_uuid, read that repo's _meta/*.json exports from S3 (never clones itself,
// per the architecture diagram) and compute the category scores. Two modes:
//
//	--repo-id=<uuid>    one-shot, grade directly, print JSON to stdout
//	--listen            consume grading.requests + repo.ready from Kafka
//
// Manual Kafka triggering lives in cmd/grading-request.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"repo-therapist/grade-service/internal/app"
	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/contracts"
	"repo-therapist/grade-service/internal/transport"
)

func main() {
	repoID := flag.String("repo-id", "", "SourceCraft repo UUID (one-shot mode)")
	configPath := flag.String("config", "config/grading.yaml", "path to grading policy config")
	listen := flag.Bool("listen", false, "consume repo.ready from Kafka instead of one-shot mode")
	flag.Parse()

	for _, key := range []string{"S3_ACCESS_KEY", "S3_SECRET_KEY"} {
		if os.Getenv(key) == "" {
			log.Fatalf("grade-service: %s is required", key)
		}
	}

	var err error
	switch {
	case *listen:
		err = transport.RunListen(*configPath)
	case *repoID != "":
		err = runOnce(*repoID, *configPath)
	default:
		fmt.Fprintln(os.Stderr, "usage: grade-service --repo-id=<uuid>  OR  --listen")
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("grade-service: %v", err)
	}
}

func runOnce(repoID, configPath string) error {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), app.GradeTimeout())
	defer cancel()

	// One-shot debugging prints every category, so it grades the full private view.
	out, err := app.Grade(ctx, repoID, cfg, "", contracts.VisibilityPrivate)
	if err != nil {
		return err
	}
	app.PrintResult(out)
	return nil
}
