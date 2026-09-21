package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	AppPort       string
	ClientTimeout string
	Dbc           *DatabaseConfig
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	SSLmode  string
	DBname   string
}

func (dbc *DatabaseConfig) DSN() string {
	port, _ := strconv.Atoi(dbc.Port)
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		dbc.User, dbc.Password, dbc.Host, port, dbc.DBname, dbc.SSLmode)
}

func LoadConfig() *Config {
	return &Config{
		AppPort:       getEnvDefault("APP_PORT", "8083"),
		ClientTimeout: getEnvDefault("CLIENT_TIMEOUT", "5"),
		Dbc: &DatabaseConfig{
			Host:     getEnvDefault("DB_HOST", "localhost"),
			Port:     getEnvDefault("DB_PORT", "5432"),
			User:     getEnvDefault("DB_USER", "postgres"),
			Password: getEnvDefault("DB_PASSWORD", "postgres"),
			DBname:   getEnvDefault("DB_NAME", "postgres"),
			SSLmode:  getEnvDefault("SSL_MODE", "disabled"),
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
