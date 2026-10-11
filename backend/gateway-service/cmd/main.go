package main

import (
	"context"
	"github.com/joho/godotenv"
	"log"

	"gateway-service/internal/app"
	"repo-therapist/runtimeconfig"
	"repo-therapist/runtimeconfig/kafkasync"
)

func main() {
	// .env before the config snapshot: its values are part of the environment layer, which always wins.
	if err := godotenv.Load(); err != nil {
		log.Printf(".env not loaded (%v), using the process environment", err)
	}
	// Settings (config/app.yaml + env + admin overrides) must load before anything reads them.
	store, err := runtimeconfig.Start(context.Background(), "gateway")
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	// Admin overrides (config.updated) are read up to date before the service starts work.
	kafkasync.Run(context.Background(), store, "gateway")
	app.StartApp()
}
