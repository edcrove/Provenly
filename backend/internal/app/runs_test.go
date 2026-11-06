package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/execution"
)

type stubCloser struct {
	mu      sync.Mutex
	closed  []execution.TestRun
	err     error
	sweeps  int
	project int64
}

func (s *stubCloser) CloseProjectRuns(_ context.Context, projectID int64) ([]execution.TestRun, error) {
	s.project = projectID
	return s.closed, s.err
}

func (s *stubCloser) CloseAbandonedRuns(context.Context, time.Duration) ([]execution.TestRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweeps++
	return s.closed, s.err
}

func (s *stubCloser) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sweeps
}

type recordNotifier struct {
	mu  sync.Mutex
	ids []int64
}

func (n *recordNotifier) RunCompleted(_ context.Context, r execution.TestRun) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ids = append(n.ids, r.ID)
}

func TestRunsCloseProjectRunsNotifiesEachClosedRun(t *testing.T) {
	boom := errors.New("boom")
	stub := &stubCloser{closed: []execution.TestRun{{ID: 1}, {ID: 2}}, err: boom}
	n := &recordNotifier{}
	err := Runs{runs: stub, notify: n}.CloseProjectRuns(context.Background(), 7)
	require.ErrorIs(t, err, boom, "the error reaches the archive")
	assert.Equal(t, int64(7), stub.project)
	assert.Equal(t, []int64{1, 2}, n.ids, "runs closed before the error still fire run.completed")
}

func TestRunsCloseAbandoned(t *testing.T) {
	n := &recordNotifier{}
	Runs{runs: &stubCloser{closed: []execution.TestRun{{ID: 3}}}, notify: n}.CloseAbandoned(context.Background(), time.Hour)
	assert.Equal(t, []int64{3}, n.ids)

	n = &recordNotifier{}
	Runs{runs: &stubCloser{closed: []execution.TestRun{{ID: 4}}, err: errors.New("boom")}, notify: n}.CloseAbandoned(context.Background(), time.Hour)
	assert.Empty(t, n.ids, "a failed sweep notifies nothing")

	Runs{runs: &stubCloser{}, notify: n}.CloseAbandoned(context.Background(), time.Hour)
	assert.Empty(t, n.ids)
}

func TestRunsRunSweepsUntilCancelled(t *testing.T) {
	stub := &stubCloser{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Runs{runs: stub, notify: &recordNotifier{}}.Run(ctx, time.Millisecond, time.Hour)
		close(done)
	}()
	require.Eventually(t, func() bool { return stub.count() >= 2 }, 5*time.Second, time.Millisecond, "it sweeps again on every tick")
	cancel()
	<-done
}

func TestRunsRunWithoutTimeoutNeverSweeps(t *testing.T) {
	stub := &stubCloser{}
	Runs{runs: stub, notify: &recordNotifier{}}.Run(context.Background(), time.Millisecond, 0)
	assert.Zero(t, stub.count())
}
