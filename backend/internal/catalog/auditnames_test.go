package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/etag"
)

// The audit log names projects, test cases and steps through these lookups (card #48).
func TestAuditNames(t *testing.T) {
	svc, repo, ctx := setup(t)
	tc, err := svc.Create(ctx, CreateInput{ProjectID: 1, Title: "pay"})
	require.NoError(t, err)
	_, _, err = svc.CreateStep(ctx, tc.ID, CreateStepInput{Action: "open"}, etag.Match{})
	require.NoError(t, err)
	second, _, err := svc.CreateStep(ctx, tc.ID, CreateStepInput{Action: "pay"}, etag.Match{})
	require.NoError(t, err)

	key, err := svc.ProjectKey(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, "TC", key)
	key, err = svc.TestCaseKey(ctx, tc.ID)
	require.NoError(t, err)
	assert.Equal(t, tc.Key(), key)
	pos, err := svc.StepPosition(ctx, tc.ID, second.ID)
	require.NoError(t, err)
	assert.Equal(t, int32(2), pos)

	_, err = svc.StepPosition(ctx, tc.ID, 999)
	assert.ErrorIs(t, err, ErrNotFound)
	repo.errs["ListAllTestSteps"] = errBoom
	_, err = svc.StepPosition(ctx, tc.ID, second.ID)
	assert.ErrorIs(t, err, errBoom)
}
