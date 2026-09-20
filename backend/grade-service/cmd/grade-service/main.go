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
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"repo-therapist/grade-service/internal/app"
	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/transport"
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
		err = transport.RunRequest(*requestOrg, *requestRepo)
	case *listen:
		err = transport.RunListen(*configPath)
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

func runOnce(repoID, configPath string) error {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	out, err := app.Grade(ctx, repoID, cfg)
	if err != nil {
		return err
	}
	app.PrintResult(out)
	return nil
}
