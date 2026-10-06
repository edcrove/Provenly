package ingestion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

func digestOf(body string) string {
	d := sha256.Sum256([]byte(body))
	return hex.EncodeToString(d[:])
}

// A shard answers for its own report: its results count, diagnostics, parse errors, digest and status; the run waits
// for the missing shards and is notified once, when it ends.
func TestIngestShard(t *testing.T) {
	ctx := context.Background()
	body := `<testsuite name="s"><testcase name="a"/></testsuite>`
	m := meta
	m.Shard, m.ShardTotal = 2, 3
	running := execution.TestRun{ID: 1, Status: execution.RunRunning, Mode: execution.ModeSharded, ShardTotal: 3, ShardsReceived: []int32{2},
		Shards: []execution.RunShard{{Shard: 1, ReportSHA256: "x", Status: execution.RunCompleted, ResultCount: 9},
			{Shard: 2, ReportSHA256: digestOf(body), Status: execution.RunCompleted, ResultCount: 4}}}
	rec := &fakeRecorder{created: true, shardRun: &running,
		diags:    []execution.Diagnostic{{TestName: "one", Shard: 1}, {TestName: "two", Shard: 2}},
		storedPE: []execution.ParseError{{Index: 0, Message: "one", Shard: 1}, {Index: 0, Message: "two", Shard: 2}}}
	n := &recordingNotifier{}
	svc := NewService(&fakeCatalog{}, rec, fakeAccess{})
	svc.SetNotifier(n)

	out, err := svc.IngestJUnit(ctx, m, strings.NewReader(body))
	require.NoError(t, err)
	assert.Equal(t, int32(2), rec.gotRun.Shard)
	assert.Equal(t, int32(3), rec.gotRun.ShardTotal)
	assert.Equal(t, 4, out.Persisted)
	require.Len(t, out.Diagnostics, 1)
	assert.Equal(t, "two", out.Diagnostics[0].TestName)
	assert.Equal(t, []execution.ParseError{{Index: 0, Message: "two", Shard: 2}}, out.ParseErrors)
	assert.Equal(t, []string{fmt.Sprintf(ShardsPendingWarning, 2, 3, "1, 3")}, out.Warnings)
	assert.Empty(t, n.runs, "a running sharded run is not notified")

	// A replay with another report and another status says so, against the shard's own digest and status.
	rec.created = false
	m.Status = execution.RunInterrupted
	out, err = svc.IngestJUnit(ctx, m, strings.NewReader(`<testsuite name="other"/>`))
	require.NoError(t, err)
	assert.Contains(t, out.Warnings, ReportDiffersWarning)
	assert.Contains(t, out.Warnings, fmt.Sprintf(StatusDiffersWarning, execution.RunInterrupted, execution.RunCompleted))

	// The last shard completes the run: notified, no pending warning.
	done := running
	done.Status, done.ShardsReceived = execution.RunCompleted, []int32{1, 2, 3}
	rec.shardRun, rec.created = &done, true
	m.Status = ""
	out, err = svc.IngestJUnit(ctx, m, strings.NewReader(body))
	require.NoError(t, err)
	assert.Empty(t, out.Warnings)
	require.Len(t, n.runs, 1)
}

func TestValidateShard(t *testing.T) {
	for _, c := range [][2]int32{{1, 2}, {100, 100}, {0, 0}} {
		m := meta
		m.Shard, m.ShardTotal = c[0], c[1]
		assert.NoError(t, ValidateMeta(m), "%v", c)
	}
	for _, c := range [][2]int32{{1, 1}, {0, 2}, {3, 2}, {1, 101}, {-1, -1}, {1, 0}} {
		m := meta
		m.Shard, m.ShardTotal = c[0], c[1]
		e, ok := apperr.As(ValidateMeta(m))
		require.True(t, ok, "%v", c)
		assert.Equal(t, "shard", e.Fields[0].Field, "%v", c)
	}
}

