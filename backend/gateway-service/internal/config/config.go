package config

import (
	"fmt"
	"os"
	"strconv"
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
	Session    SessionConfig
}

type HTTPConfig struct {
	Address string
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
		},

		Aggregator: GRPCServiceConfig{
			Address: getEnvDefault(
				"AGGREGATOR_GRPC_ADDRESS",
				"localhost:8081",
			),
			Timeout: getEnvDuration(
				"AGGREGATOR_GRPC_TIMEOUT",
				10*time.Second,
			),
		},

		Rating: GRPCServiceConfig{
			Address: getEnvDefault(
				"RATING_GRPC_ADDRESS",
				"localhost:8084",
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
				100,
			),
			Window: getEnvDuration(
				"RATE_LIMIT_WINDOW",
				time.Minute,
			),
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
