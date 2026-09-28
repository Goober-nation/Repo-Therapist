package app

import (
	"aggregator-service/internal/config"
	postgres_repositories "aggregator-service/internal/database/postgres-repositories"
	"aggregator-service/internal/service"
	"aggregator-service/internal/transport/kafka"
	grpctransport "aggregator-service/internal/transport/rpc"
	"aggregator-service/internal/transport/rpc/gen/client"
	pb "aggregator-service/internal/transport/rpc/gen/server"
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func StartApp() {
	err := godotenv.Load()
	if err != nil {
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
	reposRepo := postgres_repositories.NewRepoRepository(db)
	gradesRepo := postgres_repositories.NewGradesRepository(db)
	llmRepo := postgres_repositories.NewLLMRepository(db)

	gradingPublisher := kafka.NewGradingPublisher(
		cfg.Kafka.Brokers,
		cfg.Kafka.GradingRequestsTopic,
	)

	llmPublisher := kafka.NewLLMPublisher(
		cfg.Kafka.Brokers,
		cfg.Kafka.LLMRequestsTopic,
	)

	wrapperConn, err := grpc.Dial(
		cfg.SourceCraftWrapper.GRPCAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("connect to sourcecraft wrapper: %v", err)
	}
	defer wrapperConn.Close()

	wrapperClient := client.NewCodeHealthClient(wrapperConn)

	aggregatorService := service.NewAggregatorService(
		reposRepo,
		gradesRepo,
		llmRepo,
		wrapperClient,
		gradingPublisher,
		llmPublisher,
		cfg.GRPC.Timeout,
		cfg.Token.Token,
	)
	aggregatorService.RegradeLimit = cfg.Grading.RegradeLimit
	aggregatorService.RegradeListTimeout = cfg.Grading.RegradeListTimeout
	if cfg.Grading.LLMRequestTTL > 0 {
		aggregatorService.LLMRequestTTL = cfg.Grading.LLMRequestTTL
	}

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

	aggregatorService.StartPublicGrading(
		ctx,
		cfg.Grading.RegradeInterval,
		cfg.Grading.RegradeOnStart,
	)

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
	log.Println("aggregator service stopped")
}
