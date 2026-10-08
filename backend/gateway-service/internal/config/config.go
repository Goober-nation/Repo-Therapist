package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTP       HTTPConfig
	Aggregator GRPCServiceConfig
	Rating     GRPCServiceConfig
	Database   DatabaseConfig
	Redis      RedisConfig
	Yandex     YandexConfig
	RateLimit  RateLimitConfig
	// PublicAPIRateLimit is the per-IP budget of the public JSON score API.
	PublicAPIRateLimit RateLimitConfig
	// ExportRateLimit is the per-IP budget of the public grades export, which reads every grade.
	ExportRateLimit RateLimitConfig
	Session         SessionConfig
	Admin           AdminConfig
	Metrics         MetricsConfig
}

// MetricsConfig is the monitoring side (docs/metrics.md).
type MetricsConfig struct {
	// Address serves this service's /metrics for Prometheus (METRICS_ADDRESS); empty leaves it off.
	// It is a separate port from the public HTTP one and is never published.
	Address string
	// PrometheusURL is where the graphs of the admin tab and the Analytics tab read their series
	// (PROMETHEUS_URL); empty turns the graphs off.
	PrometheusURL string
	// SeriesCache is how long one graph answer is reused (SERIES_CACHE_SECONDS), so many viewers
	// cost Prometheus one query.
	SeriesCache time.Duration
}

// AdminConfig controls who may use the admin routes and how they are audited (docs/admin.md).
type AdminConfig struct {
	// YandexIDs are the Yandex IDs of the admins (ADMIN_YANDEX_IDS, comma-separated). Empty means
	// there are no admins.
	YandexIDs []string
	// AuditHashSecret keys the HMAC that turns a Yandex ID into the actor hash in the audit log
	// (AUDIT_HASH_SECRET). Optional: see HashSecret.
	AuditHashSecret string
	// DeniedLog caps how many denied admin attempts per client IP reach the audit log, so a flood of
	// probes cannot fill it.
	DeniedLog RateLimitConfig
	// Password unlocks admin rights for a signed-in account (ADMIN_PASSWORD, plain text in the
	// environment); empty turns the unlock off. UnlockMaxAttempts wrong passwords lock the client IP and
	// the account for UnlockLock.
	Password          string
	UnlockMaxAttempts int
	UnlockLock        time.Duration
}
type HTTPConfig struct {
	Address           string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

type GRPCServiceConfig struct {
	Address string
	Timeout time.Duration
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	SSLMode  string
	DBName   string
}

func (dbc DatabaseConfig) DSN() string {
	port, err := strconv.Atoi(dbc.Port)
	if err != nil {
		port = 5432
	}

	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		dbc.User,
		dbc.Password,
		dbc.Host,
		port,
		dbc.DBName,
		dbc.SSLMode,
	)
}

type RedisConfig struct {
	Address  string
	Password string
	DB       int
}

type YandexConfig struct {
	UserInfoURL string
	Timeout     time.Duration
}

type RateLimitConfig struct {
	Limit  int
	Window time.Duration
}

type SessionConfig struct {
	TTL       time.Duration
	KeyPrefix string
}

