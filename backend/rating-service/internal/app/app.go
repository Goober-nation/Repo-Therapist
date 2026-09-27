package app

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"rating-service/internal/config"
	"rating-service/internal/database"
	"rating-service/internal/service"
	pb "rating-service/internal/transport/gen"
	"rating-service/internal/transport/rpc"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"google.golang.org/grpc"
)

func StartApp() {
	if err := godotenv.Load(); err != nil {
		log.Printf(".env not found, using environment variables")
	}

	cfg := config.LoadConfig()

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	db, err := pgxpool.New(ctx, cfg.Database.DSN())
	if err != nil {
		log.Fatalf("create postgres pool: %v", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		log.Fatalf("ping postgres: %v", err)
	}

	log.Println("postgres connected")

	rankingRepo := database.NewRankingRepository(db)
	ratingService := service.NewRatingService(rankingRepo)

	grpcServer := grpc.NewServer()
	pb.RegisterRatingServer(grpcServer, rpc.NewServer(ratingService))

	listener, err := net.Listen("tcp", cfg.GRPC.Address)
	if err != nil {
		log.Fatalf("listen grpc: %v", err)
	}

	log.Printf("grpc server listening on %s", cfg.GRPC.Address)

	serverErr := make(chan error, 1)

	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			serverErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Println("shutdown signal received")

	case err := <-serverErr:
		log.Printf("grpc server error: %v", err)
	}

	stop()

	grpcServer.GracefulStop()

	log.Println("rating service stopped")
}
