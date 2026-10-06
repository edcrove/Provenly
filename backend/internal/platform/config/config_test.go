package config

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"PROVENLY_DATABASE_URL": "postgres://db"}))
	require.NoError(t, err)
	assert.Equal(t, Config{
		Env: "development", HTTPAddr: ":8080", DatabaseURL: "postgres://db",
		LogLevel: slog.LevelInfo, MaxIngestBytes: 10 << 20, WebhooksAllowPrivate: true, GitHubAPIURL: "https://api.github.com",
		WebhookDeliveryRetentionDays: 90,
	}, cfg)
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"PROVENLY_DATABASE_URL":                    "postgres://db",
		"PROVENLY_ENV":                             "ci",
		"PROVENLY_HTTP_ADDR":                       ":9999",
		"PROVENLY_LOG_LEVEL":                       "debug",
		"PROVENLY_MAX_INGEST_BYTES":                "1024",
		"PROVENLY_AUTO_MIGRATE":                    "true",
		"PROVENLY_JWT_SECRET":                      strings.Repeat("k", 32),
		"PROVENLY_ADMIN_USERNAME":                  "admin",
		"PROVENLY_ADMIN_PASSWORD":                  "correct horse",
		"PROVENLY_SECRETS_KEY":                     "a2tra2tra2tra2tra2tra2tra2tra2tra2tra2tra2s=",
		"PROVENLY_WEBHOOKS_ALLOW_PRIVATE":          "false",
		"PROVENLY_GITHUB_API_URL":                  "https://github.example.com/api/v3/",
		"PROVENLY_WEBHOOK_DELIVERY_RETENTION_DAYS": "0",
	}))
	require.NoError(t, err)
	assert.Equal(t, Config{
		Env: "ci", HTTPAddr: ":9999", DatabaseURL: "postgres://db",
		LogLevel: slog.LevelDebug, MaxIngestBytes: 1024, AutoMigrate: true,
		JWTSecret: []byte(strings.Repeat("k", 32)), AdminUsername: "admin", AdminPassword: "correct horse",
		SecretsKey: []byte(strings.Repeat("k", 32)), GitHubAPIURL: "https://github.example.com/api/v3",
		WebhookDeliveryRetentionDays: 0,
	}, cfg)
}

// Prod refuses private, loopback and link-local webhook and connector targets unless explicitly allowed: the
// SSRF guard is on by default only there.
func TestLoadProdDefaults(t *testing.T) {
	prod := map[string]string{
		"PROVENLY_DATABASE_URL": "postgres://db", "PROVENLY_ENV": "prod", "PROVENLY_JWT_SECRET": strings.Repeat("k", 32),
		"PROVENLY_SECRETS_KEY": "a2tra2tra2tra2tra2tra2tra2tra2tra2tra2tra2s=",
	}
	cfg, err := Load(env(prod))
	require.NoError(t, err)
	assert.Equal(t, "prod", cfg.Env)
	assert.False(t, cfg.WebhooksAllowPrivate, "prod blocks private targets by default")
	for _, e := range []string{"development", "ci", "demo", "qa"} {
		cfg, err := Load(env(map[string]string{"PROVENLY_DATABASE_URL": "x", "PROVENLY_ENV": e}))
		require.NoError(t, err, e)
		assert.True(t, cfg.WebhooksAllowPrivate, e)
	}
}

// An unknown environment name would silently get development defaults (random keys, SSRF guard off): refused.
func TestLoadRefusesUnknownEnvironments(t *testing.T) {
	for name, want := range map[string]string{
		"production": `PROVENLY_ENV must be one of development, ci, demo, qa, prod, got "production"`,
		"Prod":       `got "Prod"`,
		"staging":    `got "staging"`,
	} {
		_, err := Load(env(map[string]string{"PROVENLY_DATABASE_URL": "x", "PROVENLY_ENV": name}))
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), want)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]map[string]string{
		"PROVENLY_DATABASE_URL is required":             {},
		"PROVENLY_LOG_LEVEL":                            {"PROVENLY_DATABASE_URL": "x", "PROVENLY_LOG_LEVEL": "loud"},
		"PROVENLY_MAX_INGEST_BYTES":                     {"PROVENLY_DATABASE_URL": "x", "PROVENLY_MAX_INGEST_BYTES": "-1"},
		"PROVENLY_AUTO_MIGRATE":                         {"PROVENLY_DATABASE_URL": "x", "PROVENLY_AUTO_MIGRATE": "maybe"},
		"PROVENLY_JWT_SECRET must be at least 32 bytes": {"PROVENLY_DATABASE_URL": "x", "PROVENLY_JWT_SECRET": "short"},
		"PROVENLY_JWT_SECRET is required in prod":       {"PROVENLY_DATABASE_URL": "x", "PROVENLY_ENV": "prod"},
		"PROVENLY_SECRETS_KEY is required in prod":      {"PROVENLY_DATABASE_URL": "x", "PROVENLY_ENV": "prod", "PROVENLY_JWT_SECRET": strings.Repeat("k", 32)},
		"PROVENLY_SECRETS_KEY must be 32 bytes":         {"PROVENLY_DATABASE_URL": "x", "PROVENLY_SECRETS_KEY": "c2hvcnQ="},
		"PROVENLY_WEBHOOKS_ALLOW_PRIVATE":               {"PROVENLY_DATABASE_URL": "x", "PROVENLY_WEBHOOKS_ALLOW_PRIVATE": "sometimes"},
		"must be set together":                          {"PROVENLY_DATABASE_URL": "x", "PROVENLY_ADMIN_USERNAME": "admin"},
		`RETENTION_DAYS must be a whole number of days from 0 (keep) to 36500, got "-1"`: {"PROVENLY_DATABASE_URL": "x", "PROVENLY_WEBHOOK_DELIVERY_RETENTION_DAYS": "-1"},
		`got "90d"`:   {"PROVENLY_DATABASE_URL": "x", "PROVENLY_WEBHOOK_DELIVERY_RETENTION_DAYS": "90d"},
		`got "36501"`: {"PROVENLY_DATABASE_URL": "x", "PROVENLY_WEBHOOK_DELIVERY_RETENTION_DAYS": "36501"},
		"public demo password": {"PROVENLY_DATABASE_URL": "x", "PROVENLY_ENV": "prod", "PROVENLY_JWT_SECRET": strings.Repeat("k", 32),
			"PROVENLY_SECRETS_KEY": "a2tra2tra2tra2tra2tra2tra2tra2tra2tra2tra2s=", "PROVENLY_ADMIN_USERNAME": "admin", "PROVENLY_ADMIN_PASSWORD": "provenly-demo"},
	}
	for want, m := range cases {
		_, err := Load(env(m))
		require.Error(t, err)
		assert.Contains(t, err.Error(), want)
	}
}
