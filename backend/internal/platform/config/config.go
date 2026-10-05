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
	// JWTSecret signs session tokens; nil means a random one per start (not allowed in prod).
	JWTSecret []byte
	// AdminUsername and AdminPassword create the first administrator when there are no users.
	AdminUsername string
	AdminPassword string
}

// DemoAdminPassword is the published password of the local demo administrator; prod refuses it.
const DemoAdminPassword = "provenly-demo"

// minSecretBytes is the shortest accepted PROVENLY_JWT_SECRET (HS256 key size).
const minSecretBytes = 32

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
	if secret := getenv("PROVENLY_JWT_SECRET"); secret != "" {
		if len(secret) < minSecretBytes {
			return Config{}, fmt.Errorf("PROVENLY_JWT_SECRET must be at least %d bytes", minSecretBytes)
		}
		cfg.JWTSecret = []byte(secret)
	} else if cfg.Env == "prod" {
		return Config{}, fmt.Errorf("PROVENLY_JWT_SECRET is required in prod (at least %d bytes)", minSecretBytes)
	}
	cfg.AdminUsername, cfg.AdminPassword = getenv("PROVENLY_ADMIN_USERNAME"), getenv("PROVENLY_ADMIN_PASSWORD")
	if (cfg.AdminUsername == "") != (cfg.AdminPassword == "") {
		return Config{}, fmt.Errorf("PROVENLY_ADMIN_USERNAME and PROVENLY_ADMIN_PASSWORD must be set together")
	}
	if cfg.Env == "prod" && cfg.AdminPassword == DemoAdminPassword {
		return Config{}, fmt.Errorf("PROVENLY_ADMIN_PASSWORD is the public demo password: set your own in prod")
	}
	return cfg, nil
}

func valueOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
