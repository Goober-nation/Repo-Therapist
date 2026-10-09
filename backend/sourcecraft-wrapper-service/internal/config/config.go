package config

import "os"

type Config struct {
	AppPort        string
	SourceCraftURL string
	ClientTimeout  string
	// RetryAttempts/RetryWaitSeconds govern retries of SourceCraft calls on 429/502/503/504 and
	// network errors; CLIENT_TIMEOUT applies to each attempt separately.
	RetryAttempts    string
	RetryWaitSeconds string
	// MaxPages caps paginated SourceCraft fetches; it limits what Activity/Issues grading sees.
	MaxPages string
}

func LoadConfig() *Config {
	return &Config{
		AppPort:          getEnvDefault("APP_PORT", "8082"),
		SourceCraftURL:   getEnvDefault("SOURCECRAFT_URL", ""),
		ClientTimeout:    getEnvDefault("CLIENT_TIMEOUT", "5"),
		RetryAttempts:    getEnvDefault("HTTP_RETRY_ATTEMPTS", "3"),
		RetryWaitSeconds: getEnvDefault("HTTP_RETRY_WAIT_SECONDS", "1"),
		MaxPages:         getEnvDefault("SOURCECRAFT_MAX_PAGES", "50"),
	}
}

func getEnvDefault(key string, def string) string {
	value := os.Getenv(key)
	if value == "" {
		return def
	}
	return value
}
