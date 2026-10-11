package main

import (
	"context"
	"github.com/joho/godotenv"
	"log"

	"repo-therapist/runtimeconfig"
	"sourcecraft-wrapper-service/internal/app"
)

func main() {
	// .env before the config snapshot: its values are part of the environment layer, which always wins.
	if err := godotenv.Load(); err != nil {
		log.Printf(".env not loaded (%v), using the process environment", err)
	}
	// Settings (config/app.yaml + env + admin overrides) must load before anything reads them.
	if _, err := runtimeconfig.Start(context.Background(), "sourcecraft_wrapper"); err != nil {
		log.Fatalf("config: %v", err)
	}
	app.StartApp()
}
