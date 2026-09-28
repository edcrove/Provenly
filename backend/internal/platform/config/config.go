// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// Config is the process configuration.
type Config struct {
	Env            string
	HTTPAddr       string
	DatabaseURL    string
	LogLevel       slog.Level
	MaxIngestBytes int64
	AutoMigrate    bool
}

// Load reads configuration through getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Env:            valueOr(getenv("PROVENLY_ENV"), "development"),
		HTTPAddr:       valueOr(getenv("PROVENLY_HTTP_ADDR"), ":8080"),
		DatabaseURL:    getenv("PROVENLY_DATABASE_URL"),
		MaxIngestBytes: 10 << 20,
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("PROVENLY_DATABASE_URL is required")
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(valueOr(getenv("PROVENLY_LOG_LEVEL"), "info"))); err != nil {
		return Config{}, fmt.Errorf("PROVENLY_LOG_LEVEL: %w", err)
	}
	if raw := getenv("PROVENLY_MAX_INGEST_BYTES"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("PROVENLY_MAX_INGEST_BYTES must be a positive integer, got %q", raw)
		}
		cfg.MaxIngestBytes = n
	}
	if raw := getenv("PROVENLY_AUTO_MIGRATE"); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("PROVENLY_AUTO_MIGRATE must be a boolean, got %q", raw)
		}
		cfg.AutoMigrate = b
	}
	return cfg, nil
}

func valueOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
