package config

import "os"

type Config struct {
	AppPort        string
	SourceCraftURL string
	ClientTimeout  string
}

func LoadConfig() *Config {
	return &Config{
		AppPort:        getEnvDefault("APP_PORT", "8083"),
		SourceCraftURL: getEnvDefault("SOURCECRAFT_URL", ""),
		ClientTimeout:  getEnvDefault("CLIENT_TIMEOUT", "5"),
	}
}

func getEnvDefault(key string, def string) string {
	value := os.Getenv(key)
	if value == "" {
		return def
	}
	return value
}
