package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunServesUntilCancelled(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, l, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "hi") }), time.Second)
	}()
	resp, err := http.Get("http://" + l.Addr().String())
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, "hi", string(body))
	cancel()
	assert.NoError(t, <-done)
}

func TestRunReturnsServeError(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, l.Close())
	assert.Error(t, Run(context.Background(), l, http.NotFoundHandler(), time.Second))
}

func TestRunReportsShutdownTimeout(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	slow := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, l, slow, time.Millisecond) }()
	go func() {
		resp, err := http.Get("http://" + l.Addr().String())
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started
	cancel()
	assert.ErrorIs(t, <-done, context.DeadlineExceeded)
}
