package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	GRPC     GRPCConfig
	Database DatabaseConfig
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
	return &Config{
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
	value := os.Getenv(key)
	if value == "" {
		return def
	}

	return value
}
