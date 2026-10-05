package ingestion

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

type recordingNotifier struct{ runs []execution.TestRun }

func (n *recordingNotifier) RunCompleted(_ context.Context, run execution.TestRun) {
	n.runs = append(n.runs, run)
}

// A report that creates (or completes) a run is notified once; a replay is not; a failed recording is not.
func TestIngestNotifiesCompletedRuns(t *testing.T) {
	ctx := context.Background()
	n := &recordingNotifier{}
	rec := &fakeRecorder{created: true}
	svc := NewService(&fakeCatalog{}, rec, fakeAccess{})
	svc.SetNotifier(n)
	_, err := svc.IngestJUnit(ctx, meta, strings.NewReader(`<testsuite name="s"/>`))
	require.NoError(t, err)
	require.Len(t, n.runs, 1)

	rec.created = false
	_, err = svc.IngestJUnit(ctx, meta, strings.NewReader(`<testsuite name="s"/>`))
	require.NoError(t, err)
	assert.Len(t, n.runs, 1, "a replay changes nothing")

	failing := NewService(&fakeCatalog{}, &fakeRecorder{created: true, recordErr: errBoom}, fakeAccess{})
	failing.SetNotifier(n)
	_, err = failing.IngestJUnit(ctx, meta, strings.NewReader(`<testsuite name="s"/>`))
	require.Error(t, err)
	assert.Len(t, n.runs, 1)
}

func TestManualFinishNotifies(t *testing.T) {
	ctx := context.Background()
	n := &recordingNotifier{}
	m := NewManual(&manualCatalog{}, &manualRecorder{}, manualAccess{role: authz.RoleMember})
	m.SetNotifier(n)
	_, err := m.Finish(ctx, 9, execution.RunCancelled)
	require.NoError(t, err)
	require.Len(t, n.runs, 1)
	assert.Equal(t, execution.RunCancelled, n.runs[0].Status)

	failing := NewManual(&manualCatalog{}, &manualRecorder{err: errBoomIngest}, manualAccess{role: authz.RoleMember})
	failing.SetNotifier(n)
	_, err = failing.Finish(ctx, 9, execution.RunCompleted)
	require.Error(t, err)
	assert.Len(t, n.runs, 1)
}
