package config

import (
	"fmt"
	"repo-therapist/runtimeconfig"
	"strconv"
)

type Config struct {
	GRPC GRPCConfig
	// MetricsAddress serves /metrics for Prometheus (METRICS_ADDRESS); empty leaves it off. It is its own
	// port, never published (docs/metrics.md).
	MetricsAddress string
	Database       DatabaseConfig
	// DynamicsDays is the window of the score and place change in the ranking (RANKING_DYNAMICS_DAYS).
	DynamicsDays int
	// SizeSmallLOC and SizeLargeLOC are the size-filter limits in lines of code (RANKING_SIZE_SMALL_LOC,
	// RANKING_SIZE_LARGE_LOC).
	SizeSmallLOC, SizeLargeLOC int
	// AnalyticsCacheSeconds is how long the platform analytics are reused (ANALYTICS_CACHE_SECONDS).
	AnalyticsCacheSeconds int
}

type GRPCConfig struct {
	Address string
}

// DatabaseConfig uses the same env vars and defaults as the aggregator, since both services
// connect to the same shared DB.
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

func LoadConfig() *Config {
	dynamicsDays, err := strconv.Atoi(getEnvDefault("RANKING_DYNAMICS_DAYS", "7"))
	if err != nil || dynamicsDays < 1 {
		dynamicsDays = 7
	}

	sizeSmall, err := strconv.Atoi(getEnvDefault("RANKING_SIZE_SMALL_LOC", "2000"))
	if err != nil || sizeSmall < 1 {
		sizeSmall = 2000
	}

	sizeLarge, err := strconv.Atoi(getEnvDefault("RANKING_SIZE_LARGE_LOC", "20000"))
	if err != nil || sizeLarge <= sizeSmall {
		sizeLarge = 20000
		if sizeLarge <= sizeSmall {
			sizeSmall = 2000
		}
	}

	cacheSeconds, err := strconv.Atoi(getEnvDefault("ANALYTICS_CACHE_SECONDS", "300"))
	if err != nil || cacheSeconds < 1 {
		cacheSeconds = 300
	}

	return &Config{
		AnalyticsCacheSeconds: cacheSeconds,
		DynamicsDays:          dynamicsDays,
		SizeSmallLOC:          sizeSmall,
		SizeLargeLOC:          sizeLarge,

		MetricsAddress: getEnvDefault("METRICS_ADDRESS", ""),

		GRPC: GRPCConfig{
			Address: getEnvDefault("GRPC_ADDRESS", ":8086"),
		},

		Database: DatabaseConfig{
			Host:     getEnvDefault("DB_HOST", "localhost"),
			Port:     getEnvDefault("DB_PORT", "5432"),
			User:     getEnvDefault("DB_USER", "postgres"),
			Password: getEnvDefault("DB_PASSWORD", "postgres"),
			DBName:   getEnvDefault("DB_NAME", "postgres"),
			SSLMode:  getEnvDefault("DB_SSL_MODE", "disable"),
		},
	}
}

func getEnvDefault(key string, def string) string {
	value := runtimeconfig.Getenv(key)
	if value == "" {
		return def
	}

	return value
}
