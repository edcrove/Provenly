// Package config loads runtime configuration from environment variables.
package config

import (
	"encoding/base64"
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
	// SecretsKey encrypts the secrets Provenly must read back (webhook signing secrets, connector tokens): 32 bytes,
	// base64 in PROVENLY_SECRETS_KEY. Nil means a random key per start (not allowed in prod).
	SecretsKey []byte
	// WebhooksAllowPrivate lets webhooks target private, loopback and link-local addresses (default: everywhere
	// but prod).
	WebhooksAllowPrivate bool
	// GitHubAPIURL is the GitHub REST API the GitHub connector calls (GitHub Enterprise or a test double).
	GitHubAPIURL string
	// OTLPExport is true when an OpenTelemetry OTLP endpoint is configured (OTEL_EXPORTER_OTLP_ENDPOINT or
	// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT): spans are then exported.
	OTLPExport bool
}

// DemoAdminPassword is the published password of the local demo administrator; prod refuses it.
const DemoAdminPassword = "provenly-demo"

// secretsKeyBytes is the size of PROVENLY_SECRETS_KEY (AES-256).
const secretsKeyBytes = 32

// minSecretBytes is the shortest accepted PROVENLY_JWT_SECRET (HS256 key size).
const minSecretBytes = 32

// Load reads configuration through getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Env:            valueOr(getenv("PROVENLY_ENV"), "development"),
		HTTPAddr:       valueOr(getenv("PROVENLY_HTTP_ADDR"), ":8080"),
		DatabaseURL:    getenv("PROVENLY_DATABASE_URL"),
		MaxIngestBytes: 10 << 20,
		OTLPExport:     getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "",
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
	if raw := getenv("PROVENLY_SECRETS_KEY"); raw != "" {
		key, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(key) != secretsKeyBytes {
			return Config{}, fmt.Errorf("PROVENLY_SECRETS_KEY must be %d bytes in base64 (openssl rand -base64 32)", secretsKeyBytes)
		}
		cfg.SecretsKey = key
	} else if cfg.Env == "prod" {
		return Config{}, fmt.Errorf("PROVENLY_SECRETS_KEY is required in prod (openssl rand -base64 32)")
	}
	cfg.WebhooksAllowPrivate = cfg.Env != "prod"
	if raw := getenv("PROVENLY_WEBHOOKS_ALLOW_PRIVATE"); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("PROVENLY_WEBHOOKS_ALLOW_PRIVATE must be a boolean, got %q", raw)
		}
		cfg.WebhooksAllowPrivate = b
	}
	cfg.GitHubAPIURL = strings.TrimRight(valueOr(getenv("PROVENLY_GITHUB_API_URL"), "https://api.github.com"), "/")
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
