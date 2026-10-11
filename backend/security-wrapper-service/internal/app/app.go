package app

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"repo-therapist/runtimeconfig"
	"repo-therapist/security-wrapper-service/internal/appsecclient"
	"repo-therapist/security-wrapper-service/internal/config"
	"repo-therapist/security-wrapper-service/internal/retry"
	"repo-therapist/security-wrapper-service/internal/service"
	pb "repo-therapist/security-wrapper-service/internal/transport/gen"
	"repo-therapist/security-wrapper-service/internal/transport/rpc"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/grpc"
)

func StartApp() {
	cfg := config.LoadConfig()

	// No Client.Timeout: it would cap all retry attempts together; the transport times each one.
	transport := newSwapTransport(retryTransport(cfg))
	httpClient := http.Client{Transport: transport}
	client := appsecclient.New(httpClient, cfg.AppsecURL)
	pollInterval, pollTimeout := polling(cfg)
	wrapper := service.NewWrapper(client, pollInterval, pollTimeout)
	// Retries, timeouts and polling follow config/app.yaml reloads; APP_PORT and APPSEC_URL need
	// a restart.
	runtimeconfig.OnChange(func() {
		live := config.LoadConfig()
		transport.set(retryTransport(live))
		wrapper.SetPolling(polling(live))
	})

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
func retryTransport(cfg *config.Config) *retry.Transport {
	return &retry.Transport{
		Attempts:       intSetting("HTTP_RETRY_ATTEMPTS", cfg.RetryAttempts, 3, 1),
		Wait:           time.Duration(intSetting("HTTP_RETRY_WAIT_SECONDS", cfg.RetryWaitSeconds, 1, 0)) * time.Second,
		AttemptTimeout: time.Duration(intSetting("CLIENT_TIMEOUT", cfg.ClientTimeout, 5, 1)) * time.Second,
	}
}

func polling(cfg *config.Config) (interval, timeout time.Duration) {
	interval = time.Duration(intSetting("REPORT_POLL_INTERVAL_SECONDS", cfg.PollIntervalSeconds, 2, 1)) * time.Second
	timeout = time.Duration(intSetting("REPORT_POLL_TIMEOUT_SECONDS", cfg.PollTimeoutSeconds, 60, 1)) * time.Second
	return interval, timeout
}

func intSetting(name, value string, def, min int) int {
	n, err := strconv.Atoi(value)
	if err != nil || n < min {
		log.Printf("ignoring invalid %s=%q, using %d", name, value, def)
		return def
	}
	return n
}
