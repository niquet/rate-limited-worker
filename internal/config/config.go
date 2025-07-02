package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port         int    `json:"port"`
	LogLevel     string `json:"log_level"`
	OTELEndpoint string `json:"otel_endpoint"`
	Environment  string `json:"environment"`
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:         getEnvInt("PORT", 8080),
		LogLevel:     getEnvString("LOG_LEVEL", "INFO"),
		OTELEndpoint: getEnvString("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
		Environment:  getEnvString("ENVIRONMENT", "development"),
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", c.Port)
	}

	validLogLevels := map[string]bool{
		"DEBUG": true,
		"INFO":  true,
		"WARN":  true,
		"ERROR": true,
	}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("invalid log level: %s", c.LogLevel)
	}

	if c.OTELEndpoint == "" {
		return fmt.Errorf("OTEL endpoint cannot be empty")
	}

	return nil
}

func getEnvString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
