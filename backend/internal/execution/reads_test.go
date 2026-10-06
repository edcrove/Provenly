package execution

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// A live poll reads the run once and its summary inputs once; the other run reads check existence with the run's
// project alone (card #44: it used to read the run three times and the summary inputs twice).
func TestRunReads(t *testing.T) {
	svc, repo, ctx := setup()
	run, err := svc.StartRun(ctx, NewRun{ProjectID: 7, Provider: "github", ProviderRunID: "1", RunAttempt: 1, Mode: ModeLive}, []int64{1})
	require.NoError(t, err)

	reads := func(read func()) (runs, inputs int) {
		repo.calls, repo.summaryReads = nil, nil
		read()
		return repo.calls["GetTestRun"], len(repo.summaryReads)
	}
	runs, inputs := reads(func() { _, err = svc.Live(ctx, run.ID) })
	require.NoError(t, err)
	assert.Equal(t, [2]int{1, 1}, [2]int{runs, inputs}, "live: one run read, one summary read")
	runs, inputs = reads(func() { _, err = svc.Summary(ctx, run.ID) })
	require.NoError(t, err)
	assert.Equal(t, [2]int{0, 1}, [2]int{runs, inputs}, "summary: no run read")
	for name, read := range map[string]func() error{
		"results": func() error {
			_, err := svc.ListRunResults(ctx, run.ID, ResultFilter{}, pagination.Default())
			return err
		},
		"parse errors": func() error { _, err := svc.ListParseErrors(ctx, run.ID, pagination.Default()); return err },
		"amendments":   func() error { _, err := svc.ListAmendments(ctx, run.ID, pagination.Default()); return err },
	} {
		runs, inputs = reads(func() { require.NoError(t, read()) })
		assert.Equal(t, [2]int{0, 0}, [2]int{runs, inputs}, name)
		assert.Equal(t, 1, repo.calls["GetRunProject"], name)
	}

	project, err := svc.RunProject(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(7), project)
	_, err = svc.RunProject(ctx, 999)
	e, ok := apperr.As(err)
	require.True(t, ok)
	assert.Equal(t, apperr.KindNotFound, e.Kind)
	repo.errs["GetRunProject"] = errBoom
	_, err = svc.RunProject(ctx, run.ID)
	assert.ErrorIs(t, err, errBoom)
}
