package app

import (
	"aggregator-service/internal/config"
	postgres_repositories "aggregator-service/internal/database/postgres-repositories"
	redis_repositories "aggregator-service/internal/database/redis"
	"aggregator-service/internal/domain"
	"aggregator-service/internal/metrics"
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
	"repo-therapist/runtimeconfig"
	"repo-therapist/runtimeconfig/kafkasync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
	reposRepo := postgres_repositories.NewRepoRepository(db)
	gradesRepo := postgres_repositories.NewGradesRepository(db)
	llmRepo := postgres_repositories.NewLLMRepository(db)
	auditRepo := postgres_repositories.NewAuditRepository(db)
	batchRepo := postgres_repositories.NewBatchRepository(db)
	catalogRepo := postgres_repositories.NewCatalogRepository(db)

	redisClient := goredis.NewClient(&goredis.Options{
		Addr:     cfg.Redis.Address,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("ping redis: %v", err)
	}

	log.Println("redis connected")

	ensureTopics(ctx, cfg)

	// Admin overrides (docs/config.md): read config.updated up to date before anything uses the
	// settings, follow config.status for the env locks other processes report.
	configStore := runtimeconfig.Default()
	configSync := kafkasync.New(configStore, cfg.Kafka.Brokers, "aggregator")
	configSync.Start(ctx)
	configStatuses := kafkasync.ReadStatuses(ctx, cfg.Kafka.Brokers)
	settingsPublisher := kafkasync.NewPublisher(cfg.Kafka.Brokers)
	defer settingsPublisher.Close()

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
	appMetrics := metrics.New()
	appMetrics.Serve(ctx, cfg.MetricsAddress)
	appMetrics.RegisterScheduledRegrade(func() (bool, int, int) {
		status := aggregatorService.GetRegradeStatus(ctx)

		return status.Running, status.Total, status.Done
	})

	aggregatorService.Metrics = appMetrics
	aggregatorService.AuditRepo = auditRepo
	aggregatorService.BatchRepo = batchRepo
	aggregatorService.CatalogRepo = catalogRepo
	aggregatorService.BatchConcurrency = cfg.Batch.Concurrency
	aggregatorService.TokenStore = redis_repositories.NewTokenStore(redisClient)
	aggregatorService.GradeLimiter = service.NewGradeLimiter(cfg.Grading.RequestLimit, cfg.Grading.RequestWindow)
	// Non-static settings (config/app.yaml outside `static:`) are re-read on every reload and admin
	// override; static ones above (addresses, topics, BatchConcurrency) need a restart.
	applyLive := func() {
		live := config.LoadConfig()
		reposRepo.LockTimeout.Store(int64(live.Grading.LockTimeout))
		aggregatorService.UpdateLive(func(as *service.AggregatorService) {
			as.RPCTimeout = live.GRPC.Timeout
			as.CatalogMaxPages = live.Batch.CatalogMaxPages
			as.BatchItemTimeout = live.Batch.ItemTimeout
			as.BatchMaxLimit = live.Batch.MaxLimit
			as.TokenTTL = live.Grading.LockTimeout
			as.RegradeLimit = live.Grading.RegradeLimit
			as.RegradeMissingAnalytics = live.Grading.RegradeMissingAnalytics
			as.LLMRequestTTL = service.DefaultLLMRequestTTL
			if live.Grading.LLMRequestTTL > 0 {
				as.LLMRequestTTL = live.Grading.LLMRequestTTL
			}
			as.AuditRetentionDays = live.Audit.RetentionDays
		})
		aggregatorService.GradeLimiter.SetLimits(live.Grading.RequestLimit, live.Grading.RequestWindow)
		aggregatorService.SetRegradeInterval(live.Grading.RegradeInterval)
		if warning := config.LockTimeoutWarning(live.Grading.LockTimeout); warning != "" {
			log.Println(warning)
		}
	}
	applyLive()
	runtimeconfig.OnChange(applyLive)

	aggregatorService.Settings = &service.Settings{
		Repo:      postgres_repositories.NewSettingsRepository(db),
		Publisher: settingsPublisher,
		File:      configStore.File,
		Processes: func() []domain.ConfigProcess {
			statuses := map[string]service.ProcessStatus{}
			for name, st := range configStatuses.Statuses() {
				statuses[name] = service.ProcessStatus{Sections: st.Sections, EnvLocked: st.EnvLocked,
					RestartPending: st.RestartPending, ReportedAt: st.ReportedAt}
			}

			return service.ProcessesFromStatuses(statuses)
		},
		OwnEnvLocked: func(key string) bool { return configStore.Owns(key) && configStore.EnvLocked(key) },
	}
	if err := aggregatorService.ReconcileSettings(ctx, configSync.Seen()); err != nil {
		log.Printf("config: reconciling admin overrides with Kafka: %v", err)
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

	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(appMetrics.UnaryServerInterceptor()))

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

	aggregatorService.StartAuditRetention(ctx)
	aggregatorService.ResumeBatch(ctx)
	aggregatorService.StartCatalog(ctx)

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

// ensureTopics provisions the topics the aggregator reads and writes before any consumer joins,
// retrying while the broker starts up. A failure is logged, not fatal: stored grades are still
// served over gRPC, and another service may create the topics later.
func ensureTopics(ctx context.Context, cfg *config.Config) {
	topics := []string{
		cfg.Kafka.GradingRequestsTopic,
		cfg.Kafka.GradingResultsTopic,
		cfg.Kafka.LLMRequestsTopic,
		cfg.Kafka.LLMRecommendationsTopic,
	}

	var err error
	for attempt := 1; attempt <= 10; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = kafka.EnsureTopics(attemptCtx, cfg.Kafka.Brokers, topics)
		cancel()
		if err == nil {
			log.Printf("kafka topics ready: %v", topics)
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}

	log.Printf("ensure kafka topics: %v", err)
}
