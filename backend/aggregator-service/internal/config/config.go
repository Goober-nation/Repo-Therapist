package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	GRPC               GRPCConfig
	Database           DatabaseConfig
	Kafka              KafkaConfig
	SourceCraftWrapper WrapperConfig
	Grading            GradingConfig
	Token              ServiceToken
	Redis              RedisConfig
	Audit              AuditConfig
	Batch              BatchConfig
	// MetricsAddress serves /metrics for Prometheus (METRICS_ADDRESS); empty leaves it off. It is its own
	// port, never published (docs/metrics.md).
	MetricsAddress string
}

// BatchConfig tunes the admin batch regrade (docs/admin.md).
type BatchConfig struct {
	// Concurrency is how many repos of a batch are graded at once.
	Concurrency int
	// MaxRepos caps one batch; a filter that matches more is refused.
	MaxRepos int
	// ItemTimeout is how long one repo may take before it counts as failed.
	ItemTimeout time.Duration
}

// AuditConfig is the audit log's retention (docs/admin.md).
type AuditConfig struct {
	// RetentionDays deletes audit entries older than this many days; 0 keeps them forever.
	RetentionDays int
}

// RedisConfig uses the same env names as gateway-service.
type RedisConfig struct {
	Address  string
	Password string
	DB       int
}
type ServiceToken struct {
	Token string
}
type GRPCConfig struct {
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

type KafkaConfig struct {
	Brokers []string

	GradingRequestsTopic string
	LLMRequestsTopic     string

	GradingResultsTopic     string
	LLMRecommendationsTopic string

	GradingConsumerGroup string
	LLMConsumerGroup     string
}

type WrapperConfig struct {
	GRPCAddress string
}

type GradingConfig struct {
	RegradeInterval time.Duration
	// RegradeOnStart runs the public regrade once at startup, before the first interval tick.
	RegradeOnStart bool
	// RegradeLimit caps each public regrade to the top N public repos by SourceCraft rating;
	// 0 means every listed repo.
	RegradeLimit int
	// RegradeListTimeout bounds listing every public repo at the start of a regrade.
	RegradeListTimeout time.Duration
	LLMRequestTTL      time.Duration
	// RegradeMissingAnalytics regrades a repo whose grade has no analytics row yet.
	RegradeMissingAnalytics bool
	// RequestLimit caps new gradings started by users within RequestWindow, service-wide;
	// 0 disables the limit.
	RequestLimit  int
	RequestWindow time.Duration
	// LockTimeout frees a repo lock busy longer than this (a grading that never reported back);
	// keep it above CLONE_TIMEOUT + GRADE_TIMEOUT. 0 disables it.
	LockTimeout time.Duration
}

func LoadConfig() *Config {
	return &Config{
		Audit: AuditConfig{
			RetentionDays: getEnvInt("AUDIT_RETENTION_DAYS", 30),
		},

		MetricsAddress: getEnvDefault("METRICS_ADDRESS", ""),

		Batch: BatchConfig{
			Concurrency: getEnvInt("BATCH_REGRADE_CONCURRENCY", 3),
			MaxRepos:    getEnvInt("BATCH_REGRADE_MAX_REPOS", 500),
			ItemTimeout: getEnvDuration("BATCH_REGRADE_ITEM_TIMEOUT", 15*time.Minute),
		},

		Redis: RedisConfig{
			Address:  getEnvDefault("REDIS_ADDRESS", "localhost:6379"),
			Password: getEnvDefault("REDIS_PASSWORD", ""),
			DB:       getEnvInt("REDIS_DB", 0),
		},
		Token: ServiceToken{
			Token: getEnvDefault(
				"SERVICE_TOKEN",
				"",
			),
		},
		GRPC: GRPCConfig{
			Address: getEnvDefault(
				"GRPC_ADDRESS",
				":8083",
			),
			Timeout: getEnvDuration(
				"GRPC_TIMEOUT",
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

		Kafka: KafkaConfig{
			Brokers: getEnvList(
				"KAFKA_BROKERS",
				[]string{"localhost:9092"},
			),

			GradingRequestsTopic: getEnvDefault(
				"KAFKA_GRADING_REQUESTS_TOPIC",
				"grading.requests",
			),

			LLMRequestsTopic: getEnvDefault(
				"KAFKA_LLM_REQUESTS_TOPIC",
				"llm.recommendations.requested",
			),

			GradingResultsTopic: getEnvDefault(
				"KAFKA_GRADING_RESULTS_TOPIC",
				"grading.results",
			),

			LLMRecommendationsTopic: getEnvDefault(
				"KAFKA_LLM_RECOMMENDATIONS_TOPIC",
				"llm.recommendations.received",
			),

			GradingConsumerGroup: getEnvDefault(
				"KAFKA_GRADING_CONSUMER_GROUP",
				"aggregator-grading",
			),

			LLMConsumerGroup: getEnvDefault(
				"KAFKA_LLM_CONSUMER_GROUP",
				"aggregator-llm",
			),
		},

		SourceCraftWrapper: WrapperConfig{
			GRPCAddress: getEnvDefault(
				"WRAPPER_GRPC_ADDRESS",
				"localhost:8082",
			),
		},

		Grading: GradingConfig{
			RegradeInterval: getEnvDuration(
				"GRADING_REGRADE_INTERVAL",
				72*time.Hour,
			),
			RegradeOnStart: getEnvBool(
				"GRADING_REGRADE_ON_START",
				false,
			),
			RegradeLimit: getEnvInt(
				"GRADING_REGRADE_LIMIT",
				20,
			),
			RegradeListTimeout: getEnvDuration(
				"GRADING_REGRADE_LIST_TIMEOUT",
				2*time.Minute,
			),
			LLMRequestTTL: getEnvDuration(
				"LLM_REQUEST_TTL",
				2*time.Minute,
			),
			RegradeMissingAnalytics: getEnvBool("ANALYTICS_REGRADE_MISSING", true),
			RequestLimit: getEnvInt(
				"GRADING_REQUEST_LIMIT",
				10,
			),
			RequestWindow: getEnvDuration(
				"GRADING_REQUEST_WINDOW",
				10*time.Minute,
			),
			LockTimeout: getEnvDuration(
				"GRADING_LOCK_TIMEOUT",
				20*time.Minute,
			),
		},
	}
}

func getEnvDefault(key string, def string) string {
	value := os.Getenv(key)
	if value == "" {
		return def
	}

	return value
}

func getEnvBool(key string, def bool) bool {
	value, err := strconv.ParseBool(os.Getenv(key))
	if err != nil {
		return def
	}

	return value
}

func getEnvInt(key string, def int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value < 0 {
		return def
	}

	return value
}

func getEnvList(key string, def []string) []string {
	value := os.Getenv(key)
	if value == "" {
		return def
	}

	var result []string

	for _, item := range splitComma(value) {
		if item != "" {
			result = append(result, item)
		}
	}

	if len(result) == 0 {
		return def
	}

	return result
}

func getEnvDuration(key string, def time.Duration) time.Duration {
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

func splitComma(value string) []string {
	var result []string
	current := ""

	for _, char := range value {
		if char == ',' {
			result = append(result, current)
			current = ""
			continue
		}

		current += string(char)
	}

	result = append(result, current)

	return result
}
