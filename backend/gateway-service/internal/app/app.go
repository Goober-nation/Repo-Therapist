package app

import (
	"context"
	"log"

	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gateway-service/internal/config"
	"gateway-service/internal/database/postgres"
	redis_repository "gateway-service/internal/database/redis"
	"gateway-service/internal/service"
	httptransport "gateway-service/internal/transport/http"
	"gateway-service/internal/transport/rpc/gen/agg"
	"gateway-service/internal/transport/rpc/gen/rank"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
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

	db, err := pgxpool.New(
		ctx,
		cfg.Database.DSN(),
	)
	if err != nil {
		log.Fatalf("create postgres pool: %v", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		log.Fatalf("ping postgres: %v", err)
	}

	log.Println("postgres connected")

	userRepository := postgres.NewUserRepository(db)

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Address,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("ping redis: %v", err)
	}

	log.Println("redis connected")

	sessionRepository := redis_repository.NewSessionRepository(
		redisClient,
		cfg.Session.TTL,
		cfg.Session.KeyPrefix,
	)

	rateLimiter := service.NewRedisLimiter(
		redisClient,
		cfg.RateLimit.Limit,
		cfg.RateLimit.Window,
	)
	aggregatorConn, err := grpc.Dial(
		cfg.Aggregator.Address,
		grpc.WithTransportCredentials(
			insecure.NewCredentials(),
		),
	)
	if err != nil {
		log.Fatalf(
			"connect to aggregator: %v",
			err,
		)
	}
	defer aggregatorConn.Close()

	aggregatorClient := agg.NewAggregatorClient(
		aggregatorConn,
	)

	log.Printf(
		"connected to aggregator: %s",
		cfg.Aggregator.Address,
	)

	ratingConn, err := grpc.Dial(
		cfg.Rating.Address,
		grpc.WithTransportCredentials(
			insecure.NewCredentials(),
		),
	)
	if err != nil {
		log.Fatalf(
			"connect to rating: %v",
			err,
		)
	}
	defer ratingConn.Close()

	ratingClient := rank.NewRatingClient(
		ratingConn,
	)

	log.Printf(
		"connected to rating: %s",
		cfg.Rating.Address,
	)

	yandexHTTPClient := &http.Client{
		Timeout: cfg.Yandex.Timeout,
	}

	yandexClient := httptransport.NewClient(
		yandexHTTPClient, cfg.Yandex.UserInfoURL)

	gatewayService := service.NewGatewayService(
		aggregatorClient,
		ratingClient,
		yandexClient,
		rateLimiter,
		userRepository,
		sessionRepository,
		cfg.Aggregator.Timeout,
		cfg.Rating.Timeout,
		cfg.Yandex.Timeout,
	)

	gatewayService.SetPublicAPILimiter(service.NewRedisLimiter(
		redisClient,
		cfg.PublicAPIRateLimit.Limit,
		cfg.PublicAPIRateLimit.Window,
	))

	handler := httptransport.NewHandler(
		gatewayService,
	)

	router := http.NewServeMux()

	handler.Register(router)

	loggedHandler := loggingMiddleware(router)

	httpServer := &http.Server{
		Addr:              cfg.HTTP.Address,
		Handler:           loggedHandler,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	serverErr := make(chan error, 1)

	go func() {
		log.Printf(
			"http server listening on %s",
			cfg.HTTP.Address,
		)

		if err := httpServer.ListenAndServe(); err != nil &&
			err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Println("shutdown signal received")

	case err := <-serverErr:
		log.Printf("http server error: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf(
			"http server shutdown: %v",
			err,
		)
	}

	log.Println("gateway service stopped")
}

func loggingMiddleware(
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			log.Println(
				"http request started method=" + r.Method + " path=" + r.URL.Path + " remote=" + r.RemoteAddr)

			recorder := &responseRecorder{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(
				recorder,
				r,
			)

			log.Printf(
				"http request completed method=%s path=%s status=%d duration=%s remote=%s",
				r.Method,
				r.URL.Path,
				recorder.statusCode,
				time.Since(start),
				r.RemoteAddr,
			)
		},
	)
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	if r.wroteHeader {
		return
	}

	r.statusCode = statusCode
	r.wroteHeader = true

	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(body []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}

	return r.ResponseWriter.Write(body)
}
