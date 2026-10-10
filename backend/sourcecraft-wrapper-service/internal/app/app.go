package app

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sourcecraft-wrapper-service/internal/config"
	"sourcecraft-wrapper-service/internal/retry"
	"sourcecraft-wrapper-service/internal/service"
	pb "sourcecraft-wrapper-service/internal/transport/gen"
	"sourcecraft-wrapper-service/internal/transport/rpc"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"google.golang.org/grpc"
)

func StartApp() {
	if err := godotenv.Load(); err != nil {
		log.Printf("no .env file loaded: %v", err)
	}
	cfg := config.LoadConfig()
	var client http.Client
	timeout := intSetting("CLIENT_TIMEOUT", cfg.ClientTimeout, 5, 1)
	retryAttempts := intSetting("HTTP_RETRY_ATTEMPTS", cfg.RetryAttempts, 3, 1)
	rateLimitAttempts := intSetting("HTTP_RATE_LIMIT_ATTEMPTS", cfg.RateLimitAttempts, 6, 1)
	retryWait := intSetting("HTTP_RETRY_WAIT_SECONDS", cfg.RetryWaitSeconds, 1, 0)
	maxPages := intSetting("SOURCECRAFT_MAX_PAGES", cfg.MaxPages, 50, 1)
	// No Client.Timeout: it would cap all retry attempts together; the transport times each one.
	client = http.Client{
		Transport: &retry.Transport{
			Attempts:          retryAttempts,
			RateLimitAttempts: rateLimitAttempts,
			Wait:              time.Duration(retryWait) * time.Second,
			AttemptTimeout:    time.Duration(timeout) * time.Second,
		},
	}
	srvc := service.NewSCWrapperService(client, cfg.SourceCraftURL)
	srvc.MaxPages = maxPages
	lis, err := net.Listen("tcp", ":"+cfg.AppPort)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()

	serverRPC := rpc.NewServerRPC(srvc)
	pb.RegisterCodeHealthServer(grpcServer, serverRPC)

	go func() {
		log.Printf("Starting gRPC server on port %s", cfg.AppPort)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down gRPC server...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		log.Println("Server stopped gracefully")
	case <-ctx.Done():
		log.Println("Graceful shutdown timed out, forcing stop")
		grpcServer.Stop()
	}

	log.Println("Server exiting")
}

// intSetting parses a numeric setting; an invalid or too small value falls back to def with a log
// line instead of stopping the service (the container's entrypoint stops every service when one exits).
func intSetting(name, value string, def, min int) int {
	n, err := strconv.Atoi(value)
	if err != nil || n < min {
		log.Printf("ignoring invalid %s=%q, using %d", name, value, def)
		return def
	}
	return n
}
