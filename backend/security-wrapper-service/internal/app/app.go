package app

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"repo-therapist/security-wrapper-service/internal/appsecclient"
	"repo-therapist/security-wrapper-service/internal/config"
	"repo-therapist/security-wrapper-service/internal/retry"
	"repo-therapist/security-wrapper-service/internal/service"
	pb "repo-therapist/security-wrapper-service/internal/transport/gen"
	"repo-therapist/security-wrapper-service/internal/transport/rpc"
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

	clientTimeout := intSetting("CLIENT_TIMEOUT", cfg.ClientTimeout, 5, 1)
	pollInterval := intSetting("REPORT_POLL_INTERVAL_SECONDS", cfg.PollIntervalSeconds, 2, 1)
	pollTimeout := intSetting("REPORT_POLL_TIMEOUT_SECONDS", cfg.PollTimeoutSeconds, 60, 1)

	retryAttempts := intSetting("HTTP_RETRY_ATTEMPTS", cfg.RetryAttempts, 3, 1)
	retryWait := intSetting("HTTP_RETRY_WAIT_SECONDS", cfg.RetryWaitSeconds, 1, 0)

	// No Client.Timeout: it would cap all retry attempts together; the transport times each one.
	httpClient := http.Client{Transport: &retry.Transport{
		Attempts:       retryAttempts,
		Wait:           time.Duration(retryWait) * time.Second,
		AttemptTimeout: time.Duration(clientTimeout) * time.Second,
	}}
	client := appsecclient.New(httpClient, cfg.AppsecURL)
	wrapper := service.NewWrapper(
		client,
		time.Duration(pollInterval)*time.Second,
		time.Duration(pollTimeout)*time.Second,
	)

	lis, err := net.Listen("tcp", ":"+cfg.AppPort)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	serverRPC := rpc.NewServerRPC(wrapper)
	pb.RegisterSecurityWrapperServer(grpcServer, serverRPC)

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