func LoadConfig() *Config {
	return &Config{
		HTTP: HTTPConfig{
			Address: getEnvDefault(
				"HTTP_ADDRESS",
				":8080",
			),
			ReadHeaderTimeout: getEnvDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			ReadTimeout:       getEnvDuration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:      getEnvDuration("HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:       getEnvDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
		},

		Aggregator: GRPCServiceConfig{
			Address: getEnvDefault(
				"AGGREGATOR_GRPC_ADDRESS",
				"localhost:8083",
			),
			Timeout: getEnvDuration(
				"AGGREGATOR_GRPC_TIMEOUT",
				10*time.Second,
			),
		},

		Rating: GRPCServiceConfig{
			Address: getEnvDefault(
				"RATING_GRPC_ADDRESS",
				"localhost:8086",
			),
			Timeout: getEnvDuration(
				"RATING_GRPC_TIMEOUT",
				10*time.Second,
			),
		},

		Database: DatabaseConfig{
			Host: getEnvDefault(
				"DB_HOST",
				"localhost",
			),
			Port: getEnvDefault(
				"DB_PORT",
				"5432",
			),
			User: getEnvDefault(
				"DB_USER",
				"postgres",
			),
			Password: getEnvDefault(
				"DB_PASSWORD",
				"postgres",
			),
			DBName: getEnvDefault(
				"DB_NAME",
				"postgres",
			),
			SSLMode: getEnvDefault(
				"DB_SSL_MODE",
				"disable",
			),
		},

		Redis: RedisConfig{
			Address: getEnvDefault(
				"REDIS_ADDRESS",
				"localhost:6379",
			),
			Password: getEnvDefault(
				"REDIS_PASSWORD",
				"",
			),
			DB: getEnvIntDefault(
				"REDIS_DB",
				0,
			),
		},

		Yandex: YandexConfig{
			UserInfoURL: getEnvDefault(
				"YANDEX_USER_INFO_URL",
				"https://login.yandex.ru/info?format=json",
			),
			Timeout: getEnvDuration(
				"YANDEX_TIMEOUT",
				5*time.Second,
			),
		},

		RateLimit: RateLimitConfig{
			Limit: getEnvIntDefault(
				"RATE_LIMIT",
				10,
			),
			Window: getEnvDuration(
				"RATE_LIMIT_WINDOW",
				time.Minute,
			),
		},

		PublicAPIRateLimit: RateLimitConfig{
			Limit: getEnvIntDefault(
				"PUBLIC_API_RATE_LIMIT",
				300,
			),
			Window: getEnvDuration(
				"PUBLIC_API_RATE_LIMIT_WINDOW",
				time.Minute,
			),
		},

		ExportRateLimit: RateLimitConfig{
			Limit: getEnvIntDefault(
				"EXPORT_RATE_LIMIT",
				5,
			),
			Window: getEnvDuration(
				"EXPORT_RATE_LIMIT_WINDOW",
				time.Minute,
			),
		},

		Admin: AdminConfig{
			YandexIDs:       getEnvList("ADMIN_YANDEX_IDS"),
			AuditHashSecret: getEnvDefault("AUDIT_HASH_SECRET", ""),
			DeniedLog: RateLimitConfig{
				Limit:  getEnvIntDefault("ADMIN_DENIED_LOG_LIMIT", 5),
				Window: getEnvDuration("ADMIN_DENIED_LOG_WINDOW", 10*time.Minute),
			},
			Password:          os.Getenv("ADMIN_PASSWORD"),
			UnlockMaxAttempts: getEnvIntDefault("ADMIN_UNLOCK_MAX_ATTEMPTS", 5),
			UnlockLock:        getEnvDuration("ADMIN_UNLOCK_LOCK", 15*time.Minute),
		},

		Metrics: MetricsConfig{
			Address:       getEnvDefault("METRICS_ADDRESS", ""),
			PrometheusURL: getEnvDefault("PROMETHEUS_URL", ""),
			SeriesCache:   time.Duration(getEnvIntDefault("SERIES_CACHE_SECONDS", 60)) * time.Second,
		},

		Session: SessionConfig{
			TTL: getEnvDuration(
				"SESSION_TTL",
				24*time.Hour,
			),
			KeyPrefix: getEnvDefault(
				"SESSION_KEY_PREFIX",
				"session:",
			),
		},
	}
}

func getEnvDefault(
	key string,
	def string,
) string {
	value := os.Getenv(key)
	if value == "" {
		return def
	}

	return value
}

// HashSecret is the key of the audit actor hash and of the unlock fingerprint. It is AUDIT_HASH_SECRET
// when set. Otherwise it is derived from the admin password and the admin list, so setting only
// ADMIN_PASSWORD (or only ADMIN_YANDEX_IDS) is enough to turn the admin tab on; the hashes then change
// when those settings change, which AUDIT_HASH_SECRET avoids. Empty when there is nothing to derive from,
// which keeps the admin routes off.
func (a AdminConfig) HashSecret() string {
	if a.AuditHashSecret != "" {
		return a.AuditHashSecret
	}

	if a.Password == "" && len(a.YandexIDs) == 0 {
		return ""
	}

	sum := sha256.Sum256([]byte("repo-health audit key v1\x00" + a.Password + "\x00" + strings.Join(a.YandexIDs, ",")))

	return hex.EncodeToString(sum[:])
}

// getEnvList reads a comma-separated list, trimming blanks and dropping empty items.
func getEnvList(key string) []string {
	var items []string

	for _, item := range strings.Split(os.Getenv(key), ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}

	return items
}

func getEnvIntDefault(
	key string,
	def int,
) int {
	value := os.Getenv(key)
	if value == "" {
		return def
	}

	result, err := strconv.Atoi(value)
	if err != nil {
		return def
	}

	return result
}

func getEnvDuration(
	key string,
	def time.Duration,
) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return def
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return def
	}

	return duration
}
