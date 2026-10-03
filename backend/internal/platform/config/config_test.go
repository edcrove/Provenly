package config

import (
	"log/slog"
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
		LogLevel: slog.LevelInfo, MaxIngestBytes: 10 << 20,
	}, cfg)
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"PROVENLY_DATABASE_URL":     "postgres://db",
		"PROVENLY_ENV":              "ci",
		"PROVENLY_HTTP_ADDR":        ":9999",
		"PROVENLY_LOG_LEVEL":        "debug",
		"PROVENLY_MAX_INGEST_BYTES": "1024",
		"PROVENLY_AUTO_MIGRATE":     "true",
	}))
	require.NoError(t, err)
	assert.Equal(t, Config{
		Env: "ci", HTTPAddr: ":9999", DatabaseURL: "postgres://db",
		LogLevel: slog.LevelDebug, MaxIngestBytes: 1024, AutoMigrate: true,
	}, cfg)
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]map[string]string{
		"PROVENLY_DATABASE_URL is required": {},
		"PROVENLY_LOG_LEVEL":                {"PROVENLY_DATABASE_URL": "x", "PROVENLY_LOG_LEVEL": "loud"},
		"PROVENLY_MAX_INGEST_BYTES":         {"PROVENLY_DATABASE_URL": "x", "PROVENLY_MAX_INGEST_BYTES": "-1"},
		"PROVENLY_AUTO_MIGRATE":             {"PROVENLY_DATABASE_URL": "x", "PROVENLY_AUTO_MIGRATE": "maybe"},
	}
	for want, m := range cases {
		_, err := Load(env(m))
		require.Error(t, err)
		assert.Contains(t, err.Error(), want)
	}
}
