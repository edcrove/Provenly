package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

var errBoom = errors.New("boom")

type recorder struct {
	migrations []string
	served     bool
}

func testDeps(t *testing.T, env map[string]string, r *recorder) (Deps, *bytes.Buffer) {
	t.Helper()
	var stderr bytes.Buffer
	return Deps{
		Getenv: func(k string) string { return env[k] },
		Stderr: &stderr,
		OpenDB: pgxpool.New, // lazy: does not connect
		Migrate: func(_ context.Context, _ *pgxpool.Pool, cmd string) error {
			r.migrations = append(r.migrations, cmd)
			return nil
		},
		Listen: func(string) (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") },
		Serve: func(_ context.Context, l net.Listener, h http.Handler) error {
			r.served = h != nil
			return l.Close()
		},
	}, &stderr
}

var baseEnv = map[string]string{"PROVENLY_DATABASE_URL": "postgres://u:p@127.0.0.1:1/db"}

func TestServe(t *testing.T) {
	r := &recorder{}
	d, stderr := testDeps(t, baseEnv, r)
	assert.Equal(t, 0, Run(context.Background(), nil, d))
	assert.Contains(t, stderr.String(), `"msg":"starting provenly","version":"dev"`)
	assert.True(t, r.served)
	assert.Empty(t, r.migrations)
}

// A configured secret signs sessions (no random fallback).
func TestServeWithSecret(t *testing.T) {
	r := &recorder{}
	env := map[string]string{"PROVENLY_DATABASE_URL": baseEnv["PROVENLY_DATABASE_URL"], "PROVENLY_JWT_SECRET": strings.Repeat("k", 32)}
	d, stderr := testDeps(t, env, r)
	assert.Equal(t, 0, Run(context.Background(), nil, d))
	assert.NotContains(t, stderr.String(), "random secret")
	r = &recorder{}
	d, stderr = testDeps(t, baseEnv, r)
	assert.Equal(t, 0, Run(context.Background(), nil, d))
	assert.Contains(t, stderr.String(), "random secret")
}

// A configured secrets key encrypts webhook secrets and connector tokens; without one a random key is used, with a
// warning (they cannot be read after a restart).
func TestServeWithSecretsKey(t *testing.T) {
	r := &recorder{}
	env := map[string]string{"PROVENLY_DATABASE_URL": baseEnv["PROVENLY_DATABASE_URL"], "PROVENLY_SECRETS_KEY": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))}
	d, stderr := testDeps(t, env, r)
	assert.Equal(t, 0, Run(context.Background(), nil, d))
	assert.NotContains(t, stderr.String(), "PROVENLY_SECRETS_KEY")
	d, stderr = testDeps(t, baseEnv, &recorder{})
	assert.Equal(t, 0, Run(context.Background(), nil, d))
	assert.Contains(t, stderr.String(), "PROVENLY_SECRETS_KEY is not set")
}

func TestServeWithAutoMigrate(t *testing.T) {
	r := &recorder{}
	env := map[string]string{"PROVENLY_DATABASE_URL": baseEnv["PROVENLY_DATABASE_URL"], "PROVENLY_AUTO_MIGRATE": "true"}
	d, _ := testDeps(t, env, r)
	assert.Equal(t, 0, Run(context.Background(), []string{"serve"}, d))
	assert.Equal(t, []string{"up"}, r.migrations)
}

func TestMigrate(t *testing.T) {
	r := &recorder{}
	d, _ := testDeps(t, baseEnv, r)
	assert.Equal(t, 0, Run(context.Background(), []string{"migrate", "status"}, d))
	assert.Equal(t, []string{"status"}, r.migrations)
	assert.False(t, r.served)
}

func TestErrors(t *testing.T) {
	cases := map[string]struct {
		args   []string
		env    map[string]string
		mutate func(*Deps)
		want   string
	}{
		"usage":        {args: []string{"explode"}, env: baseEnv, want: "usage"},
		"migrate args": {args: []string{"migrate"}, env: baseEnv, want: "usage"},
		"config":       {env: map[string]string{}, want: "PROVENLY_DATABASE_URL"},
		"open db": {env: baseEnv, mutate: func(d *Deps) {
			d.OpenDB = func(context.Context, string) (*pgxpool.Pool, error) { return nil, errBoom }
		}, want: "boom"},
		"auto migrate": {env: map[string]string{"PROVENLY_DATABASE_URL": "postgres://x@127.0.0.1:1/db", "PROVENLY_AUTO_MIGRATE": "1"}, mutate: func(d *Deps) {
			d.Migrate = func(context.Context, *pgxpool.Pool, string) error { return errBoom }
		}, want: "boom"},
		"bootstrap admin": {env: map[string]string{"PROVENLY_DATABASE_URL": "postgres://x@127.0.0.1:1/db",
			"PROVENLY_ADMIN_USERNAME": "admin", "PROVENLY_ADMIN_PASSWORD": "correct horse"}, want: "127.0.0.1"},
		"listen": {env: baseEnv, mutate: func(d *Deps) {
			d.Listen = func(string) (net.Listener, error) { return nil, errBoom }
		}, want: "boom"},
	}
	for name, c := range cases {
		d, stderr := testDeps(t, c.env, &recorder{})
		if c.mutate != nil {
			c.mutate(&d)
		}
		assert.Equal(t, 1, Run(context.Background(), c.args, d), name)
		assert.Contains(t, stderr.String(), c.want, name)
	}
}

func TestDefaultDeps(t *testing.T) {
	var stderr bytes.Buffer
	d := DefaultDeps(func(string) string { return "" }, &stderr)
	require.NotNil(t, d.OpenDB)
	require.NotNil(t, d.Migrate)
	l, err := d.Listen("127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.NoError(t, d.Serve(ctx, l, http.NotFoundHandler()))
}

// With an OTLP endpoint the exporter is built at startup; one that cannot be built stops the command.
func TestOpenTelemetry(t *testing.T) {
	env := map[string]string{"PROVENLY_DATABASE_URL": baseEnv["PROVENLY_DATABASE_URL"], "OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:1"}
	r := &recorder{}
	d, _ := testDeps(t, env, r)
	built := false
	d.Exporter = func(context.Context) (sdktrace.SpanExporter, error) {
		built = true
		return tracetest.NewInMemoryExporter(), nil
	}
	assert.Equal(t, 0, Run(context.Background(), nil, d))
	assert.True(t, built)
	d.Exporter = func(context.Context) (sdktrace.SpanExporter, error) { return nil, errors.New("bad endpoint") }
	d2, stderr := testDeps(t, env, r)
	d2.Exporter = d.Exporter
	assert.Equal(t, 1, Run(context.Background(), nil, d2))
	assert.Contains(t, stderr.String(), "opentelemetry: bad endpoint")
}
