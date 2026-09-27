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
}

func LoadConfig() *Config {
	return &Config{
		GRPC: GRPCConfig{
			Address: getEnvDefault(
				"GRPC_ADDRESS",
				":8083",
			),
			Timeout: time.Second * getEnvDuration(
				"GRPC_TIMEOUT",
				time.Second*10,
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
				"llm.recommendations.requests",
			),

			GradingResultsTopic: getEnvDefault(
				"KAFKA_GRADING_RESULTS_TOPIC",
				"grading.results",
			),

			LLMRecommendationsTopic: getEnvDefault(
				"KAFKA_LLM_RECOMMENDATIONS_TOPIC",
				"llm.recommendations.results",
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
				24*time.Hour,
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