func TestFinalizeShards(t *testing.T) {
	ctx := context.Background()
	interrupted := execution.TestRun{ID: 1, Status: execution.RunInterrupted, Mode: execution.ModeSharded, ShardTotal: 3,
		ShardsReceived: []int32{1}, ResultCount: 5}
	rec := &fakeRecorder{finalRun: interrupted, finished: true}
	n := &recordingNotifier{}
	svc := NewService(&fakeCatalog{}, rec, fakeAccess{})
	svc.SetNotifier(n)

	out, err := svc.FinalizeShards(ctx, meta)
	require.NoError(t, err)
	assert.True(t, out.Created)
	assert.Equal(t, 5, out.Persisted)
	assert.Equal(t, []string{fmt.Sprintf(ShardsMissingWarning, "2, 3", 3)}, out.Warnings)
	assert.Equal(t, [4]any{int64(1), "github", "99", int32(1)}, rec.gotFinal)
	require.Len(t, n.runs, 1)

	// An ended run is returned unchanged; a completed one has nothing missing.
	rec.finished = false
	rec.finalRun.Status, rec.finalRun.ShardsReceived = execution.RunCompleted, []int32{1, 2, 3}
	out, err = svc.FinalizeShards(ctx, meta)
	require.NoError(t, err)
	assert.False(t, out.Created)
	assert.Empty(t, out.Warnings)
	assert.Len(t, n.runs, 1)

	_, err = svc.FinalizeShards(ctx, RunMeta{})
	assert.Error(t, err)
	_, err = NewService(&fakeCatalog{projectErr: errBoom}, rec, fakeAccess{}).FinalizeShards(ctx, meta)
	assert.ErrorIs(t, err, errBoom)
	rec.finalErr = errBoom
	_, err = svc.FinalizeShards(ctx, meta)
	assert.ErrorIs(t, err, errBoom)
}

func TestIngestHandlerShards(t *testing.T) {
	api := &stubAPI{out: Outcome{Created: true}}
	rec := post(api, 1024, "provider=github&runId=7&runAttempt=1&shard=2/4", "application/xml", "<x/>")
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, [2]int32{2, 4}, [2]int32{api.gotMeta.Shard, api.gotMeta.ShardTotal})
	for _, bad := range []string{"", "x", "2", "2/", "/4", "0/4", "5/4", "1/1", "1/101", "1000/1000", "2/4/6", "-1/4"} {
		rec = post(&stubAPI{}, 1024, "provider=github&runId=7&runAttempt=1&shard="+bad, "application/xml", "<x/>")
		assert.Equal(t, http.StatusBadRequest, rec.Code, bad)
		assert.Contains(t, rec.Body.String(), `"field":"shard"`, bad)
	}
}

func TestFinalizeHandler(t *testing.T) {
	finalize := func(api API, query string) *httptest.ResponseRecorder {
		mux := http.NewServeMux()
		NewHandler(api, 1024).Register(mux)
		r := httptest.NewRecorder()
		mux.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/v1/ingestion/finalize?"+query, nil))
		return r
	}
	api := &stubAPI{out: Outcome{Created: true, Run: execution.TestRun{ID: 1}, Warnings: []string{"w"}}}
	rec := finalize(api, "project=CHK&provider=github&runId=7&runAttempt=2")
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, RunMeta{ProjectKey: "CHK", Provider: "github", ProviderRunID: "7", RunAttempt: 2}, api.gotMeta)
	assert.Contains(t, rec.Body.String(), `"warnings":["w"]`)
	api.out.Created = false
	assert.Equal(t, http.StatusOK, finalize(api, "provider=github&runId=7&runAttempt=2").Code)

	rec = finalize(&stubAPI{}, "provider=github&runId=7&runAttempt=2&shard=1/2")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"field":"shard"`)
	assert.Equal(t, http.StatusBadRequest, finalize(&stubAPI{}, "provider=github").Code)
	assert.Equal(t, http.StatusConflict, finalize(&stubAPI{err: apperr.Conflict("not sharded")}, "provider=github&runId=7&runAttempt=2").Code)
}
