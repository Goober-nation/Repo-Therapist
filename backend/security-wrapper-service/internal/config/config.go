package config

import "os"

type Config struct {
	AppPort       string
	AppsecURL     string
	ClientTimeout string
	// PollInterval/PollTimeout govern polling POST /v1/reports/generate's operation, both
	// seconds. Defaults are a guess (no real AppSec report-generation latency data yet) — revisit
	// once tested against the real API.
	PollIntervalSeconds string
	PollTimeoutSeconds  string
}

func LoadConfig() *Config {
	return &Config{
		AppPort:             getEnvDefault("APP_PORT", "8085"),
		AppsecURL:           getEnvDefault("APPSEC_URL", "https://appsec.sourcecraft.tech"),
		ClientTimeout:       getEnvDefault("CLIENT_TIMEOUT", "5"),
		PollIntervalSeconds: getEnvDefault("REPORT_POLL_INTERVAL_SECONDS", "2"),
		PollTimeoutSeconds:  getEnvDefault("REPORT_POLL_TIMEOUT_SECONDS", "60"),
	}
}

func getEnvDefault(key string, def string) string {
	value := os.Getenv(key)
	if value == "" {
		return def
	}
	return value
}
