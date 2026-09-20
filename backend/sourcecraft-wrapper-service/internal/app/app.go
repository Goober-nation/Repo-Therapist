package app

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sourcecraft-wrapper-service/internal/config"
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
	timeout, err := strconv.Atoi(cfg.ClientTimeout)
	if err != nil {
		log.Println("Invalid client timeout: ", err)
		return
	}
	client = http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}
	srvc := service.NewSCWrapperService(client, cfg.SourceCraftURL)
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
