package app

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"repo-therapist/runtimeconfig"
	"syscall"
	"time"

	"rating-service/internal/config"
	"rating-service/internal/database"
	"rating-service/internal/metrics"
	"rating-service/internal/service"
	pb "rating-service/internal/transport/gen"
	"rating-service/internal/transport/rpc"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
)

func StartApp() {

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
	ratingService := service.NewRatingService(rankingRepo, cfg.DynamicsDays)
	ratingService.SetSizeBuckets(cfg.SizeSmallLOC, cfg.SizeLargeLOC)
	ratingService.SetAnalytics(rankingRepo, time.Duration(cfg.AnalyticsCacheSeconds)*time.Second)
	ratingService.SetSearch(rankingRepo)
	// Non-static settings follow config/app.yaml reloads and admin overrides.
	runtimeconfig.OnChange(func() {
		live := config.LoadConfig()
		ratingService.SetDynamicsDays(live.DynamicsDays)
		ratingService.SetSizeBuckets(live.SizeSmallLOC, live.SizeLargeLOC)
		ratingService.SetAnalyticsTTL(time.Duration(live.AnalyticsCacheSeconds) * time.Second)
	})

	appMetrics := metrics.New()
	appMetrics.Serve(ctx, cfg.MetricsAddress)
	ratingService.SetCacheRecorder(appMetrics)

	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(appMetrics.UnaryServerInterceptor()))
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
