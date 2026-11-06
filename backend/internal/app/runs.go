package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/ingestion"
)

// Runs ends the runs Provenly closes on its own and tells the project's webhooks (CHG-003): the running runs of a
// project being archived, and the live and sharded runs abandoned by CI.
type Runs struct {
	runs   *execution.Service
	notify ingestion.RunNotifier
}

// CloseProjectRuns ends the running runs of a project being archived (catalog.RunCloser).
func (c Runs) CloseProjectRuns(ctx context.Context, projectID int64) error {
	closed, err := c.runs.CloseProjectRuns(ctx, projectID)
	c.notifyAll(ctx, closed)
	return err
}

// CloseAbandoned ends the live and sharded runs without an event or shard for idle.
func (c Runs) CloseAbandoned(ctx context.Context, idle time.Duration) {
	closed, err := c.runs.CloseAbandonedRuns(ctx, idle)
	if err != nil {
		slog.ErrorContext(ctx, "closing abandoned runs failed", "error", err)
		return
	}
	if len(closed) > 0 {
		slog.InfoContext(ctx, "closed abandoned runs", "count", len(closed), "idle", idle.String())
	}
	c.notifyAll(ctx, closed)
}

// Run closes abandoned runs every interval until ctx ends; idle 0 never closes them.
func (c Runs) Run(ctx context.Context, interval, idle time.Duration) {
	if idle <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		c.CloseAbandoned(ctx, idle)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (c Runs) notifyAll(ctx context.Context, runs []execution.TestRun) {
	for _, r := range runs {
		c.notify.RunCompleted(ctx, r)
	}
}
