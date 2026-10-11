package app

import (
	"context"
	"log"
	"repo-therapist/runtimeconfig"

	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gateway-service/internal/config"
	"gateway-service/internal/database/postgres"
	redis_repository "gateway-service/internal/database/redis"
	"gateway-service/internal/metrics"
	"gateway-service/internal/series"
	"gateway-service/internal/service"
	httptransport "gateway-service/internal/transport/http"
	"gateway-service/internal/transport/rpc/gen/agg"
	"gateway-service/internal/transport/rpc/gen/rank"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
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
	appMetrics := metrics.New()
	appMetrics.Serve(ctx, cfg.Metrics.Address)

	aggregatorConn, err := grpc.Dial(
		cfg.Aggregator.Address,
		grpc.WithTransportCredentials(
			insecure.NewCredentials(),
		),
		grpc.WithChainUnaryInterceptor(appMetrics.UnaryClientInterceptor("aggregator")),
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
		grpc.WithChainUnaryInterceptor(appMetrics.UnaryClientInterceptor("rating")),
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

	// No client-wide Timeout: every Yandex call carries a context deadline of YANDEX_TIMEOUT, read
	// per call so config changes apply without a restart.
	yandexHTTPClient := &http.Client{}

	var yandexClient httptransport.YandexIDClient = httptransport.NewClient(
		yandexHTTPClient, cfg.Yandex.UserInfoURL)
	if cfg.Yandex.DevBypassUserID != "" {
		log.Printf("WARNING: DEV_AUTH_BYPASS_USER_ID is set: any token signs in as Yandex ID %q "+
			"and the session cookie is not Secure. Never enable this on a public instance.",
			cfg.Yandex.DevBypassUserID)
		yandexClient = httptransport.NewDevBypassClient(cfg.Yandex.DevBypassUserID)
	}

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

	publicAPILimiter := service.NewRedisLimiter(redisClient, cfg.PublicAPIRateLimit.Limit, cfg.PublicAPIRateLimit.Window)
	gatewayService.SetPublicAPILimiter(publicAPILimiter)

	exportLimiter := service.NewRedisLimiter(redisClient, cfg.ExportRateLimit.Limit, cfg.ExportRateLimit.Window)
	gatewayService.SetExportLimiter(exportLimiter)

	gatewayService.SetMetrics(appMetrics)

	deniedLogLimiter := service.NewRedisLimiter(redisClient, cfg.Admin.DeniedLog.Limit, cfg.Admin.DeniedLog.Window)
	gatewayService.SetAdmin(
		cfg.Admin.YandexIDs,
		cfg.Admin.HashSecret(),
		deniedLogLimiter,
	)

	attemptGuard := redis_repository.NewAttemptGuard(redisClient, cfg.Admin.UnlockMaxAttempts, cfg.Admin.UnlockLock)
	gatewayService.SetAdminUnlock(
		cfg.Admin.Password,
		redis_repository.NewAdminGrantStore(redisClient),
		attemptGuard,
		cfg.Session.TTL,
	)

	// Non-static settings (config/app.yaml outside `static:`) are re-read on every reload and admin
	// override. Rate-limit counters live in Redis, so changing a limit keeps them.
	var seriesKey string
	applyLive := func() {
		live := config.LoadConfig()
		rateLimiter.SetLimits(live.RateLimit.Limit, live.RateLimit.Window)
		publicAPILimiter.SetLimits(live.PublicAPIRateLimit.Limit, live.PublicAPIRateLimit.Window)
		exportLimiter.SetLimits(live.ExportRateLimit.Limit, live.ExportRateLimit.Window)
		deniedLogLimiter.SetLimits(live.Admin.DeniedLog.Limit, live.Admin.DeniedLog.Window)
		attemptGuard.SetLimits(live.Admin.UnlockMaxAttempts, live.Admin.UnlockLock)
		gatewayService.SetTimeouts(live.Aggregator.Timeout, live.Rating.Timeout, live.Yandex.Timeout)

		if live.ForceRegradeCooldown > 0 {
			gatewayService.SetForceRegradeCooldown(redis_repository.NewCooldown(redisClient, live.ForceRegradeCooldown))
		} else {
			gatewayService.SetForceRegradeCooldown(nil)
		}

		if live.Admin.UserAuditWindow > 0 {
			gatewayService.SetUserAudit(service.NewRedisLimiter(redisClient, 1, live.Admin.UserAuditWindow))
		} else {
			gatewayService.SetUserAudit(nil)
		}

		// A new series client only when its settings change, so its cache survives other reloads.
		if key := live.Metrics.PrometheusURL + "|" + live.Metrics.SeriesCache.String(); key != seriesKey {
			seriesKey = key
			if live.Metrics.PrometheusURL != "" {
				gatewayService.SetSeries(series.NewClient(live.Metrics.PrometheusURL, live.Metrics.SeriesCache, nil))
				log.Printf("graphs read metrics from %s", live.Metrics.PrometheusURL)
			} else {
				gatewayService.SetSeries(nil)
				log.Println("graphs are off: set PROMETHEUS_URL to turn them on")
			}
		}
	}
	applyLive()
	runtimeconfig.OnChange(applyLive)

	if gatewayService.UnlockAvailable() {
		log.Printf("admin password unlock is on: %d wrong tries lock for %s", cfg.Admin.UnlockMaxAttempts, cfg.Admin.UnlockLock)
	}

	if cfg.Admin.AuditHashSecret == "" && gatewayService.AdminEnabled() {
		log.Println("AUDIT_HASH_SECRET is not set: the audit hash key is derived from the admin settings and changes with them")
	}

	if gatewayService.AdminEnabled() {
		log.Printf("admin routes are on for %d admin(s)", len(cfg.Admin.YandexIDs))
	} else {
		log.Println("admin routes are off: set ADMIN_PASSWORD or ADMIN_YANDEX_IDS to turn them on")
	}

	handler := httptransport.NewHandler(
		gatewayService,
	)
	handler.SetInsecureCookies(cfg.Yandex.DevBypassUserID != "")
	gatewayService.SetDevAuthBypass(cfg.Yandex.DevBypassUserID != "")

	router := http.NewServeMux()

	handler.Register(router)

	loggedHandler := loggingMiddleware(router, appMetrics.ObserveHTTP)

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

// loggingMiddleware logs every request and reports its matched route pattern, status and duration to
// observe. The pattern is read after the mux has handled the request: it sets it on the request itself.
func loggingMiddleware(
	next http.Handler,
	observe func(pattern string, status int, duration time.Duration),
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

			observe(r.Pattern, recorder.statusCode, time.Since(start))

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
