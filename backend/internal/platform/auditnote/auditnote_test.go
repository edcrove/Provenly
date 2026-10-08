package auditnote

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNote(t *testing.T) {
	Project(context.Background(), 3) // without a note: nothing to record, no panic
	ctx, n := Open(context.Background())
	assert.Zero(t, n.ProjectID())
	Project(ctx, 2)
	Project(ctx, 5)
	assert.Equal(t, int64(2), n.ProjectID(), "the first project allowed wins")

	Created(context.Background(), "CHK-1", true) // without a note: nothing to record, no panic
	assert.Empty(t, n.Created())
	Created(ctx, "CHK", false)
	assert.Equal(t, "CHK", n.Created())
	assert.Empty(t, n.TestCaseKey())
	Created(ctx, "CHK-12", true)
	assert.Equal(t, "CHK-12", n.TestCaseKey())
}
