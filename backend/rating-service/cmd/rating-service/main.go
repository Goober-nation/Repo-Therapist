package main

import (
	"context"
	"github.com/joho/godotenv"
	"log"

	"rating-service/internal/app"
	"repo-therapist/runtimeconfig"
)

func main() {
	// .env before the config snapshot: its values are part of the environment layer, which always wins.
	if err := godotenv.Load(); err != nil {
		log.Printf(".env not loaded (%v), using the process environment", err)
	}
	// Settings (config/app.yaml + env + admin overrides) must load before anything reads them.
	if _, err := runtimeconfig.Start(context.Background(), "rating"); err != nil {
		log.Fatalf("config: %v", err)
	}
	app.StartApp()
}
