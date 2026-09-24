package app

import (
	"aggregator-service/internal/config"
	postgres_repositories "aggregator-service/internal/database/postgres-repositories"
	"aggregator-service/internal/service"
	"aggregator-service/internal/transport/kafka"
	pb "aggregator-service/internal/transport/rpc/gen"
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	grpctransport "aggregator-service/internal/transport/rpc"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func StartApp() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal(err)
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

	// -------------------------------------------------------------------------
	// Repositories
	// -------------------------------------------------------------------------

	reposRepo := postgres_repositories.NewRepoRepository(db)
	gradesRepo := postgres_repositories.NewGradesRepository(db)
	llmRepo := postgres_repositories.NewLLMRepository(db)

	// -------------------------------------------------------------------------
	// Kafka publishers
	// -------------------------------------------------------------------------

	gradingPublisher := kafka.NewGradingPublisher(
		cfg.Kafka.Brokers,
		cfg.Kafka.GradingRequestsTopic,
	)

	llmPublisher := kafka.NewLLMPublisher(
		cfg.Kafka.Brokers,
		cfg.Kafka.LLMRequestsTopic,
	)

	// -------------------------------------------------------------------------
	// SourceCraft wrapper RPC client
	// -------------------------------------------------------------------------

	wrapperConn, err := grpc.Dial(
		cfg.SourceCraftWrapper.GRPCAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("connect to sourcecraft wrapper: %v", err)
	}
	defer wrapperConn.Close()

	wrapperClient := pb.NewCodeHealthClient(wrapperConn)

	// -------------------------------------------------------------------------
	// Aggregator service
	// -------------------------------------------------------------------------

	aggregatorService := service.NewAggregatorService(
		reposRepo,
		gradesRepo,
		llmRepo,
		wrapperClient,
		gradingPublisher,
		llmPublisher,
	)

	// -------------------------------------------------------------------------
	// Kafka consumers
	// -------------------------------------------------------------------------

	gradeConsumer := kafka.NewGradeConsumer(
		cfg.Kafka.Brokers,
		cfg.Kafka.GradingResultsTopic,
		cfg.Kafka.GradingConsumerGroup,
		aggregatorService,
	)

	llmConsumer := kafka.NewLLMConsumer(
		cfg.Kafka.Brokers,
		cfg.Kafka.LLMRecommendationsTopic,
		cfg.Kafka.LLMConsumerGroup,
		aggregatorService,
	)

	gradeConsumer.Start(ctx)
	llmConsumer.Start(ctx)

	log.Println("kafka consumers started")

	grpcServer := grpc.NewServer()

	rpcServer := grpctransport.NewServer(
		aggregatorService,
	)

	pb.RegisterAggregatorServer(
		grpcServer,
		rpcServer,
	)

	listener, err := net.Listen(
		"tcp",
		cfg.GRPC.Address,
	)
	if err != nil {
		log.Fatalf("listen grpc: %v", err)
	}

	log.Printf("grpc server listening on %s", cfg.GRPC.Address)

	// -------------------------------------------------------------------------
	// Public grading ticker
	// -------------------------------------------------------------------------

	aggregatorService.StartPublicGrading(
		ctx,
		cfg.Grading.RegradeInterval,
	)

	// -------------------------------------------------------------------------
	// Start gRPC
	// -------------------------------------------------------------------------

	serverErr := make(chan error, 1)

	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			serverErr <- err
		}
	}()

	// -------------------------------------------------------------------------
	// Wait for shutdown
	// -------------------------------------------------------------------------

	select {
	case <-ctx.Done():
		log.Println("shutdown signal received")

	case err := <-serverErr:
		log.Printf("grpc server error: %v", err)
	}

	// -------------------------------------------------------------------------
	// Graceful shutdown
	// -------------------------------------------------------------------------

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	stop()

	grpcServer.GracefulStop()

	_ = listener.Close()

	if err := gradeConsumer.Close(); err != nil {
		log.Printf("close grade consumer: %v", err)
	}

	if err := llmConsumer.Close(); err != nil {
		log.Printf("close llm consumer: %v", err)
	}

	if err := gradingPublisher.Close(); err != nil {
		log.Printf("close grading publisher: %v", err)
	}

	if err := llmPublisher.Close(); err != nil {
		log.Printf("close llm publisher: %v", err)
	}

	wrapperConn.Close()

	select {
	case <-shutdownCtx.Done():
		log.Println("shutdown timeout")
	default:
		log.Println("aggregator service stopped")
	}
}
